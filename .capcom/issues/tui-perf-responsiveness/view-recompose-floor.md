---
title: Every frame pays about 2.2 ms of View composition with nothing changed
status: open
type: task
priority: 3
labels: [perf, tui]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: idle, scroll — every frame. `View` (`internal/tui/model.go:3731`) recomposes every
row on every call; there is no frame-level cache. So it is paid on every Msg, including a
spinner tick that changes only the status line.

Measured (Raspberry Pi 5, `View` alone on an unchanged model): 2.11 / 2.20 / 2.67 ms, about
720 KB and 5,260 allocs per frame at 48 / 300 / 900 tool blocks; a settled spinner tick costs
2.7 ms, a wheel notch 3.4 ms, PgUp/PgDn 3.0 ms, all flat in session size. CPU (N=300):
bubbles `viewport.Model.View` 24.6% (a lipgloss Width/Height render of the visible rows),
`inputView` 14.9%, `joinFrame` (`internal/tui/boxdraw.go:237`) 14.9%, `squareLine`
(`boxdraw.go:27`) 13.2%, `lipgloss.Wrap` 12.1%, `sealRowEdges` (`boxdraw.go:276`) 8.3%,
`joinScrollbar` (`boxdraw.go:183`) 8.0%, footer 7.3%; lipgloss `Style.Render` 36.7% cum.

Each visible transcript row is width-measured about five to six times per frame: in
`vp.View()`'s lipgloss pass, by `squareLine` in `joinScrollbar` (`boxdraw.go:194`), again in
`joinColumn` (`:218`) while the side panel shows, again in `joinFrame` (`:241`), plus
`sealRowEdges`' parse of rows carrying a background SGR. The scroll bar renders its two constant
glyphs once per bar row (`th.scrollThumb.Render` / `th.scrollTrack.Render`,
`boxdraw.go:168-170`).

Filed separately and not counted here: the Model heap copies behind ~55% of `View`'s bytes
(`open-panes-heap-copies`) and the footer's transcript walk (`footer-spend-walks-transcript`).

## Why

At the spinner's 10–20 frames a second for the whole of every Turn, a 2.2–2.7 ms floor is
about 5% of a Cortex-A76 core plus its garbage, spent redrawing a screen that did not change —
on a box that is also running the model.

## Suggested fix

Suggestion; leads from the profile and the audit's lenses, not individually verified:

- compose the transcript body from the visible window of `m.lines` (offset and height are known,
  and `reserveWidgetCells` already bounds every line) with one `squareLine` per row, instead of
  `vp.View()`'s lipgloss pass;
- carry the known row width from `joinScrollbar` / `joinColumn` into `joinFrame` so it skips
  re-squaring rows already squared to `m.width`;
- memo `sealRowEdges` for chrome rows that repeat frame to frame (status, footer, input box) in a
  pointer-held memo keyed by (line, width);
- render the scroll bar's thumb and track glyphs once, not per row.

Byte identity must be proven against today's output, including VS16 rows (lipgloss pads in
GraphemeWidth and `squareLine` corrects).

## Acceptance

- [ ] The `View`-alone benchmark on an unchanged model is measurably below 2.2 ms and 720 KB
      per frame; before/after recorded.
- [ ] A test compares the composed frame against today's path on a fixture holding wide and
      VS16 graphemes and a short tail, and frames driven through `internal/tuitest` +
      `internal/stubllm` (never an `httptest` upstream closure) are byte-identical.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("View costs about 2.2 ms and
720 KB per frame on an unchanged model, flat in N"). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
