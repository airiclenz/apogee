---
title: Side panel facts are all muted while the transcript colours the same facts
status: open
type: task
priority: 3
labels: [tui]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

**Today** (frame `01-wide`; style dump)
- Every right-hand fact is the muted grey `statusFaint`: `+N −M` (`internal/tui/sidepanel.go:565`),
  phase and elapsed (`:464`), and the usage figures (`:606`).
- Failure is painted three different ways:
  - the panel's `✗` and the MCP `failed` use `errorText`, which is bold red (`sidepanel.go:454,479`);
  - the `/mcp` pane paints `failed` plainly (`mcpStateWord`, `internal/tui/mcppane.go`);
  - the transcript's failure is its red outcome slot, and its done `✓` is deliberately not bold
    (`internal/tui/theme.go:359-366`).
- MCP rows are laid out flush left as `name · state · N tools`, outside `panelRow`
  (`sidepanel.go:476-489`). Every other section right-aligns its facts.
- The done mark sits before the name in the panel (`✓ planner`) and after it in the transcript
  (`planner ✓`).

**The rest of the UI**
- The transcript paints `+N −M` in `toolMarker` and a finished sub-agent's `done` in the success
  tone (`summaryStyle`, `internal/tui/toolleader.go:316-328`).

**Wanted**
- Panel facts routed through the same roles as the transcript's outcome slot: `+N −M` in
  `toolMarker`, `done` in the success tone, and failure in one treatment across the panel, `/mcp`
  and the transcript.
- MCP rows right-align state and tool count like the other sections.
- The mark position is decided once for both surfaces.

## Acceptance

- A style dump of a populated panel shows `+N −M` in `toolMarker`, `done` in the success tone, and failure in the same style as the transcript's failure.
- MCP rows right-align `state` and `N tools`; `/mcp` and the panel paint `failed` the same way.
