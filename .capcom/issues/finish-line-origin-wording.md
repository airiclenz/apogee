---
title: Background finish line does not name a sub_agent origin
status: done
type: feature
priority: 3
plan: 2026-10-05 - 01 - subagent-wave-and-background-leftovers-plan
aliases: [apogee-finish-line-origin-wording]
created: 2026-10-05T16:15:28Z
closed: 2026-10-05T18:03:14Z
reason: resolved by plan 2026-10-05 - 01 - subagent-wave-and-background-leftovers (archived at 7af6fde3), item 7
---

When a background sub_agent finishes, the transcript line still uses the generic backgroundFinishFormat (internal/tui/workflow.go), naming the run only by its delegation name. Wording like 'background sub_agent <name> finished' needs an Origin field on domain.WorkflowPhaseEvent, set by the engine's workflowObserver (internal/agent). workflow.OriginOf already exposes the origin from the store. Deferred from item 11 (fcf93542) of the subagent-wave-and-background plan (ADR 0094); see docs/handoffs/2026-10-05 - 00 - subagent-wave-and-background-leftovers.md.
