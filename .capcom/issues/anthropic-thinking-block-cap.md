---
title: Anthropic stream accepts unlimited `thinking` blocks, allowing memory exhaustion
status: done
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 8 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `openReasoning` (internal/provider/wire_anthropic_stream.go:207, layout append :212) stores each non-redacted `thinking` block without charging `maxReplyTextBytes` or counting it against a cap; only `redacted_thinking` is charged. The idle timer resets on any byte, so the stream has no total bound.

## Why

A hostile `wire: anthropic` endpoint or a middlebox on a cleartext link can stream endless unclosed `thinking` starts and OOM a Pi-class box in under a minute.

## Acceptance

- Open reasoning blocks per reply are capped and each block charges a fixed cost; exceeding it ends the stream with the existing FaultLocal error.
- A replay test of many `thinking` starts ends the stream.
