---
title: An aborted Exchange drops a retained background sub_agent child
status: done
type: bug
priority: 2
plan: 2026-10-05 - 01 - subagent-wave-and-background-leftovers-plan
aliases: [apogee-background-child-exchange-rollback]
created: 2026-10-05T15:33:24Z
closed: 2026-10-05T18:03:12Z
reason: resolved by plan 2026-10-05 - 01 - subagent-wave-and-background-leftovers (archived at 7af6fde3), item 1
---

Deferred from plan docs/plans/2026-10-05 - 00 - subagent-wave-and-background-plan.md (ADR 0094). When a background child is retained into the root's retainedDelegates (internal/agent/subagent.go:1270) while the root has its own Exchange open, an abort of that Exchange (rollBackExchange, internal/agent/children.go:363, restores the atExchange snapshot taken at internal/agent/children.go:350) drops the background child's entry, so a later continue cannot find it. Worked out from the code; no test has shown it failing.
