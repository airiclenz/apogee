---
title: Dragging the side-panel separator cold-repaints the transcript per column
status: open
type: task
priority: 2
labels: [perf, tui, slow-box]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: separator drag in a long session. Each held-button motion that moves the side-panel
separator by one column stores the new width (`dragPanelSeparator`,
`internal/tui/sidepanel.go:1291`). `settleFrame` (`internal/tui/model.go:3156`) lets that motion
through (`panelDragResized`, `sidepanel.go:1302`, passes every width change), `settle`
(`model.go:3136`) sees `frameKey.sidePanelWidth` (`internal/tui/paintcache.go:444`) differ from
the painted key and calls `layout` (`model.go:3050`) → `refreshViewport` (`model.go:3381`) →
`renderView` over the whole transcript. The transcript width is in every block's `paintKey`
(`paintcache.go:466`) and the paint cache keeps one paint per block (`paintcache.go:327`), so
every block misses: one cold whole-transcript repaint per column dragged. Nothing coalesces the
motion events (Bubble Tea does not merge them; the only coalescing in `internal/tui` is for token
deltas, `sink.go:74`).

Measured (Raspberry Pi 5, the same cold repaint as a one-column resize): 69.8 ms at 300 tool
blocks, 200.3 ms at 900 — per column. A 20-column drag is ~1.4 s of blocked `Update` at 300
blocks and ~4 s at 900.

Live re-wrap on every column is a ratified behaviour (`adr-0110-amend-1108` (e), pinned by
`TestMouseMotionRepaintsADraggedSeparator`), so the fix is coalescing, not dropping the live
re-wrap.

## Why

On a Pi 5–class host the separator lags far behind the pointer in a long session and keys
queue behind the drag; the drag feels broken rather than slow.

## Suggested fix

Suggestion from the audit, keeping every drawn frame one of today's frames: on a width-moving
motion, record the target width only, hold the drawn panel width at
`m.painted.sidePanelWidth`, and return a Cmd that sends a `panelDragSettle` Msg; that Msg lays
out at the latest recorded width (later ones find the key already current and do nothing). A
burst of queued motion then costs one cold repaint instead of one per column; intermediate
widths are skipped, none is drawn differently. If this is judged to break the live re-wrap rule,
revisit `adr-0110-amend-1108` rather than working around it. The structural remedy (paint the
rows the viewport shows at the new width first, the rest lazily) is shared with
`resize-cold-repaint-cost`, which makes each cold repaint cheaper.

## Acceptance

- [ ] A burst of K queued separator motions costs one cold repaint, not K — pinned by the
      `paintCache` miss counter.
- [ ] The separator still re-wraps live while dragged: `TestMouseMotionRepaintsADraggedSeparator`
      passes, plus a drag driven through `internal/tuitest` (+ `internal/stubllm` for the
      session; never an `httptest` upstream closure) whose final frame is byte-identical to a
      direct layout at that width.
- [ ] No tight deadline or fixed clock added; the settle waits on its Msg, not a timer
      (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 4 (High). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
