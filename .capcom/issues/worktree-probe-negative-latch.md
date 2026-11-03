---
title: Git work-tree probe latches a negative answer for the Agent's life after one transient failure
status: open
type: bug
priority: 2
labels: [slow-box]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 10 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `active()` (internal/agent/treesnapshot.go:130-150, timeout :55) runs under `sync.Once` with the first call's context and a fixed 2 s timeout; any error leaves `isRepo` false for the life of the Agent.

## Why

On a throttled Pi with a cold cache, or an Esc during the first `run_command`, the always-on tracked-file mutation warning is silently off for the session. Breaks "slower, never broken".

## Acceptance

- Only a definitive answer is cached; a timeout, cancellation or transient error retries on the next call, latching "no" after a bounded number of transient failures.
- The probe has its own slow-box budget; ADR 0056 D4 is amended for it.
