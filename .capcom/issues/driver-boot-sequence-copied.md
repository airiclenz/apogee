---
title: Every Driver hand-runs the Agent's order-sensitive boot sequence
status: done
type: task
priority: 2
plan: 2026-10-27 - 00 - session-briefing-and-construction-seed-plan
created: 2026-10-27T17:00:29Z
closed: 2026-10-27T18:58:18Z
reason: "Resolved by plan '2026-10-27 - 00 - session-briefing-and-construction-seed-plan' (de29e028): New/Resume apply the whole construction seed and announce session-started; DeferSessionStart/AnnounceSessionStart removed; TestConstructionSeed pins the order."
---

run.Once, lateEngine.Bind and measureTurn1 each set DeferSessionStart, construct, replay SetReactions / journal / seat-before-target, then call AnnounceSessionStart. The seat-before-target bug was fixed twice (b0bbeac1, bb00dfa1) and measureTurn1 was patched separately (5ee60135). Architecture review 2026-10-27 item 2.

## Why

Each new Driver re-learns the order; a forgotten step ships a bug.

## Acceptance

- agent.New / agent.Resume take the whole construction seed (Sync lane, delegation seat and target, undo journal) and announce session-started themselves
- DeferSessionStart and AnnounceSessionStart no longer exist
- One construction-order test in internal/agent; the per-Driver seat-before-target tests are gone
