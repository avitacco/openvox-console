# Translating the OpenVox Console

The console's interface is translated with gettext catalogues. Each
language has a `.po` file here, editable with any translation tool —
[Poedit](https://poedit.net), [Weblate](https://weblate.org), or a text
editor.

**You do not need to translate everything.** Any string without a
translation renders in English, so a partial catalogue is safe to ship
and improves as it fills in.

## Getting started

Pick your language's file — `de.po`, `es.po`, `ja.po` and so on — and
fill in the `msgstr` lines:

```po
#: src/nodes.js:118
msgid "Add node"
msgstr "Node hinzufügen"
```

`msgid` is the English source text and is also the lookup key. Never edit
it: changing it disconnects the entry from the code, and the string
reverts to English.

## Terminology

Terms that the OpenVox and Puppet communities use in English are usually
better left in English, even in an otherwise translated interface —
practitioners search for them, read them in logs, and use them in
conversation. In German these were kept as-is:

> Node, Job, Task, Plan, Fact, Report, Deploy, Dashboard, Environment

When a term stays English, write it as its own translation rather than
leaving it blank:

```po
msgid "Node"
msgstr "Node"
```

Blank means "nobody has looked at this yet". Writing it out means
"somebody decided". The compiled catalogue drops identical pairs, so this
costs nothing at runtime.

Which terms to keep is a judgement for each language, and yours is better
than ours — if "Knoten" reads better than "Node" to the people who will
use this, use it.

## Placeholders

Some strings carry `{name}` placeholders, replaced at runtime:

```po
msgid "Showing {shown} of {total} packages"
msgstr "{shown} von {total} Paketen angezeigt"
```

Keep every placeholder, spelled exactly the same. They can be reordered
freely to suit your grammar. A missing placeholder means the value simply
does not appear.

## Plurals

Strings that vary with a count have a `msgid_plural` and one `msgstr[N]`
per form your language uses:

```po
msgid "{count} node selected"
msgid_plural "{count} nodes selected"
msgstr[0] "{count} Node ausgewählt"
msgstr[1] "{count} Nodes ausgewählt"
```

How many forms, and which count maps to which, comes from the
`Plural-Forms` header at the top of your file — already set correctly.
Russian and Polish have three forms, Chinese and Japanese have one.

**Fill in every form.** A plural entry with any form left blank is
treated as untranslated and falls back to English entirely, because
showing an empty string for one particular count is worse than showing
English for all of them.

## Checking your work

```sh
cd frontend
go run ./i18n status    # completeness per language
./build.sh              # compile catalogues into the console
```

A translation marked `#, fuzzy` counts as untranslated and is not
shipped. That is deliberate: fuzzy means a tool guessed, and a confident
wrong translation is worse than English. Remove the flag once you have
checked the entry.

## Adding a language

```sh
cd frontend
go run ./i18n add <code>
```

The code must first appear in `frontend/i18n/languages.go` with its
`Plural-Forms` rule and its name written in that language. The rule is
not optional — an incorrect one produces grammatically wrong output that
renders perfectly and nobody reports.

Right-to-left languages (Arabic, Hebrew, Farsi, Urdu) need layout support
in the voxblocks design system before they can be added; translation
alone is not enough.

## When the English changes

```sh
cd frontend
go run ./i18n extract   # rescan the source
go run ./i18n merge     # fold new strings into every .po
```

New strings arrive untranslated. A string removed from the code is
commented out rather than deleted, so its translation survives if it
comes back.
