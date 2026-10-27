---
title: The session-started window and both advise lanes are driven from ~9 Agent methods
status: done
type: task
priority: 2
deps: [driver-boot-sequence-copied]
plan: 2026-10-27 - 00 - session-briefing-and-construction-seed-plan
created: 2026-10-27T17:00:29Z
closed: 2026-10-27T18:58:18Z
reason: "Resolved by plan '2026-10-27 - 00 - session-briefing-and-construction-seed-plan' (de29e028): sessionBriefing owns the window, both advise lanes and held runs; shared held ledger; boundary lane-replace and window-open one critical section; only the module's own briefing_test.go reads lane.waiting()."
---

sessionWindow, the session-started advise lane and the compaction-finished advise lane form one lifecycle, but Submit, Wake, step, Close, announceSessionStart, fireNewlyArmed, foldFor, refold and exchangeClosed/Aborted each drive part of it; its lock order and ordering invariants live only in comments. Fixes 817dc5c0 and 125990ed were transitions a caller forgot; 839f4dd9 a reported run replayed. Tests reach into lane.waiting(). A latent race: announceSessionStart replaces the session lane before it reopens the window, so a Submit between the two sees the window reopen after it. Architecture review 2026-10-27 item 1.

## Why

A new shut site or lifecycle event can silently miss a transition.

## Acceptance

- One unexported module in internal/agent owns the window, both lanes and their held runs; the Agent passes it lifecycle events
- The advise lanes and the background manager share one held ledger
- No test reaches lane.waiting(); the boundary lane-replace and window-open are one critical section
