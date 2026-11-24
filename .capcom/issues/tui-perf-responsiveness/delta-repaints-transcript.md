---
title: Every streamed delta repaints the whole transcript
status: done
type: task
priority: 2
labels: [perf, tui, slow-box]
plan: 2026-11-23 - 08 - tui-perf-repaint-plan.md
created: 2026-11-23T19:29:39Z
closed: 2026-11-24T07:01:47Z
reason: "Resolved by tui-perf-repaint-plan (2026-11-23 - 08), archived at ff4144896, item 6: a streamed batch re-renders only the preview tail; session=300 5.16 -> 2.64 ms/delta (session=0 2.46)."
---

## Problem

Scenario: streaming a reply. Each coalesced token Msg (`teaSink`, at most one per 30 ms
window, `internal/tui/sink.go:74`) goes `foldEventAgainst` (`internal/tui/fold.go:51`) →
`transcript.appendToken` (`internal/tui/transcript.go:1703`), which bumps the transcript
generation; `Update`'s tail `settleFrame` → `settle` (`internal/tui/model.go:3135`) then sees a
`frameKey` mismatch and runs `layout` → `refreshViewport` (`model.go:3381`): `renderView` over
every block (all paint-cache hits, but O(blocks)), `reserveWidgetCells`, and the viewport's
re-measure of every line (`viewport-remeasures-lines`) — although only the streaming preview at
the tail changed.

Measured (Raspberry Pi 5, one 8-byte delta per Msg, i.e. no coalescing — the upper bound):
3.84 ms per delta with an empty session against 16.31 ms behind 300 tool blocks (about +42 µs
per block); 69% of stream CPU is under `refreshViewport`. A 32 KB reply streamed that way takes
66.8 s of TUI CPU at 300 blocks. One Pi 5 core saturates at about 60 uncoalesced deltas a
second. Within one reply the per-delta cost grows only 6–25% — the scrollback behind the stream
is the multiplier, not the stream.

## Why

While a local model streams on the same Pi 5–class box, the TUI's repaints compete with it for
CPU, and the cost grows with session length: long sessions stream slower and keys lag during a
reply.

## Suggested fix

Suggestion, to be designed: an incremental repaint — keep the settled prefix's lines (and their
known widths) from the previous paint and re-render and splice only the blocks whose key changed,
here the preview tail. That needs per-block line ranges from the previous paint and touches the
one-paint invariant of `refreshViewport`, so it is a candidate for an architecture pass before
implementation. Pairs with `viewport-remeasures-lines`, which removes the biggest share of each
repaint but not the whole-list walk. Output must stay byte-identical.

## Acceptance

- [ ] The per-delta cost at 300 tool blocks is close to the empty-session cost — a benchmark in
      `internal/tui` streams N deltas through `Update` + `View` over a resumed session;
      before/after recorded.
- [ ] A test driving the TUI through `internal/tuitest` + `internal/stubllm` (never an
      `httptest` upstream closure) streams a reply into a long session and its frames are
      byte-identical to a cold render.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("every streamed delta
repaints the whole transcript"). Report: `docs/reviews/tui-perf-audit-2026-11-23.md`.
