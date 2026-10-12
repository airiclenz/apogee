---
title: "ADR 0022 addendum of 2026-08-02: the single flight covers every record write, not only saves"
---

Decision 1's "a crash loses **at most one Turn**" was not true under the default config. The
single-flight latch covered `Save` alone, but a session record has three writers, not one: the
per-Turn `Save` replaces the record wholesale, `Store.Rename` (the only writer of a stored title —
the Addendum of 2026-07-31 put an automatic naming call on that path, on by default) is a
read-modify-write of the *whole* record, and the browser's `Delete` removes it. Nothing serialized
them against each other, and `saveComplete` in fact `tea.Batch`ed the title flush beside the
coalesced save, whose members run on separate goroutines. A rename that read the record before a
save replaced it wrote the pre-save version back — reverting engine state and scrollback together
by a whole Turn. A probe over `internal/session.Store` lost the newer payload in 25% of runs
(audit 2026-08-01). Nothing about the cadence, the record shape or the failure posture moves.

- **One queue for all three writes.** The TUI's latch is now "a record write is in flight", and
  `Save`/`Rename`/`Delete` all queue behind it. Saves still coalesce latest-wins, so Decision 1's
  "one in-flight write plus one pending" is unchanged for the save path; renames and deletes keep
  their order, being distinct instructions rather than restatements of one. A fold may never batch
  two record writes.
- **The store serializes the same three as a floor.** `internal/session.Store` holds a mutex across
  `Save`, `Delete` and — crucially — the whole of `Rename`'s read-modify-write, so any caller that
  does not come through the fold still cannot interleave. Readers stay unguarded: the atomic write
  means a reader sees a whole record either way. The split between the two layers is settled
  (roadmap C7, 2026-09-24): the fold owns ordering and the store owns atomicity.
- **A title that could not be written is retried, not dropped.** The apply path branches on
  `ActiveID()`, which the host mints at the *start* of the first `Save`, before the atomic write
  lands — so a title answering in that window renamed a record that did not exist yet and was
  discarded silently. It is now put back on the pending-title stash and applied at the next
  successful save, under the same never-clobber rule the stash always obeyed.
