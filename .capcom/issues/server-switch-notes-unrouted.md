---
title: Two server-switch notes bypass the start-up chrome
status: open
type: task
priority: 3
labels: [tui]
created: 2026-11-21T20:27:34Z
---

Deferred out of plan `2026-11-21 - 02 - startup-notes-plan.md` (item 5): `serverSwitchNote` and the "server fallback: the display could not follow" note in `internal/tui/heartbeat.go` still call `transcript.addNote` directly (internal/tui/heartbeat.go:649, internal/tui/heartbeat.go:690), because the item's list did not name them; every other heartbeat note now routes through the start-up chrome.

## Acceptance

- Both notes route through the start-up chrome (a noticeKinds row each), or the chrome's doc records why they stay direct.
