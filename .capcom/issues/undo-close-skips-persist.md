---
title: "undo: Journal.Close early returns bypass persist(), leaving a stale redo stack on disk"
status: done
type: bug
priority: 1
aliases: [apogee-vhn]
created: 2026-09-07T09:00:46Z
closed: 2026-09-07T09:36:36Z
reason: Closed
---

internal/undo/snapshot.go:116 — Journal.Close has four early returns (funnel-only group, dropped group, failed Capture, failed Diff) that skip persist(). After a Record that cleared the redo stack, a journal.json written by an earlier Revert keeps a redo stack memory has already discarded, until the next successful save overwrites it. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
