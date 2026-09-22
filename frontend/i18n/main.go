// Command i18n manages the console's message catalogues.
//
//	i18n extract          rescan the source and update locales/console.pot
//	i18n merge            fold new strings from the .pot into each .po
//	i18n compile <out>    turn each .po into the JSON the browser loads
//	i18n status           report how complete each translation is
//
// The catalogues are gettext .po files, so they can be edited with any
// translation tool (Poedit, Weblate) rather than only by hand.
//
// msgids are the English source strings themselves, not opaque keys.
// That means an untranslated string falls back to correct English on its
// own, and there is no key-to-English table that can drift out of step
// with the code.
//
// Build-time only; no part of this reaches the console binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// localesDir holds the catalogues, relative to frontend/.
const localesDir = "locales"

// potName is the extracted template every translation is merged from.
const potName = "console.pot"

// sourceRoots are scanned for translatable strings.
var sourceRoots = []string{"src", "templates"}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "extract":
		err = cmdExtract()
	case "merge":
		err = cmdMerge()
	case "compile":
		if len(os.Args) != 3 {
			err = fmt.Errorf("usage: i18n compile <output-dir>")
			break
		}
		err = cmdCompile(os.Args[2])
	case "add":
		if len(os.Args) != 3 {
			err = fmt.Errorf("usage: i18n add <language-code>")
			break
		}
		err = cmdAdd(os.Args[2])
	case "status":
		err = cmdStatus()
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "i18n: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: i18n <command>

  extract          rescan src/ and templates/, update locales/console.pot
  add <code>       create a catalogue for a language (see languages.go)
  merge            fold new strings from the .pot into every .po
  compile <dir>    write each locale's JSON catalogue into <dir>
  status           report translation completeness
`)
}

// cmdExtract rescans the source and rewrites the .pot.
func cmdExtract() error {
	messages, err := extract(sourceRoots)
	if err != nil {
		return err
	}
	path := filepath.Join(localesDir, potName)
	if err := writePOT(path, messages); err != nil {
		return err
	}
	fmt.Printf("%d translatable string(s) -> %s\n", len(messages), path)
	return nil
}

// cmdAdd creates an empty catalogue for a language, with the right
// Plural-Forms rule already set.
//
// Every string starts untranslated, which renders as English - so a new
// language is immediately safe to ship and fills in over time.
func cmdAdd(code string) error {
	lang, err := findLanguage(code)
	if err != nil {
		return err
	}

	path := filepath.Join(localesDir, lang.Code+".po")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	messages, err := extract(sourceRoots)
	if err != nil {
		return err
	}

	cat := newCatalog(lang.Code)
	cat.Header = potHeader(lang.Code)
	cat.Header["Plural-Forms"] = lang.PluralForms
	cat.Header["Language-Team"] = lang.Name
	for _, m := range messages {
		cat.add(&entry{ID: m.ID, Plural: m.Plural, References: m.References})
	}

	if err := os.MkdirAll(localesDir, 0o755); err != nil {
		return err
	}
	if err := writePO(path, cat); err != nil {
		return err
	}

	fmt.Printf("Created %s (%s) with %d untranslated string(s), %d plural form(s)\n",
		path, lang.Name, len(messages), cat.nplurals())
	fmt.Printf("Every string falls back to English until translated.\n")
	return nil
}

// cmdMerge adds newly extracted strings to every .po and marks entries
// whose source string has gone as obsolete.
//
// Existing translations are never discarded: a string that disappears is
// commented out rather than deleted, so restoring it (or fixing a typo in
// the English that changed its msgid) does not throw the translation
// away.
func cmdMerge() error {
	messages, err := extract(sourceRoots)
	if err != nil {
		return err
	}

	locales, err := localeFiles()
	if err != nil {
		return err
	}
	if len(locales) == 0 {
		return fmt.Errorf("no .po files in %s - create one first, e.g. %s/de.po", localesDir, localesDir)
	}

	for _, path := range locales {
		cat, err := readPO(path)
		if err != nil {
			return err
		}
		added, obsolete := cat.merge(messages)
		if err := writePO(path, cat); err != nil {
			return err
		}
		fmt.Printf("%-24s %d new, %d obsolete, %d/%d translated\n",
			filepath.Base(path), added, obsolete, cat.translated(), cat.active())
	}
	return nil
}

// cmdCompile writes one JSON catalogue per locale.
//
// JSON rather than shipping the .po: the browser would otherwise need a
// PO parser, and the compiled form drops everything a translator needs
// but a runtime does not - comments, source references, obsolete
// entries.
func cmdCompile(outDir string) error {
	locales, err := localeFiles()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// The runtime's list of available languages is generated from the
	// catalogues that actually exist, rather than hand-maintained in
	// two places - a .po added without its entry in i18n.js would
	// otherwise be invisible to users, with nothing to catch it.
	if err := writeLocalesModule(locales); err != nil {
		return err
	}

	var names []string
	for _, path := range locales {
		cat, err := readPO(path)
		if err != nil {
			return err
		}
		lang := strings.TrimSuffix(filepath.Base(path), ".po")

		out := filepath.Join(outDir, lang+".json")
		if err := writeJSON(out, cat); err != nil {
			return err
		}
		names = append(names, fmt.Sprintf("%s (%d/%d)", lang, cat.translated(), cat.active()))
	}

	fmt.Printf("Compiled %d locale(s): %s\n", len(locales), strings.Join(names, ", "))
	return nil
}

// writeLocalesModule regenerates frontend/src/locales.js from the
// catalogues present.
//
// It is a committed, generated file rather than something fetched at
// runtime: preferredLanguage() has to choose a language before the page
// is revealed, and a second network round-trip there would delay first
// paint for everyone.
func writeLocalesModule(locales []string) error {
	var b strings.Builder
	b.WriteString(`// Generated by frontend/i18n - do not edit.
//
// Regenerate with: cd frontend && go run ./i18n compile ../internal/web/dist/locales
//
// Lists the languages with a catalogue. English is absent on purpose: it
// is the source language, so an untranslated string already renders as
// its own English msgid.

/** Language codes with a catalogue, in the order they are offered. */
export const AVAILABLE = [
`)

	type named struct{ code, name string }
	var found []named
	for _, path := range locales {
		code := strings.TrimSuffix(filepath.Base(path), ".po")
		lang, err := findLanguage(code)
		if err != nil {
			return fmt.Errorf("catalogue %s has no entry in languages.go: %w", path, err)
		}
		found = append(found, named{lang.Code, lang.Name})
	}

	for _, f := range found {
		fmt.Fprintf(&b, "  %q,\n", f.code)
	}
	b.WriteString(`];

/**
 * Each language's name in itself - somebody looking for German is
 * looking for "Deutsch", not for "German" written in a language they may
 * not read.
 */
export const LANGUAGE_NAMES = {
  en: 'English',
`)
	for _, f := range found {
		fmt.Fprintf(&b, "  %s: %q,\n", f.code, f.name)
	}
	b.WriteString("};\n")

	path := filepath.Join("src", "locales.js")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// cmdStatus reports completeness without writing anything, for a quick
// "what still needs translating".
func cmdStatus() error {
	messages, err := extract(sourceRoots)
	if err != nil {
		return err
	}
	fmt.Printf("%d translatable string(s) in source\n\n", len(messages))

	locales, err := localeFiles()
	if err != nil {
		return err
	}
	for _, path := range locales {
		cat, err := readPO(path)
		if err != nil {
			return err
		}
		lang := strings.TrimSuffix(filepath.Base(path), ".po")
		done, total := cat.translated(), cat.active()
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		fmt.Printf("  %-6s %3d%%  %d/%d translated", lang, pct, done, total)
		if missing := total - done; missing > 0 {
			fmt.Printf("  (%d missing)", missing)
		}
		fmt.Println()
	}
	return nil
}

// localeFiles lists the .po files, sorted, excluding the .pot template.
func localeFiles() ([]string, error) {
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".po" {
			continue
		}
		out = append(out, filepath.Join(localesDir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}
