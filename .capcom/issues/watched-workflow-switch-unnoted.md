---
title: A watcher writes no fallback note for a workflow-moved session seat
status: open
type: bug
priority: 3
labels: [fallback]
plan: 2026-11-02 - 02 - turn-total-and-open-bugs-plan
created: 2026-11-02T16:08:52Z
---

Since a background workflow child can move the session's own seat onto its `fallback:` (plan 2026-11-02 - 01 - fallback-followups-plan, item 2, 37ca5e97), the live eventMsg case notes that switch: internal/tui/model.go:1345 adds fallbackNote for a workflow-owned UpstreamSwitchedEvent on UpstreamSeatSession. A watcher of the same journal does not: internal/tui/watch.go:845 (foldWatchedEvent) routes every workflow-owned event to foldBackgroundEvent and returns, so the session-seat switch reaches the watcher's Inspector rings but writes no "server fallback:" note in its transcript. The conversation's own session-seat switch is noted for a watcher (TestFallbackWatchedSwitchIsNotedAndNotFollowed, internal/tui/fallback_test.go:258).

## Why

A watcher of a session whose background workflow moved the session's server sees no sign of the move, while the owner sees the note.

## Acceptance

- foldWatchedEvent notes a workflow-owned session-seat UpstreamSwitchedEvent once and follows nothing, pinned by a test beside TestFallbackWatchedSwitchIsNotedAndNotFollowed
