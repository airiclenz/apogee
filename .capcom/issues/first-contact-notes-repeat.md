---
title: First-contact beat notes pile up across resumes
status: done
type: bug
priority: 3
labels: [tui]
plan: 2026-11-21 - 03 - startup-notes-followups-plan.md
created: 2026-11-21T19:28:55Z
closed: 2026-11-22T05:58:50Z
reason: "Resolved by plan '2026-11-21 - 03 - startup-notes-followups-plan' item 2 (archived at 7b909423): first-contact beat notes are ephemeral, kept out of the record and forks; event beat notes still persist; TestFirstContactNotesStayOutOfAResumedRecord drives launch-resume-launch. Stale stash edge tracked in first-contact-lines-stale."
---

Found while planning startup-notice-class-scattered / startup-note-routing-scattered (2026-11-21, at ec353573).

A first-contact heartbeat note that lands on its own line is persisted to the session record: the unknown-window note (`unknownWindowNote`), a refused rebind (`rebindFailNote`), a profile roster not applied, an effort cleared (`effortClearedNote`), and a routine first-beat note once the session log has closed (`noteRebindRoutine`, internal/tui/heartbeat.go). Each is re-derived on every launch, so a session resumed N times carries N copies of the same warning in its transcript.

## Why

The repeats are noise in a resumed transcript and read as N separate events. The owner chose (2026-11-21) to keep the startup-notes refactor behaviour-neutral and fix this on its own.

## Acceptance

- A first-contact beat note re-derived at launch is not duplicated in a resumed session's transcript (either made ephemeral, or deduplicated against the record — decided at design time).
- A beat note produced by a mid-session event (not first contact) is still persisted.
- A test in internal/tui drives launch → resume → launch and counts the note.
