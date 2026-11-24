---
title: A cold transcript repaint costs 200 ms at 900 tool blocks
status: done
type: task
priority: 3
labels: [perf, tui, slow-box]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
closed: 2026-11-24T07:01:48Z
reason: "Resolved by tui-perf-repaint-plan (2026-11-23 - 08), archived at ff4144896, item 7: cheaper cold paint leaves; N=900 resize 166.6 -> 109.9 ms."
---

## Problem

Scenario: resize (a terminal resize; also every column of a side-panel separator drag, see
`panel-drag-cold-repaints`). The transcript width is in every block's `paintKey`
(`internal/tui/paintcache.go:466`), so a width change makes every block miss and repaint cold:
`tea.WindowSizeMsg` (`internal/tui/model.go:1269`) → `layout` → `refreshViewport` →
`renderView` → `paintBlock` (`paintcache.go:507`) for every block. The cache keeps one paint per
block (`paintcache.go:327`), so resizing back misses again.

Measured (Raspberry Pi 5, one-column width change, resumed session of N tool blocks):
13.9 / 69.8 / 200.3 ms at N = 48 / 300 / 900 (about 220 µs per block), 3.0 / 7.4 / 17.7 MB and
18.8k / 88.3k / 252.6k allocs. CPU at N=300: `renderSuperGroup` (`internal/tui/toolblock.go:176`)
43.6% cum, `leaderRowIn` (`internal/tui/toolleader.go:168`) 26.4%, `umbrellaHeader` 22.7%,
`renderToolBlock` 20.8%, lipgloss `Style.Render` 39.7% cum, `measuredCells` 9.1%.

## Why

A resize in a long session freezes a Pi 5–class host for about 200 ms per event, and a terminal
emits many resize events while its window edge is dragged, so the stall adds up to seconds.

## Suggested fix

Suggestion; leads from the profile and the audit's lenses, not individually verified:

- lipgloss `Style.Render` is used as a span painter for theme roles that are bare SGR styles
  (`internal/tui/theme.go:588`), yet each call runs the full block pipeline, including a width
  pass in `applyMargins`; a precomputed SGR open/close pair per role (or `Inline(true)` where
  every input is single-line) would skip it;
- `indicatorRow` (`toolblock.go:528`) re-measures leader rows `leaderRowIn` built to a known
  width — return the cell count instead;
- structurally, paint the rows the viewport shows at the new width first and the rest lazily
  (an architecture-pass candidate, shared with `panel-drag-cold-repaints`).

Output must stay byte-identical (no visual change; `layout.md`, `docs/layout/`).

## Acceptance

- [ ] A resize benchmark in `internal/tui` at 900 tool blocks is measurably below 200 ms;
      before/after recorded.
- [ ] Every changed paint helper is proven byte-identical (e.g. each theme role rendered both
      ways over a corpus including tabs and SGR-carrying text), and a resize driven through
      `internal/tuitest` + `internal/stubllm` (never an `httptest` upstream closure) yields
      the same frame as today.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("a cold repaint costs 70 ms at
N=300 and 200 ms at N=900"). Report: `docs/reviews/tui-perf-audit-2026-11-23.md`.
