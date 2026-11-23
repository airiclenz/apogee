---
title: Every keystroke repaints the whole transcript
status: open
type: task
priority: 2
labels: [perf, tui, slow-box]
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: typing in the prompt box. The edit path of `handleKey` ends with `m.layout()`
(`internal/tui/model.go:2328`, to re-flow the input box as the draft wraps), and `layout`
(`model.go:3050`) always ends in `refreshViewport` (`model.go:3082` → `:3381`): a full
`renderView` walk over every block, `reserveWidgetCells`, and the viewport's re-measure of every
line (`viewport-remeasures-lines`) — although typing changes no transcript content.

Measured (Raspberry Pi 5, per keystroke with a short draft, resumed session of N tool blocks):
3.36 ms at N=0, 5.99 ms at N=48, 16.21 ms at N=300 (about +40 µs per block, so about 40 ms at
N=900 on the repaint slope). 42.5% of the typing profile's CPU is under `refreshViewport`.

## Why

On a Pi 5–class host, key echo in a long session lags, and a fast typist's keys queue behind
repaints of a transcript nobody changed — while the CPU is shared with the local model.

## Suggested fix

Suggestion, to be designed: let `layout` skip the transcript repaint when `frameKey()` still
equals `m.painted` and the viewport width is unchanged; when only the widget height moved (the
box grew or shrank a row), re-seat the offset (`GotoBottom` / `floorShortScreenOffset` / the
`detached` rule) instead of re-rendering. `refreshViewport` also re-seats the block cursor and
the selection; on identical lines those are no-ops, which the change must confirm. Keep "an arm
mutates, Update's tail lays out and repaints" (`TestArmsLeaveLayoutToTail`, ADR 0011).

## Acceptance

- [ ] Typing a printable rune in a session of 300 tool blocks causes no transcript repaint —
      pinned by a repaint/`paintCache` counter; a per-key benchmark in `internal/tui` is flat
      in N; before/after recorded.
- [ ] The input box still auto-grows and the transcript still re-fits above it, shown by a test
      driving the TUI through `internal/tuitest` + `internal/stubllm` (never an `httptest`
      upstream closure) that types a draft wrapping to a second row; frames are byte-identical.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("every keystroke repaints the
whole transcript"). Report: `docs/reviews/tui-perf-audit-2026-11-23.md`.
