---
title: A reused upstream call id resumes a finished background sub_agent
status: done
type: bug
priority: 3
plan: 2026-10-05 - 01 - subagent-wave-and-background-leftovers-plan
aliases: [apogee-reused-call-id-resumes-run]
created: 2026-10-05T15:33:25Z
closed: 2026-10-05T18:03:13Z
reason: resolved by plan 2026-10-05 - 01 - subagent-wave-and-background-leftovers (archived at 7af6fde3), item 3
---

Deferred from plan docs/plans/2026-10-05 - 00 - subagent-wave-and-background-plan.md. The call-id salt (ADR 0094 D8; backgroundSubAgentPlan, internal/agent/workflowcall.go:359) assumes upstream call ids are unique. A server that reuses an id for an identical task in a later Turn would resume the finished workflow folder instead of starting a new run. Seen in code, not tested.
