---
title: TestLateEngineReplaysTheFloorGatesAtTheBind asserts on the unexported pendingGeneration
status: done
type: task
priority: 3
aliases: [apogee-370]
created: 2026-09-09T03:23:31Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): `cmd/apogee/wire_engine_test.go:118` asserts on the unexported `pendingGeneration` field rather than an observable outcome, and its rewritten second swap no longer pins 'a later edit keeps the earlier one's bits' — nothing replaced its coverage of a Floor-only swap leaving Bypass and Observe alone at the holder.
