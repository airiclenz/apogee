---
title: "ADR 0022 addendum of 2026-08-02 (second): the closing flushes join the queue, and the exit waits for it"
---

The addendum above put every record write on one queue and left one caller outside it: the
synchronous closing flush a clean quit and `/clear`|`/new` shared (`saveSession`, now folded into
`saveAtIdle`). It wrote the record straight from the `Update` goroutine, so two windows survived the
single flight. First, the host reads its active session — id, `CreatedAt`, **title** — at the top of
`Save`, and `Rename` mirrors a new title onto that identity only *after* its store write returns; a
flush landing between the two wrote the pre-rename title back over the name the human had just
chosen. Second, `Rotate` ran straight through beside it, so a save still in flight (or waiting) when
`/clear` landed reached an already-inactive host and minted a **second id** for the outgoing
conversation — a duplicate record the fresh session then kept updating as its own. Decision 1's
cadence, Decision 4's "final Save → `Rotate` → reset" order, and the record shape are all unchanged;
only the mechanism by which that order is guaranteed moves.

- **The flush is a queued save like any other.** Both closing paths now schedule through
  `saveAtIdle` instead of calling the host, so a flush can never overlap an in-flight `Rename` or
  `Delete`, and a stale pending save is superseded by it under the same latest-wins coalescing.
- **`Rotate` is the queue's fourth write kind.** It writes no file, but it retires the id every
  save resolves against, so ordering against that one stream is the whole of what it needs: queued
  behind its own flush, it can no longer overtake it. Decision 4's "no rotate on a `ClearContext`
  error" is unchanged — nothing is queued on that path.
- **A clean quit defers its exit to the drain.** With the flush asynchronous, `tea.Quit` moves to
  the fold that finds the queue empty (the `quitting` latch ADR 0011's C4 deferral already used for
  a busy quit, now also armed at idle). Writes asked for on the way out therefore land before the
  program exits, where before they were abandoned mid-flight. A quit with nothing to flush and an
  idle queue still exits on the keypress.
