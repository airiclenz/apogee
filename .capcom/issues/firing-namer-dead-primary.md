---
title: A Firing's namer dials the dead primary for unrouted children after a fallover
status: done
type: bug
priority: 2
plan: 2026-11-02 - 04 - seat-and-resolved-entry-plan.md
created: 2026-11-02T20:02:19Z
closed: 2026-11-02T22:16:35Z
reason: "Resolved by 2026-11-02 - 04 - seat-and-resolved-entry-plan (archived at 66e4d894), item 8: the Firing namer reads the run's seat resolver's latest landing, so unrouted children are named on the fallback; TestFiringNamerFollowsTheSessionSeatFallover drives it."
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. 715c380e fixed only the routed half; for unrouted children the Firing's namer (cmd/apogee/naming.go) still dials the primary after a session-seat fallover.

## Acceptance

- After a session-seat fallover, a Firing names unrouted children through the fallback upstream; a test drives it.
