---
title: "One MCP state fold; dials queued is a fact, not a note string (architecture review 2026-11-10 #9)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-10T19:37:32Z
---

Found by docs/reviews/architecture-review-2026-11-10.html, verified 2026-11-10 at a5c4f2db. Verified PARTLY: the same MCPServerEvents are folded twice, by `mcpServerStates.fold` (cmd/apogee/wire_mcp.go:393-448) and `sidepanel.MCPServers.Fold` (internal/sidepanel/mcp.go:50-100), kept in step by a comment and one test. The `mcp-servers` row is armed and cleared by comparing the host's returned note against `tui.MCPReconnectingNote` (internal/tui/settingsapply.go:370, settingswatcher.go:222, mcpreconnect.go:217/221); that constant is also the busy word. The review's claim that rewording the busy indicator breaks arming is overstated (all comparisons share the constant). The real coupling: a display string is the protocol across `SettingsHost.Apply`, so a host wording the note any other way silently stops the row arming.

Solution: one MCP state module (Fold, Events()) serves both the journal/TUI seed and the views; "dials queued" becomes a typed fact across tui.MCPHost (or the Apply result), and the TUI words it.

Caveat: the seed fold keeps raw Events to restate them (`events()`), while sidepanel rows are escape-stripped; the merged fold must keep the raw Event and strip only when building a row.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
