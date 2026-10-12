---
title: "Turn-1 context cost: ContextCost report, probe context, headless run_finished keys, bytes golden"
status: done
type: feature
priority: 2
plan: 2026-09-17 - 00 - turn1-context-cost-plan
aliases: [apogee-agm]
created: 2026-09-17T06:43:52Z
closed: 2026-09-17T08:03:14Z
reason: Closed
---

Measure and surface the tokens apogee itself puts in front of the model at Turn 1 (standing system content + tool menu) as a per-piece estimate and, once measured, the Turn-1 prompt_tokens. Designed by plan docs/plans/2026-09-17 - 00 - turn1-context-cost-plan.md (ADR 0079 in item 1). Origin: external feedback 2026-09-17 flagged self-imposed context cost as the one blind spot the bench does not cover.
