---
title: TUI performance and responsiveness — review and refactor
status: open
type: task
priority: 3
created: 2026-11-22T16:52:23Z
---

## Problem

No recent pass has measured how the TUI behaves under load: long
streaming replies, big tool blocks, folds opening and closing, fast
scrolling in a long session. Cost is still paid in the hot path even
where a cache exists (the side-panel resume rebuild was reworked in
`panel-resume-tui-fold`, but the render path itself has not been
reviewed). A throttled box can make each of these visible as dropped
frames or laggy keys.

## Why

- apogee targets small, locally hosted models on modest hardware
  (a Raspberry Pi 5 is a first-class host), so every avoidable
  per-frame cost matters.
- Responsiveness problems show up as jank only under real load, so
  they are found late, one defect at a time, after users hit them.
- The Bubble Tea `Model` is copied by value on every `Update`; a
  no-copy type held by value anywhere it reaches is a standing hazard
  (ADR 0011).

## Scope

A review-and-refactor task, not a feature change:

1. Profile the render and `Update` path under realistic load: long
   streaming reply, many tool blocks, fold open/close, fast scroll.
2. Fix what the profile shows, cheapest first. Likely candidates:
   rebuild-per-frame instead of incremental; unbounded re-renders
   while streaming; per-frame allocations in the hot path.
3. Keep the fix behind the existing structure — do not redesign the
   surface.

## Non-goals

- No visual or interaction change; layout stays as specified in
  `layout.md` and `docs/layout/`.
- No new features or modes.

## Acceptance

- [ ] A profile under load exists (before/after), and the dominant
      per-frame costs found are listed with their fix status.
- [ ] The fixes are covered by tests in `internal/tui` that drive the
      TUI through `internal/tuitest` + `internal/stubllm` (never an
      `httptest` upstream closure; rule: `capcom know show adr-0062-amend-1118`).
- [ ] Any deadline or budget added waits on a condition with a
      generous backstop, not a tight clock (slow-box rule).
- [ ] `make test` passes on this machine, run as
      `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` (never a raw
      `go test -race` over a whole heavy package).
- [ ] `layout.md` and `docs/layout/` still describe the rendered
      output; any drift is fixed in the same change.

## Notes for whoever picks this up

- TUI stack: Bubble Tea v2 (`charm.land/bubbletea/v2`); key events
  are `tea.KeyPressMsg` matched on `msg.String()`.
- Neighbouring defects already filed: `snapshot-frame-cache` (test
  side, done) and `bottom-chrome-erased-cells` (render side).
- Design the approach in a plan under `docs/plans/` before starting
  (`capcom update <id> --plan '<basename>'`), and read
  `docs/design/test-drivers.md` before writing a new test.

## Log

- 2026-11-22 Airic Lenz: Filed from owner request, 2026-11-22: review and improve the TUI for performance and responsiveness as a refactoring task.
- 2026-11-23 Airic Lenz: TUI performance audit 2026-11-23 (docs/reviews/tui-perf-audit-2026-11-23.md) filed 13 child issues: footer-spend-walks-transcript, at-file-walk-blocks, thinking-pane-rebuilds-rows, panel-drag-cold-repaints, panel-sections-every-frame, delegation-key-copies-span, reserve-overrun-recut, viewport-remeasures-lines, keystroke-repaints-transcript, delta-repaints-transcript, open-panes-heap-copies, resize-cold-repaint-cost, view-recompose-floor.
