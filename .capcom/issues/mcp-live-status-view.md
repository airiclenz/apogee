---
title: No view of each MCP server's live connection state
status: open
type: task
priority: 3
created: 2026-11-07T09:22:21Z
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
