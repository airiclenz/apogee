---
title: "ADR 0043 amendment of 2026-08-21: the registry row carries the value; the pane keeps the presentation"
---

Decision 3 above draws the line at the file: `settingsrows.go` and `settingsedit.go` stay in the
binary as the /settings **display projection**. They do stay. What moved is a narrower thing the
sentence lumped in with them — "the config file's own spelling of a value". Three tables in those
two files answered, per key, what the resolved config was holding: `settingValues` (one closure per
registry row), `settingTexts` (the prose behind a summary) and `settingStructures` (the lossless
value behind a structured summary). Each needed a test walking the registry to prove it had an entry
for every row, and each was a place a key added to the schema could be forgotten.

**Those three answers are now three fields on the registry row** — `Key.Read`, `Key.Text`,
`Key.Structure` — and the tables are loops over `KeyRegistry`. The three cover tests collapse into
one row invariant (`TestRegistryRowsProjectEveryValue`): every row reads, prose rides exactly the
text rows, a lossless value rides exactly the structured rows. A key described in the registry is
rendered by the act of being described.

**The line the record draws moves from the file to the fact.** How a key's value is SPELLED —
`true`, `[AGENTS.md]`, `1m30s`, "3 servers" — is a fact about the schema, and it is the same string
the splice writer verifies a written value against; the two spellings were literally duplicated
across the seam before this, with comments on both copies explaining why. How a row is DRAWN —
which section it groups under, the mask over a secret, the word an empty block shows, which of the
pane's edit idioms its kind gets, when ⏎ opens `$EDITOR` — stays in the binary, and that is the
whole of what those two files now do. ADR 0011's thin renderer is untouched: what crosses the seam
is still a list of rows a pane can paint. The rejected alternative above still stands as rejected —
moving `settingsrows.go` / `settingsedit.go` themselves would put renderer-shaped output in the
config package, and none of that output moved.
