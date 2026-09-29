# Library fixture for `Suggest`

A synthetic, library-sized skill catalog: 22 made-up skills written for these tests. Each
`skills/<id>/SKILL.md` holds frontmatter (name, description, the odd argument hint) over a one-line
placeholder body. `Suggest` never reads bodies; `suggest_library_test.go` and the TUI's
`TestSuggestBandPrecision` care only about id, display name, description (indexed in full — the
200-rune summary clamp is the "/" menu's alone) and triggers. Loaded through the ordinary
`Load(Sources{Home: "testdata/library"})`.

The descriptions are shaped for the rows they pin:

- the positive rows each have a skill that shares several content words with the draft, and
  `project-briefing` places its phrase (`get me up to speed`) past the 200-rune summary cap;
- the generic-edit rows each have a skill that shares one content word with the draft plus a
  dev-generic stopword (`add`, `file`, `files`) — e.g. `feature-flags` for "add a flag to the
  command line parser", `csv-import` for "add the missing import to this file", `log-search` for
  "split this file into two smaller files" — so a stopword or cutoff regression makes those rows
  return something again.

Every body must be exactly `Synthetic fixture skill — see testdata/library/README.md.` —
`TestLibraryFixtureIsSynthetic` fails on any other.
