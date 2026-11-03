---
title: Streamed parallel tool calls sharing `index 0` with distinct ids fold into one invalid call
status: open
type: bug
priority: 2
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 13 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `address` (internal/provider/stream.go:560, `atIndex` :580, `fold` :528) merges a fragment into the call at its wire index even when it carries a different non-empty id. A server giving every parallel call `index: 0` with its own id (some OpenAI-compatible shims) yields one call with `{"a":1}{"b":2}`.

## Why

The loop sees invalid JSON and the second call id never gets a tool result.

## Acceptance

- An indexed fragment with a different non-empty id opens a new call; empty-id continuation is unchanged.
- A replay test with two `index:0` chunks and different ids yields two calls.
