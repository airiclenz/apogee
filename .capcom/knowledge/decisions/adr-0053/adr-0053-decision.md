---
title: ADR 0053 decision
---

**Every popup surface that is a LIST embeds one `listSurface`, and a pane's own code is its rows,
its accept and its wording.** The module is `internal/tui/listsurface.go`
(ADR 0043: one file per concern, one
`doc.go` line).

**1 — The surface owns the state a list keeps between keypresses, and nothing else.** That is two
values: where the highlight stands, and what has been typed into the filter. Everything a list shows
is derived per frame from the state it describes and handed IN — a beat carrying a shorter offering
or a re-listed session store refreshes the pane in place, and the surface holds nothing that could
go stale against it.

**2 — The surface owns the key contract every modal list shares.** esc, ↑/↓ (and ^p/^n),
type-to-filter with backspace as its undo, and the ⏎ that lands on a row. Both clamps of a
keypress — before anything acts, and again after a key that moved the filter — read the same
composition of the list, so the count a key is measured against and the count it leaves behind can
never come from two different readings.

**3 — The surface decides nothing about what a key MEANS.** It answers with a **verdict**
(`listCloses`, `listAccepts`, `listSwallowed`, `listUnclaimed`) and the pane spends it. Closing an
overlay, resuming a session, rebinding a model are the pane's own acts with the pane's own
consequences; `listUnclaimed` is what leaves room for a pane's own verbs (the browser's ^r / ^d / ^a)
without this module knowing they exist.

**4 — What ↑/↓ do at the ENDS is a parameter, not a rule.** `listWrapsAround` / `listStopsAtEnds` is
passed per call, and every pane keeps exactly the answer it had: the picker and the /sessions browser
wrap, the approval and ask selections stop. The review found four undocumented answers; this makes
the difference a stated argument at the call site rather than an idiom re-spelled per pane.

**5 — Every accept resolves through the filter.** The highlight indexes the FILTERED rows, and
`pickerView.offeringIndex` maps it back to the list underneath before anything is touched. A pane
whose list can move between the two reads of one keypress still bounds-checks what comes back.

**6 — The filter LINE rides the surface, not the painter's spec.** Its label, its caret, its two
blank lines, the budget claim for all three, and the trade a short window makes (the rows shrink, the
line the human is typing stays) are stated once in `renderList`. A pane states its rows, its title,
its hint and its slot in the frame; it cannot forget the pads or set them out of step with the claim.

**7 — It is a plain value, embedded.** No lock, no self-pointer, no no-copy type, so it lives inline
in the `Model` that is copied on every `Update`
(ADR 0011). It is embedded rather
than named, so `m.picker.selected` and `m.picker.filter` still read as the pane's own fields. Its
zero value is "nothing typed, first row highlighted" — the state a whole-struct reset
(`m.picker = picker{}`) already leaves behind, so no close path needs to learn about it.

**8 — Which panes.** Every list: the picker (all seven `pickerKind`s), the /sessions browser (all
three modes), the /settings sub-lists, autocomplete's two variants, and the approval and ask
selections with wrapping off. The read-only **reports** (/usage, /inspect) deliberately stay on
`reportPane`: a report has no selection and no filter, it is scrolled rather than walked, and forcing
it here would hand it two fields it never reads.

**9 — A pane with several sub-lists NAMES its surfaces; it does not embed several.** A surface's
filter is a `lineEditor`, and a `lineEditor` is a `textarea` — thousands of bytes in a `Model` that
is copied on every `Update`. Adopting this module must move fields, never multiply them: a pane whose
sub-lists cannot share one surface holds them as named fields and reuses the editor it already has.
