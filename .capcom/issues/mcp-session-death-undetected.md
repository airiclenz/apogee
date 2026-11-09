---
title: An MCP server that dies mid-session is not noticed
status: done
type: task
priority: 4
plan: 2026-11-09 - 00 - mcp-test-gaps-and-session-death-plan
created: 2026-11-08T21:33:39Z
closed: 2026-11-09T12:25:04Z
reason: "Resolved by plan '2026-11-09 - 00 - mcp-test-gaps-and-session-death-plan' items 3-6 (archived at 556778e7): mcp.Client reports a session that ends on its own, liveMCP marks it disconnected (lost), and TestE2EMCPServerDeathReachesMCPPane shows a killed server reaching /mcp without a tool call."
---

`internal/mcp` has no session-death watcher and no `list_changed` handling; a crashed
server is noticed only when a call to one of its tools answers
`mcp server %q is not connected`. The MCP lifecycle Event of ADR 0111 decision 8 reports
only what the dial lifecycle knows, so `/mcp` and the `mcp` panel section keep showing such
a server as connected. Parked from the ADR 0111 grill, 2026-11-08.

## Acceptance

- A server process killed mid-session moves to `disconnected` in `/mcp` without waiting for a tool call.
