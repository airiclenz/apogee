---
title: "ADR 0105 amendment of 2026-11-02 (b): background workflows fall over with the session seat"
---

Decided with the Server fallback follow-ups (docs/plans/2026-11-02 - 01 - fallback-followups-plan.md, item 2). It records how a background workflow (ADR 0089) takes part in the per-seat move decision 6 describes, amending it.

Decision 6 moves the whole seat, but a background workflow's host — the launch-time snapshot its items run off — held no fallback resolver, so a session-seat fault inside a background workflow child was abandoned even when the entry names a fallback. **A background workflow child on the session's live holder falls over with the seat.** The host copies the session seat's resolver beside the holder it copies, so an unrouted child on that holder consults the resolver and moves the session's seat as a foreground child would: one `upstream_switched` Event on the session seat, the session following at its next Step, and the TUI noting and following that switch although the workflow owns the Event.

**A background workflow left behind by `/server` never falls over.** A server switch installs a fresh holder for the session and leaves the workflow on the client it started with (ADR 0089); the holder it leaves is now marked retired. A fault on a retired holder consults no resolver and abandons the item as it did before Server fallback, and a rehome whose resolver was still waiting when the holder was retired is refused, so the seat never moves after the human moved it.
