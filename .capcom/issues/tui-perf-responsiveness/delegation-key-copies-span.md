---
title: Collapsed sub-agent blocks copy every hidden entry to build their paint key
status: done
type: task
priority: 3
labels: [perf, tui]
plan: 2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md
created: 2026-11-23T19:29:39Z
closed: 2026-11-23T21:41:48Z
reason: item 7 resolved by plan '2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan' (archived at 2fb94a24b)
---

## Problem

Scenario: many tool blocks, streaming, fold — any transcript repaint in a session that used
sub-agents. `renderView`'s walk (`internal/tui/render.go:469-480`) calls `resolveBlock` for
every block, then builds the block's `paintKey` before `paintBlock` looks it up. To build that
key, `appendInputs` (in `resolveBlock` / `resolveGroup`, `render.go:957`, `:1002`, `:1145`)
materialises one ≈832 B `paintInput` record per entry the block covers. A collapsed Sub-agent
run covers `head+span+1` entries (`render.go:1002`), and a Sub-agents group covers
`cover := b.end - head` (`render.go:1143-1145`, `end` set by `toolBlockAt`,
`internal/tui/blockshape.go:341`) — the delegates' whole elided spans. Each record is built
from a by-value copy of the `entry` through the value-receiver `entry.painted`
(`internal/tui/paintcache.go:124`), and `spanFlags` / `spanFills` (`paintcache.go:244-297`,
called at `:474-475`) allocate a len(span)-byte string and scan the span — all before the
cache hit is known. The only mitigation is the reused `records` buffer (`render.go:446-450`),
which removes the per-block allocation but not the per-entry copy.

Cost: O(entries inside collapsed delegations) per repaint — per keystroke, per streamed flush,
per blink flip — for entries that paint no row. Magnitude not measured: the audit's fixture
had no delegations.

## Why

Sub-agent runs can hold hundreds of nested calls each. Collapsed, they show one row, yet every
repaint of a long session on a Pi 5–class host keeps copying all of them.

## Suggested fix

Suggestion from the audit; the key's fields do not change, so output is identical: compute the
key's per-entry terms (head kind/depth, span length, flag bytes, fill readings) by a pointer walk
over `t.entries[head:end]` with no record copy, make `painted` take `*entry`, and state the
`ins` records only inside `draw`, i.e. on a cache miss.

## Acceptance

- [ ] A repaint benchmark in `internal/tui` over a resumed session holding a Sub-agents group of
      4 delegates × 300 nested calls records the all-hit repaint before and after; after, its
      allocs/op do not grow with the number of elided entries.
- [ ] Paint keys are unchanged (same hits/misses on the `paintCache` counters), and a test
      driving the TUI through `internal/tuitest` + `internal/stubllm` (never an `httptest`
      upstream closure) with a collapsed sub-agent run shows a byte-identical frame.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 6 (Medium). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
