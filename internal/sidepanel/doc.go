// Package sidepanel is the Driver-neutral fold behind the side panel (ADR 0110): it folds engine
// Events into per-run panel state and answers, for a run scope, each section's rows, its summary
// line and its live predicate. It renders nothing — the TUI paints what it answers, and any other
// Driver can fold the same stream (decision 4: every section renders from engine Events, so watch
// mode folds the journal and a resumed session folds its stored record through the same entry
// points the Event fold uses).
//
// It imports only internal/domain, internal/sanitize and the standard library, so a Driver reaches
// it without pulling in the TUI or the engine.
//
// Files:
//   - sidepanel.go — the per-run panel State: the Event fold and each section's rows, summary and
//     live predicate (sub-agents, tasks, changed files, usage).
//   - mcp.go — MCPServers, the per-server MCP lifecycle fold (ADR 0111 decisions 9 and 10) a Driver
//     holds beside the State: one row per configured server, and whether any is not connected.
//   - doc.go — this map.
package sidepanel
