---
title: Per-child usage tap brackets delegations by call id
status: done
type: bug
priority: 3
aliases: [apogee-usage-tap-by-call-id]
created: 2026-09-24T16:22:36Z
closed: 2026-09-24T16:54:47Z
reason: eventTap brackets sub-agent usage by run id (SpawnRunID/RunID), legacy (depth, call id) fallback
---

internal/run/run.go eventTap (per-child SubAgentUsage, which feeds headless run_finished.sub_agents) still brackets by call id (t.open[ev.CallID], run.go:764, 816, 838, 852), so two fan-out siblings with the same call id mix their usage figures together. EventBase.RunID / SpawnRunID now identify a run exactly. Deferred from plan docs/plans/archived/2026-09-24 - 01 - subagent-premature-done-plan.md.
