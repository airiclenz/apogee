# Split diff layout

This file is the canonical diff-body layout spec (grill session 2026-08-19),
ratified by [ADR 0052](../adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md).
It replaces the `siff-layout.md` sketch. `tool-layout.md` keeps the block
grammar — headers, slots, fold states — and its per-tool table's diff rows point
here for what the expanded body paints. Terms (Split diff, Stacked diff, Edit
regions) are `CONTEXT.md`'s.

Scope: the expanded body of the six diff-bodied blocks — `write_file`,
`edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace`,
`view_diff`, `git_diff_range`. `write_file` belongs here on the same rule as the
other three writing tools: it attaches Edit regions as it applies the change
(ADR 0052), so an overwrite or a fresh create reads exactly the way an edit does. Collapsed blocks paint no body (`collapsedBodyRows = 0`) and
are untouched.

## Rules

- **One body, two readings.** The same regions — up to 3 merged context lines
  each side of a change — paint as a **Split diff** when width allows, and as a
  **Stacked diff** below that. The information is identical in both; only the
  arrangement differs.
- **Width rule, per pane.** Split paints only when each pane gives the code
  ≥ 40 columns after its number gutter and 2-column marker. Named constant;
  in practice terminals ≥ ~100 columns. Below it: Stacked.
- **Panes.** Left = before (its own line numbers; removed rows wear `-` and sit
  on a `diff-del-bg` red band). Right = after (its numbers; added rows wear `+`
  and sit on a `diff-add-bg` turquoise band). A banded row's TEXT keeps the
  expanded body's open tone, like the context rows, which appear in both panes
  unmarked and unbanded. The band runs from the marker column to that pane's
  EDGE — under a short line's trailing space and under a continuation row's text
  alike — while the number gutter beside it stays chrome.
- **Alignment.** Within a region the removed block and the inserted block start
  on the same row; the shorter side pads with blank rows. Discontiguous regions
  are separated by one damped `⋯` rule row spanning both panes.
- **Wrap, don't clip.** A line wider than its pane continues on further rows;
  continuation rows carry no number and no marker. Both panes stay row-aligned
  (the other side pads). Wrapping is computed at paint time against the width
  authority (ADR 0030), so a resize re-flows and can flip the reading.
- **File sections.** A body whose diff spans several files (`git_diff_range`
  prints one) paints one muted header row naming the file above that file's
  regions, in the order the tool printed them — in BOTH readings. A body that
  names no file (the edit tools, `view_diff`) paints no such row. Output the
  renderer cannot walk whole — a binary or rename-only section, a `--stat` or
  `--name-only` call, git's "No differences found" — keeps the plain output
  rendering it always had, in whole and never mixed with parsed sections.
- **Gutters.** The number gutter is as wide as the widest number shown in that
  body — in a multi-file body, in that file's section, since each file numbers
  its own lines — right-aligned. Numbers, the gutter, and the center divider `│` wear
  `muted`. Marker column: `-` / `+` on the first row of a changed line, blank
  on context and continuations.
- **Color never carries a change alone.** The markers are the palette-proof
  signal, glyphs riding the band rather than coloured marks of their own;
  `diff-add-bg` is turquoise (not green) in both shipped schemes so the pairing
  with red survives red-green-weak vision, as does the `diff-add` / `diff-del`
  foreground pair the bands are drawn from. `success` sits in the same turquoise
  family, a visible step from `diff-add`.
- **Sources.** Edit tools: recorded Edit regions (typed summary, apply-time).
  `view_diff`: numbers counted from line 1 over its whole-file body, then
  trimmed to regions — expanded `view_diff` no longer paints the entire file.
  `git_diff_range`: numbers from git's `@@` headers, one section per file its
  `diff --git` lines name. No summary → the old argument-derived `-`/`+` list,
  unchanged.

## Split diff (wide)

```
┌─┶ Edit ⋯ internal/tui/render.go ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ +2 −1 ▼
│   88   func paint(w int) error {      │  88   func paint(w int) error {
│   89     if w < minWidth {            │  89     if w < minWidth {
│   90 -    return errNarrow           │  90 +    return fmt.Errorf("width %d
│                                       │           under %d", w, minWidth)
│   91     }                            │  91     }
│  ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯
│  204     return nil                   │ 205     return nil
│  205 - }                              │ 206 +  }
│                                       │ 207 +
    see less…
```

- row 90: one removed line left (red band), its replacement right (turquoise
  band) wraps to a continuation row — no number, no marker — and the left pane
  pads.
- the `⋯` rule row separates two regions whose context does not touch.
- numbers drift apart across the rule (before 204 / after 205): each pane
  numbers its own file.

## Stacked diff (narrow)

```
┌─┶ Edit ⋯ internal/tui/render.go ⋯⋯⋯⋯⋯⋯ +2 −1 ▼
│    88   func paint(w int) error {
│    89     if w < minWidth {
│    90 -    return errNarrow
│    90 +    return fmt.Errorf("width %d under
│             %d", w, minWidth)
│    91     }
│   ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯
│   204     return nil
│   205 -  }
│   206 +  }
│   207 +
     see less…
```

- per region: context, then `-` rows (before numbers), then `+` rows (after
  numbers), then trailing context. Same wrap rule, same `⋯` separator.
- Same chrome rule too: the band runs from the marker column to the block's wrap
  rail, on the first row of a line and on its continuation rows alike, while the
  number gutter left of it stays chrome. A continuation row hangs under that
  gutter, so a wrapped line shows one number.
