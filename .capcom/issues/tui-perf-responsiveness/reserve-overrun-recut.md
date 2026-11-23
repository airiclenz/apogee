---
title: One overrunning scrollback line puts every repaint on the slow reserve path
status: open
type: task
priority: 4
labels: [perf, tui]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: many tool blocks, streaming, typing — any repaint after one scrollback line overruns
the viewport width in the widget's measure (for example a full-width row holding two or more
VS16/ZWJ clusters such as `⚠️ ✔️`, on a terminal that has not answered mode 2027).

`refreshViewport` (`internal/tui/model.go:3381-3383`) calls
`renderView(...).reserveWidgetCells(viewport width)` on every repaint with no memo. The
first-overrun scan (`internal/tui/render.go:557-565`) finds the overrunning line from stored
cells each time, so the early return at `:566` never fires again, and `:570-617` rebuilds five
O(n) slices (`lines`, `targets`, `pads`, `padSeps` and a `shift` array) for the entire
transcript and re-cuts every overrunning line with `splitAtWidgetWidth` / `ansi.Cut`
(`render.go:686-691`) plus `dropBlankTail`'s `ansi.Strip`. The result is never cached, though
the break depends only on the line and a limit fixed by the block's `paintKey`
(`bodyRightGutter` = 1, `internal/tui/theme.go:251`).

Measured: the existing `BenchmarkReserveWidgetCellsAllHit` (`internal/tui/paintcache_test.go:1029`,
fixture at `:865` — 12 lines with two VS16 clusters) takes this branch: `reserveWidgetCells` is
1.34 s cum (`splitAtWidgetWidth`/`ansi.Cut` 1.04 s) against `renderView`'s 1.39 s, and allocates
446 MB against `renderView`'s 435 MB — the slow branch roughly doubles an all-hit
`renderView` + reserve.

Severity moved from Medium to Low by the verifier: real sessions hold few overrunning lines,
`renderView` is a minority of `refreshViewport` (18.0 s of 63.6 s in the stream profile), and the
trigger needs VS16 rows filled to within one column on a terminal without mode 2027.

## Why

Once triggered, it stays for the rest of the session: every repaint on a Pi 5–class host
copies the whole transcript's line slices once more.

## Suggested fix

Suggestion from the audit, output identical (the break is per line and copies the line's own
target/pad, so applying it per block yields the same rows): perform the split inside
`transcript.paintBlock` when the paint is stored (it already measures `cells` there), so cached
blocks arrive pre-broken and the post-pass only ever sees separators, the breadcrumb and the
preview.

## Acceptance

- [ ] `BenchmarkReserveWidgetCellsAllHit` falls to about the cost of a reserve with no overrun;
      before/after recorded.
- [ ] A repaint whose scrollback holds a full-width `⚠️⚠️` row is byte-identical to today's,
      including through a test driving the TUI via `internal/tuitest` + `internal/stubllm`
      (never an `httptest` upstream closure).
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 7 (filed Medium, verified as Low). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
