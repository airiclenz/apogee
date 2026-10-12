---
title: ADR 0074 context
---

ADR 0051 built `/undo` as a
per-Exchange stack of pre-images captured at `internal/tools`' write funnel, held in memory for
the life of the process. Two of its calls were deliberate cost decisions taken at that phase, and
both were written down as revisitable: decision 3 makes the funnel the coverage boundary, so a
file rewritten by `terminal`, `python_exec`, `run_tests`, a git-checkout form, an MCP server or an
embedder-registered tool is outside `/undo` by construction; decision 8 keeps the journal in
memory with no redo, so a relaunch — or a crash, or the end of a headless run — takes the way back
with it. Its rejected-alternatives list closed the git door on four specific objections.

The 2026-09-06 contender assessment (`docs/handoffs/2026-09-06 - 02 - contender-gap-assessment.md`,
gap 1) is the sighting ADR 0051 asked for. Rivals ship exactly the missing half and ship it the
way that answers the objections: opencode snapshots the work-tree into a **separate** git object
database — never the user's index, stash or branches — and offers `/undo` and `/redo` across
restarts. The same gap has a second face inside apogee: an Auto Firing
run by the daemon writes files with nobody watching and, until now, no revert surface at all (bead
`apogee-kk0.7`).

The owner resolved the design by question round on 2026-09-06; this record ratifies those calls and
the ones the plan's regression check settled on the same day. It supersedes part of ADR 0051.
