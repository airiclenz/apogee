---
title: "Persistent undo: per-Exchange snapshot pair in a session-owned git object DB, /redo, apogee undo verb"
status: done
type: feature
priority: 2
plan: 2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan
aliases: [apogee-6gf]
created: 2026-09-06T16:12:32Z
closed: 2026-09-07T10:38:59Z
reason: "Delivered by plan 'docs/plans/archived/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md' items 6-16 (all 16 marked DONE 2026-09-07, plan archived a19f11e1): ADR 0074, internal/subprocess + internal/gitexec extraction, internal/snapshot session-owned git object store, snapshot-backed undo groups + redo stack, journal.json persistence, engine/Driver wiring, /undo + /redo, 'apogee undo <session-id> [confirm]', docs. Shipped in v0.20.8 (commits 04cf5d8b..4b3229af)."
---

Gap 1 of docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md. /undo is an in-memory one-process journal blind to terminal/python_exec/MCP writes (internal/undo/journal.go, ADR 0051 D3/D8). Ratified 2026-09-06: session-owned git object DB under ~/.apogee/snapshots/<session-id>/ (never touches the user's .git/index), diff-scoped skip-and-report revert (D5 kept), /redo, apogee undo <session-id> [confirm] for headless/daemon Firings (closes apogee-kk0.7), git absent -> in-memory fallback. ADR 0074 supersedes parts of ADR 0051.
