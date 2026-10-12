---
title: "floor: no end-to-end coverage of the tool_call-tag and whole-text salvage shapes"
status: done
type: task
priority: 2
aliases: [apogee-u28]
created: 2026-09-07T09:01:02Z
closed: 2026-09-07T09:36:37Z
reason: Closed
---

The stubllm e2e proves a salvaged call reaches the wire for the fenced-block shape only; the '<tool_call>…</tool_call>' tag shape and the whole-trimmed-text container shape are covered by internal/floor unit tests alone. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
