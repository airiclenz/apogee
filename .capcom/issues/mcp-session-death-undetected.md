---
title: An MCP server that dies mid-session is not noticed
status: open
type: task
priority: 4
created: 2026-11-08T21:33:39Z
---

`internal/mcp` has no session-death watcher and no `list_changed` handling; a crashed
server is noticed only when a call to one of its tools answers
`mcp server %q is not connected`. The MCP lifecycle Event of ADR 0111 decision 8 reports
only what the dial lifecycle knows, so `/mcp` and the `mcp` panel section keep showing such
a server as connected. Parked from the ADR 0111 grill, 2026-11-08.

## Acceptance

- A server process killed mid-session moves to `disconnected` in `/mcp` without waiting for a tool call.
