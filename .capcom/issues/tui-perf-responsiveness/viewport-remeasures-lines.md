---
title: The transcript viewport re-measures every scrollback line on every repaint
status: open
type: task
priority: 2
labels: [perf, tui]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: every transcript repaint — streaming, typing, fold, live-star blink, resize.
`refreshViewport` (`internal/tui/model.go:3381`) hands the whole paint to the bubbles viewport
with `viewport.SetContentLines` (`model.go:3401` when detached, `:3413` when following;
`charm.land/bubbles/v2` v2.1.0 `viewport.go:233`). That runs `maxLineWidth`
(`viewport.go:759`) — `ansi.StringWidth` over every line of the scrollback — plus a
`strings.ContainsAny` scan of every line. The widths are already known: the paint cache stores
each block's widget widths (`cells`), and `reserveWidgetCells` (`internal/tui/render.go:541`)
has already held every line to the viewport width.

Measured (Raspberry Pi 5, resumed session of N tool blocks, 120x40; N=300 ≈ 3,700 lines,
N=900 ≈ 11,000):

- 61% of an all-cache-hit repaint (N=300: 1.62 s of 2.65 s cum); `renderView` itself is 14.7%.
- 45% of streaming (`maxLineWidth` 41.4 s of 92.1 s), 26% of typing, 37.5% of a fold toggle.
- It is why the all-hit repaint is linear at about 41 µs per tool block: 4.38 / 14.82 /
  40.57 ms at N = 48 / 300 / 900.

## Why

Every path that ends in `refreshViewport` inherits this cost (see
`keystroke-repaints-transcript`, `delta-repaints-transcript`): on a Pi 5–class host a long
session makes every key, every streamed flush and every fold pay tens of milliseconds to
re-measure text that has not changed.

## Suggested fix

Suggestion, to be designed (the audit names the cost, not a fix): stop the widget re-measuring
lines whose widths apogee already knows. Options to weigh: hand the widget only the visible
window of `m.lines` (offset .. offset+height) and keep the scroll state in apogee; or, since the
transcript viewport runs with `SoftWrap=false` (`model.go:789`), check what the widget uses
`longestLineWidth` for and feed it the known maximum instead. The lines and offsets handed to
the screen must stay the same, so output is byte-identical (no visual change; `layout.md`,
`docs/layout/`). Bubbles internals are cited from the module cache; an upgrade moves those line
numbers.

## Acceptance

- [ ] An all-cache-hit repaint does no width pass over unchanged lines — a profile shows no
      `maxLineWidth` time, and a repaint benchmark in `internal/tui` at 900 tool blocks is
      measurably below 40.6 ms; before/after recorded.
- [ ] Follow-the-tail, detached scrolling, PgUp/PgDn, wheel and the block cursor behave as
      before, shown by tests driving the TUI through `internal/tuitest` + `internal/stubllm`
      (never an `httptest` upstream closure); frames are byte-identical.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("the viewport re-measures
every transcript line on every repaint"). Report: `docs/reviews/tui-perf-audit-2026-11-23.md`.
