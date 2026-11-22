---
title: A superseded first-contact refusal is re-placed after a resume or fork
status: open
type: bug
priority: 3
labels: [tui]
plan: 2026-11-22 - 00 - cold-start-notes-plan.md
created: 2026-11-22T05:43:45Z
---

Found by the startup-notes-followups run (plan 2026-11-21 - 03, deferred at closeout; code at faaeea47).

The binding's standing first-contact lines (`heartbeatState.firstContactLines`, internal/tui/heartbeat.go:94) are appended by `Model.noteBinding` (internal/tui/heartbeat.go:485-486) and re-placed after a `/sessions` resume or a `/fork` by `Model.replaceFirstContactLines` (internal/tui/model.go:1081-1084). The stash is cleared only by a later *successful* rebind for a non-first contact (internal/tui/heartbeat.go:434-437) or by a `/server` switch. A later contact-event rebind that is refused (internal/tui/heartbeat.go:425-430, which sets `lastRebindFailed` and returns before the clear) leaves the earlier first-contact refusal line in the stash, so a resume or fork re-places a refusal about the first contact's target even though the binding's most recent refusal named a different target.

Minor edge: it needs a first-contact rebind refusal followed by a mid-session refusal for another target, then a transcript swap.

## Why

`replaceFirstContactLines` promises to put back only warnings "that still hold for the session on screen"; a superseded refusal re-placed after a swap reads as the current state of the binding.

## Acceptance

- After a first-contact rebind refusal for target A and a later contact-event refusal for target B, a `/sessions` resume or `/fork` does not re-place the refusal naming A (either the stash drops it, or it is replaced by the standing refusal — decided at design time).
- The unknown-window and unapplied-roster lines that still hold are still re-placed (TestRestoreRePlacesFirstContactLines keeps passing).
- A test in internal/tui drives refusal A at first contact → refusal B at a contact event → resume, and counts the A note.
