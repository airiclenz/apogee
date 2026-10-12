---
title: Split diff / Stacked diff
aliases: [stacked-diff]
---

**Split diff** / **Stacked diff**:
The two readings of the one change body every diff-bodied tool block shares. A **Split diff**
is the two-pane reading: before on the left with its own line numbers and `-`-marked removals,
after on the right with its numbers and `+`-marked additions, unchanged context on both sides,
the panes row-aligned and long lines wrapped in place. A **Stacked diff** is the same regions
read vertically — context, removals, insertions, numbered the same way — and is what a
terminal too narrow for two readable panes shows. **Width decides between them** (a per-pane
minimum, not a magic terminal width), and **color never carries a change alone**: the `-`/`+`
markers stay, and additions wear turquoise rather than green so the pairing with red survives
red-green-weak vision. Fed by Edit regions for the edit tools; layout in
`docs/layout/split-diff-layout.md`. See
ADR 0052.
_Avoid_: "siff" (retired working name), "side-by-side view" (that is the split reading only,
not the pair), "unified diff" for the stacked reading (unified is the wire format `view_diff`
emits; stacked is a rendering).
