---
title: "undo: a concurrent Close or MarkPre can interleave with a lock-free Revert/Redo walk and act on the popped group"
status: done
type: bug
priority: 3
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-eykc]
created: 2026-09-22T20:56:37Z
closed: 2026-09-23T06:56:01Z
reason: "Item 8: snapshot calls wait for a running undo walk; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Item 1 of the 2026-09-22 audit-design-findings plan (commit 9f684b83) made Journal.Revert and Journal.Redo walk their step with the journal lock RELEASED: the group is popped, the boundary set and the generation bumped under one short hold, then the filesystem work runs lock-free (internal/undo/journal.go:465).

Before that, the whole step ran under one hold, so a concurrent snapshot call could not interleave with it. It now can: Journal.Close (internal/undo/snapshot.go:124) and Journal.MarkPre (internal/undo/snapshot.go:76) take the lock in the window where the reverted group is already popped, so a Close arriving mid-walk acts on the PREVIOUS group rather than the one being walked.

Harmless today because /undo and /redo are idle-only (ADR 0051 decision 7), so no snapshot call is in flight while a walk runs; taking the Close/MarkPre holds into the new lock discipline was declared out of scope by item 1. Worth closing before anything makes a revert concurrent with a live exchange.
