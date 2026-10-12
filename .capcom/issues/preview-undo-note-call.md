---
title: "tui: previewUndo calls eng.UndoNote() unconditionally"
status: done
type: task
priority: 3
aliases: [apogee-tzk]
created: 2026-09-07T09:01:01Z
closed: 2026-09-07T09:36:36Z
reason: Closed
---

internal/tui/undo.go:86 — previewUndo reads m.eng.UndoNote() on every preview, including the path where a step exists and the nothing-note is discarded. A wasted engine read; move it under the no-step branch. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
