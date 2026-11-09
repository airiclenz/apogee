---
title: No view of each MCP server's live connection state
status: done
type: task
priority: 3
deps: [mcp-lifecycle-event]
created: 2026-11-07T09:22:21Z
closed: 2026-11-09T09:25:18Z
reason: "Resolved by plan '2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan' items 18-21 (archived at 82491d30): /mcp report pane and mcp side-panel section show live per-server state; report-order test gap tracked in mcp-report-order-untested."
---

Nothing in the TUI shows each MCP server's live connection state: connecting, connected, failed (and
why), or closed. Under ADR 0109 each server has its own lifecycle, and its outcome reaches the user
only as an ephemeral transcript note that scrolls away. `apogee mcp list` shows config and adoption
state only, and connects to nothing.

Wanted: a view, such as a `/mcp` command, that lists every configured server with its live state,
its tool count and the last failure's cause.

Deferred from the ADR 0109 grill (2026-11-07) as out of scope for `mcp-reconnect-freezes-tui`.

## Acceptance

- One TUI surface lists every configured MCP server with its live state and, for a failed one, the
  cause text.
- The view reflects a dial that finishes while it is open.


## Design (2026-11-08)

Designed by [ADR 0111](.capcom/knowledge/decisions/adr-0111.md) decisions 9–10, on the
Event of `mcp-lifecycle-event`:

- `/mcp` is a read-only report pane (like `/usage`): every configured server with
  transport, source, live state, tool count and last failure cause; it updates while open.
  Reconnect stays on the settings `mcp-servers` row.
- A fifth side-panel section, `mcp`: rows `name · state · N tools`, last in the default
  order, `auto` (shown while any server is dialling, failed or disconnected), appended in
  `auto` to a `ui.side-panel.sections` list that does not name it; a click or `⏎` opens
  `/mcp`.
