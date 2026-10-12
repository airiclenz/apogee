---
title: Workflow item falls back from sub-agents-server without a SeatFallbackNote
status: done
type: bug
priority: 2
plan: 2026-09-28 - 00 - enterable-workflow-stages-plan
aliases: [apogee-workflow-seat-fallback-silent]
created: 2026-09-28T11:31:57Z
closed: 2026-09-29T05:59:57Z
reason: "Fixed in 164b9f0a: a background workflow's finish note ends on SeatFallbackNote once when an item fell back"
---

A workflow item asking sub-agents-server with nothing latched falls back to the session silently: no SeatFallbackNote reaches its result line, unlike sub_agent (internal/agent/subagent.go:1503 appends it for sub_agent; the workflow spawner builds its child at internal/agent/workflowspawn.go:231 via newChildAgentOn(s.seat, ...) and never surfaces the fallback). Found at engine-run workflows plan item 40.

## Log

- 2026-09-28 Airic Lenz: Plan '2026-09-28 - 00 - enterable-workflow-stages-plan' item 2 (archived at dd782ee9) landed: a workflow item that falls back from sub-agents-server now carries SeatFallbackNote in its phase result and in the foreground call's answer. Still open: a background workflow's end message (background.go) does not go through workflowAnswer, so it carries no answer-level note.
