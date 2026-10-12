---
title: No test pins a background sub_agent's unreadable run_on refusal
status: done
type: task
priority: 3
plan: 2026-10-05 - 01 - subagent-wave-and-background-leftovers-plan
aliases: [apogee-background-run-on-refusal-untested]
created: 2026-10-05T15:33:26Z
closed: 2026-10-05T18:03:13Z
reason: resolved by plan 2026-10-05 - 01 - subagent-wave-and-background-leftovers (archived at 7af6fde3), item 2
---

Deferred from plan docs/plans/2026-10-05 - 00 - subagent-wave-and-background-plan.md (item 9). The CHANGELOG claims an unreadable run_on is refused before any workflow folder exists (backgroundSubAgentResult, internal/agent/workflowcall.go:337-342), but no test pins that path: TestWorkflowCall_ABackgroundSubAgentIsRefusedBeforeItStartsAWorkflow (internal/agent/workflowcall_test.go:1924) covers only continue, empty task and unknown roster.
