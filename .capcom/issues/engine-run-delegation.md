---
title: "Engine-run sub-agent orchestration: model sets parameters, engine runs fan-out/verify/merge"
status: done
type: feature
priority: 2
plan: 2026-09-27 - 00 - engine-run-workflows-plan
aliases: [apogee-engine-run-delegation]
created: 2026-09-26T18:54:16Z
closed: 2026-09-28T15:34:38Z
reason: "Delivered by plan 'engine-run-workflows-plan' (items 1-49, archived at 8d66b4f3): fan_out and recipe workflows run by the engine with one result line per item, background workflows with wake, /bg and /workflows, headless/daemon/facade surfaces."
---

The model configures a delegation with a few parameters (task template, list to fan out over, receipt fields, output paths, verify/reduce stages) and the engine runs the whole orchestration deterministically, returning one line per item. Goal: context management for local and frontier models. Brainstorm, not yet designed: see docs/handoffs/2026-09-26 - 00 - engine-run-sub-agent-orchestration-planning.md. Folds in the parent-side half of IDEAS.md 'Sub-Agents' and Stage B of apogee-2un.
