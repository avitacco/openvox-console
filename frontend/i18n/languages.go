package main

import "fmt"

// Known languages and how they form plurals.
//
// The rules are gettext's standard Plural-Forms expressions. They are
// recorded here rather than left to whoever creates a catalogue, because
// a wrong rule is invisible: the page renders and only the grammar is
// wrong, in a language the person who added it probably does not read.
//
// Name is the language's name in itself - somebody looking for German is
// looking for "Deutsch", not for "German" spelled in a language they may
// not read.
type language struct {
	Code        string
	Name        string
	PluralForms string
}

// Two forms, one for n==1: English, German, Spanish, Italian and most of
// Germanic and Romance Europe.
const pluralTwoNotOne = "nplurals=2; plural=(n != 1);"

// Two forms, but zero takes the singular: French, Brazilian Portuguese.
const pluralTwoGreaterOne = "nplurals=2; plural=(n > 1);"

// One form, no count distinction at all: Chinese, Japanese, Korean,
// Indonesian, Turkish.
const pluralOne = "nplurals=1; plural=0;"

// Three forms, the East Slavic rule: Russian, Ukrainian.
const pluralSlavic = "nplurals=3; plural=(n%10==1 && n%100!=11 ? 0 : " +
	"n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);"

// Three forms, Polish - differs from the East Slavic rule at n==1.
const pluralPolish = "nplurals=3; plural=(n==1 ? 0 : " +
	"n%10>=2 && n%10<=4 && (n%100<10 || n%100>=20) ? 1 : 2);"

// languages are the locales the console can be built with. English is
// the source language: it has no catalogue, because an untranslated
// string already renders as its own English msgid.
//
// Adding one here and running `i18n add <code>` creates its catalogue;
// the runtime's own list lives in frontend/src/i18n.js.
var languages = []language{
	{"de", "Deutsch", pluralTwoNotOne},
	{"es", "Español", pluralTwoNotOne},
	{"fr", "Français", pluralTwoGreaterOne},
	{"pt", "Português", pluralTwoGreaterOne},
	{"it", "Italiano", pluralTwoNotOne},
	{"pl", "Polski", pluralPolish},
	{"ru", "Русский", pluralSlavic},
	{"uk", "Українська", pluralSlavic},
	{"tr", "Türkçe", pluralOne},
	{"hi", "हिन्दी", pluralTwoNotOne},
	{"id", "Bahasa Indonesia", pluralOne},
	{"zh", "中文", pluralOne},
	{"ja", "日本語", pluralOne},
	{"ko", "한국어", pluralOne},
}

// findLanguage looks up a language by code.
func findLanguage(code string) (language, error) {
	for _, l := range languages {
		if l.Code == code {
			return l, nil
		}
	}
	return language{}, fmt.Errorf("unknown language %q - add it to languages.go with its Plural-Forms rule first", code)
}
