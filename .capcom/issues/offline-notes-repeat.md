---
title: Cold-start offline notes pile up across resumes
status: open
type: bug
priority: 3
labels: [tui]
created: 2026-11-21T21:04:48Z
---

Found while planning first-contact-notes-repeat (2026-11-21, at 38fb10b7).

A cold-start `offline` heartbeat note (`kindOffline`, the first failure believed at once) and the later `connected: …` note are raised with `contactEvent`, not `contactFirst`, so the start-up chrome persists them (internal/tui/startupchrome.go `destination`). A session resumed while its server is down re-derives the offline note on every launch, so a session resumed N times carries N copies. Plan `2026-11-21 - 03 - startup-notes-followups-plan.md` makes first-contact beat notes ephemeral but leaves these, by owner decision (2026-11-21).

## Why

Same noise as first-contact-notes-repeat: the repeats read as N separate outages in a resumed transcript.

## Acceptance

- A cold-start offline note re-derived at launch is not duplicated in a resumed session's transcript.
- An offline or connected note from a mid-session outage is still persisted (design decides when a later `connected: …` is news).
- A test in internal/tui drives launch → resume → launch with the server down and counts the note.
