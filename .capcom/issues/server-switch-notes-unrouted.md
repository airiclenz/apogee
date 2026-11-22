---
title: Two server-switch notes bypass the start-up chrome
status: done
type: task
priority: 3
labels: [tui]
plan: 2026-11-21 - 03 - startup-notes-followups-plan.md
created: 2026-11-21T20:27:34Z
closed: 2026-11-22T05:58:50Z
reason: "Resolved by plan '2026-11-21 - 03 - startup-notes-followups-plan' item 1 (archived at 7b909423): both server-switch notes and the follow-refusal/fallback/unrecorded-choice notes route through the start-up chrome with noticeKinds rows."
---

Deferred out of plan `2026-11-21 - 02 - startup-notes-plan.md` (item 5): `serverSwitchNote` and the "server fallback: the display could not follow" note in `internal/tui/heartbeat.go` still call `transcript.addNote` directly (internal/tui/heartbeat.go:649, internal/tui/heartbeat.go:690), because the item's list did not name them; every other heartbeat note now routes through the start-up chrome.

## Acceptance

- Both notes route through the start-up chrome (a noticeKinds row each), or the chrome's doc records why they stay direct.
