---
title: "probe context --live: assert the 'as configured' Agent's armed sync lane, not only the request count"
status: done
type: task
priority: 3
aliases: [apogee-n5k]
created: 2026-09-17T07:46:40Z
closed: 2026-09-19T16:00:12Z
reason: "Implemented by docs/plans/archived/2026-09-19 - 00 - deferred-follow-ons-plan.md (items ✅ DONE 2026-09-19; commits 9f1b6a7f 0a01b4ce 104d0c93 4e2d1413 and the if9 item); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

The 'as configured' first Agent's armed sync lane (probe context --live) is asserted only by request count, not by reading a.Generation().Sync back — a later change that dropped the arming would still pass (the wire cannot show it); a facade-level assertion would need internal/agent access the item's Files list excludes.

- cmd/apogee/probecontext_test.go:305-307 — TestProbeContextLiveSendsTwiceWhenReactionsArmed asserts len(stub.Requests()) == 2 only.
- cmd/apogee/probecontext.go:289-297 — measureTurn1 arms the lane via a.Generation() / a.SetReactions(gen); nothing reads gen.Sync back.

Deferred out of the implement-plan run for docs/plans/2026-09-17 - 00 - turn1-context-cost-plan.md (item 6).

## Log

- 2026-09-17 beads: discovered while working on turn1-context-cost
