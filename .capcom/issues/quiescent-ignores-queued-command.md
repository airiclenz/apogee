---
title: Model.quiescent gates a Firing on staged messages only — a queued command does not hold a Schedule
status: done
type: bug
priority: 4
aliases: [apogee-289]
created: 2026-09-15T16:25:39Z
closed: 2026-09-16T21:00:38Z
reason: "fix(tui): a queued command holds a Schedule's Firing like a staged interjection"
---

Deferred from plan 2026-09-14 - 04 (item 2, queued commands). internal/tui/schedule.go:287 — quiescent() counts m.pendingInterjections but not m.deferredCommands, so a queued command waiting in stateErrored does not hold a Schedule's Firing. Harmless today: the queue is empty at every true idle (the drain runs before the state settles), but the scheduler was not in item 2's Files and the third term of quiescent should cover both rows. Remedy: add len(m.deferredCommands) == 0 to the gate and a test.
