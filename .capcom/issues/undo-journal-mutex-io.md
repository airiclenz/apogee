---
title: undo journal holds its mutex across filesystem writes (audit 2026-09-20 Critical)
status: done
type: bug
priority: 1
aliases: [apogee-m6k]
created: 2026-09-21T20:19:15Z
closed: 2026-09-22T19:13:04Z
reason: Closed
---

Source: docs/reviews/code-audit-2026-09-20.md, Critical 'the undo journal holds its mutex across filesystem writes' (internal/undo/journal.go Revert/runStep/entry.apply, snapshot.go tracked.apply). The whole revert/redo step — SafeWriteFile/SafeRemove plus blocking git reads in snapshot-backed groups — runs under j.mu, the lock Record/MarkPre/Close also take; a slow restore stalls the engine, a revert racing a sub-agent write can wedge it.

Sequencing (owner, 2026-09-21): lands AFTER plan B item 4 (docs/plans/2026-09-20 - 02, which rewrites undo.Mutation / entry.apply onto security.Fence) — never inside it. Needs its own design; inputs: pop AND close the group under the lock (else a racing Record appends to the group being walked); bump generation at pop time so plan B item 6's previewed 'apogee undo confirm <generation>' misses a second confirm; walk lock-free; re-lock for the redo push and persist(); the conflict rule (pre-image compared before overwrite) becomes a statement about WHEN it is compared. The audit's one-liner ('snapshot the group pointer, walk lock-free') is thinner than this. Candidate for /improve-codebase-architecture or a plan E after plan B archives.
