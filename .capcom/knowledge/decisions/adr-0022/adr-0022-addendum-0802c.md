---
title: "ADR 0022 addendum of 2026-08-02 (third): the queue orders WHICH record, not only what is written to it"
---

Two more `SessionHost` calls were still made straight from the `Update` goroutine, both from the
`/sessions` overlay: the `Rotate` that retires the live conversation's id when the human deletes its
file, and the `Activate` a resume uses to adopt the loaded record. Neither writes a file, which is
why both were read as "not a record write" — but both move **which record every later `Save`
resolves against**, so running them outside the queue reordered the stream exactly as the closing
`Rotate` did before the addendum above. Deleting the active session while a save was in flight
retired the id under that save, so it landed as a **second record** for the live conversation and the
delete then removed the wrong one of the two. A resume applied its `Activate` ahead of everything
queued, so the outgoing conversation's coalesced save was written into the **record just resumed** —
the loaded session's transcript and engine state replaced by the conversation the human was leaving
(audit 2026-08-01, action item 1). The seam is unchanged: same calls, same preconditions, same
"activate only after a confirmed restore" rule; only where they are issued from moves.

- **Retargets are queued writes.** `Rotate` and `Activate` are the queue's two *retargeting* kinds.
  The browser's active-record delete queues its rotate ahead of its own delete, and `resumeLoaded`
  queues its activate, so everything already pending against the outgoing record lands first. The
  view does not wait on either: a resume repaints from the record already in hand, so the human sees
  the resumed session immediately while the redirect settles behind them.
- **Saves do not coalesce across a retarget.** Latest-wins was previously "supersede the one save
  in the queue", which is right only while every queued save describes the same record. A save
  queued before a retarget and one queued after it describe different records, so coalescing across
  the line would write the incoming conversation into the outgoing record *and* lose the outgoing
  one's final state. Coalescing is now confined to the queue's last segment — the whole queue
  whenever no retarget is waiting, which is the ordinary case and is unchanged.
