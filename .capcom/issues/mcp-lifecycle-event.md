---
title: MCP server lifecycle has no engine Event
status: open
type: feature
priority: 2
created: 2026-11-08T21:33:39Z
---

The engine emits no Event about MCP servers. Per-server state lives only in `cmd/apogee`'s
wiring (`wire_mcp.go`), outcomes reach the TUI as ephemeral notes at the idle-point commit,
and the last failure's cause is dropped. Headless, the daemon, watch mode and a resumed
session see nothing.

ADR 0111 decision 8: a journaled per-server MCP Event with phases `dialling`, `connected`
(tool count), `failed` (cause text) and `disconnected` (reason), emitted when a dial starts
and when it finishes, not at the idle commit; tools still join the menu at the next idle
point (ADR 0109). How the dial's owner reaches the Event stream is the plan's to settle.
A mid-session server death stays undetected (`mcp-session-death-undetected`).

Design: [ADR 0111](.capcom/knowledge/decisions/adr-0111.md).

## Acceptance

- A startup connect, a settings-driven reconnect and a failed dial each emit the Event with the right phase, tool count or cause.
- The Event is encoded in the journal (`internal/eventjson`) and survives a resume and a watch-mode replay.
- A dial that finishes mid-Turn emits its Event at finish, before the idle-point commit.
