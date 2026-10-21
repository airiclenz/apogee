---
title: No Reaction can re-brief the model after a Compaction
status: done
type: feature
priority: 2
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-21T08:27:54Z
closed: 2026-10-21T12:34:38Z
reason: "compaction-finished notice, run:/advise: slot and stale-span strip shipped (items 8-14); resolved by plan 2026-10-21 - 00 - user-session-advice-plan (archived at 51b3a10d)"
---

After a Compaction the model loses whatever a session-start briefing told it, or keeps a stale copy in the protected first user message. Claude Code re-runs SessionStart hooks after compaction; apogee fires no Moment at a fold, so no Reaction can re-brief the model (e.g. `capcom prime`).

Wanted (ADR 0076 amendment `adr-0076-amend-1021b`, ADR 0018 amendment `adr-0018-amend-1021`): a `compaction-finished` notice fired once after every fold that ran (top-level agent only), payload `fold: auto | emergency | manual`; `run:` observes it; `advise:` on it lands fenced in the next user-role message after the fold (opening or overflow bridge), replacing the same reaction's stale span in the protected prefix.

## Acceptance
- `on: [compaction-finished]` is accepted under `run:` and `advise:`, refused under `gate:`.
- A fold that ran fires it once; a faulted or cancelled fold and a delegate fold fire none.
- An advise answer lands in the next user-role message after the fold, on both wires; a failure adds nothing; Bypass attaches nothing.
- The same reaction's earlier span in the first user message is stripped when the fresh one attaches.
- `docs/manual/reactions.md` documents the notice and its slot.
