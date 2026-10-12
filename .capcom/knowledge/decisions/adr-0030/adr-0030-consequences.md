---
title: ADR 0030 consequences
---

- **The rule for `internal/tui`, from here on: measure with `th.measure`.** `lipgloss.Width`,
  `ansi.StringWidth` and `runewidth.RuneWidth` do not belong in this package except inside a
  widget mirror that names its oracle in a comment. `internal/tui/doc.go` narrates `width.go` as
  the one measure; `layout.md` says which width its rules are stated in.
- **`layout.md`'s "rendered width" and "absolute width cap" now name the painted measure.** The
  cap is asserted where it is claimed — in painted cells — rather than in the measure that was
  producing the overflow.
- **`go.mod` gained two direct requirements**, both already in the build:
  `github.com/charmbracelet/ultraviolet` (the paint harness imports it) and `github.com/rivo/uniseg`
  (the caret mirrors call the textarea's own measure). No version changed.
- **A dependency bump is a re-verification event, and it fails loudly.** `ansi.StringWidth("⚠️") == 2`
  with the wcwidth measure of the same string `== 1` is pinned in `paint_test.go`; the `Cut`
  workaround and the `wrapText` cap enforcement each have their own pin naming the upstream
  defect they stand in for. If any of those change, the tests say so before the layout does.
- **Four measurement sites are deliberately still unconverted**, all of them safe for the cap
  and none of them silent: `popup.go:185, 212, 293` and `interject.go:393` belong to plan
  `2026-07-31 - 01` while it is live, and `wrapText`'s own `ansi.Wrap` plus `truncateToWidth`
  wait on the same file. A wrap or clip computed in grapheme width never paints *wider* than its
  limit under wcwidth, so the absolute cap holds regardless; what remains possible is a popup
  column being a cell off on a row carrying VS16. Tracked in `ISSUES.md`.
  *(The `popup.go` and `interject.go` sites were converted on 2026-08-03 once plan
  `2026-07-31 - 01` was archived: `truncateToWidth` now cuts as well as measures with the
  authority, and the drift each left possible is pinned under both methods in `paint_test.go`.
  `wrapText`'s own `ansi.Wrap` is the one site still standing, and stays tracked in `ISSUES.md`.
  The rule above is unchanged.)*
  *(Correction, 2026-08-03: `wrapText` was converted the same day — it breaks with
  `th.measure.Wrap` now, so the break is chosen in the measure §7's cap is enforced in and
  `render.go` reaches for no hard-wired `x/ansi` helper at all. **No site this ADR named is
  unconverted any more.** The user block's own padding moved with it, off `lipgloss.Style.Width`
  and onto `squareLine` for §5's reason: a `Width` style does not merely pad in GraphemeWidth, past
  its width it wraps, and a line the authority calls exactly the block width can be wider than that
  to lipgloss. What `ISSUES.md` still tracks is a different residue this uncovered — the pop-up
  pane's frame, which is composed by `lipgloss.Style.Width` end to end and so folds such a line
  into two pane rows. That fold is pre-existing and reachable through pop-up rows, which never
  wrapped; it is why the pop-up body is the one wrapped surface not yet following the painter.)*
  *(Closed 2026-08-03: the fold is fixed, and it was **two** sites of one defect class rather than
  the one this note named — the pop-up pane (`blackFill`, the selected row's `userBlock.Width`, and
  `popupBorder.Width`) and the start-up card (`startupBorder.Width`, both layouts). Both now frame
  their content with `drawBox` (§5's dated note), which pads and cuts every row with `th.measure`
  and emits the border and padding itself, so one composed row is one painted row.
  `lipgloss.JoinVertical` left the pop-up with it, being the same GraphemeWidth pad one level in.
  `TestPaintedBoxRowsAreNotFolded` (`paint_test.go`) pins composed-rows == painted-rows, and each
  row's right border, for both surfaces under both width methods. The one `lipgloss.Style.Width`
  left in the package is the prompt box's (`inputView`), which frames a widget that wraps in
  GraphemeWidth itself — §6's mirror exception, correct where it stands.)*
- **`CHANGELOG.md`'s v1.1.0 `caretTo` entry claimed the fix used "the same width the widget's own
  cursor math uses", which was not true.** Shipped history is not rewritten; the entry carries a
  dated correction pointing at the real fix.
