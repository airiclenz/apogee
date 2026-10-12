---
title: lateEngine.Bind replays the delegation target before the seat
status: done
type: bug
priority: 2
plan: 2026-09-24 - 00 - review-findings-follow-through-plan
aliases: [apogee-bind-forgets-far-width]
created: 2026-09-24T09:16:53Z
closed: 2026-09-24T12:47:49Z
reason: "Resolved by review-findings-follow-through-plan (30ad3bbc) item 13: lateEngine.Bind replays the seat before the target; the sibling order defect in run.Once is a separate deferred finding"
---

cmd/apogee/wire_engine.go Bind replays pendingDelegation before pendingSeat; SetDelegationSeat calls forgetFarWidth, so a late-bound Agent loses the far width until the next heartbeat beat. Recorded in plan 2026-09-20-00 NOTES, never fixed.
