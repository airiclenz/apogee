---
title: "The panel's resume rebuild folds records, not painted cards (architecture review 2026-11-10 #5)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-10T19:37:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified PARTLY: `rebuildSidePanel` (internal/tui/sidepanel.go) keeps its own copy of the tools' write-target rule (`panelWriterTargetArgs`, untested against internal/tools' `pathArgWriteTarget`/`destinationArgWriteTarget`) and reads task lists back off painted ✓/○ markers (`panelTaskItems`). A new writer tool left out of the map silently disappears from Changed files on resume.

Correction to the review: child-run writes are not in `Record.Session` (only the main conversation's engine snapshot); they are in `Record.Transcript`, the Driver-neutral `session.Entry` wire form (internal/session/transcript.go) with Args, Depth, RunID and Regions. A sidepanel "record door" would fold those entries, not domain tool-call records. Task items are on that wire only as painted detail lines. "Any Driver can rebuild" is speculative: only the TUI paints a panel, and its resume rebuild is what ADR 0110 D14 decided.

Solution, if pursued: tools (or the registry) declare each writer's target argument once; sidepanel gains a `FoldRecord(session.Entry)` door beside its Event fold so the TUI only paints. Retiring the marker parsing needs an additive structured task-list field on the transcript wire (ADR 0083 part 1 forbids taking verdicts off the wire, not adding facts). Respect ADR 0110 D4 and D14.

Cheapest step on its own: a test pinning `panelWriterTargetArgs` to every tool that implements `workspaceWriteTarget`.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
