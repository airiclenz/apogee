---
title: A repo MCP pin mismatch at connect aborts every server
status: open
type: bug
priority: 2
created: 2026-10-18T18:44:11Z
---

A repo MCP server's pinned files are checked again before launch (`internal/mcp/transport.go:383` `resolveStdioProgram`, `changedPinRefusal`). If a pinned file changes in the window between the startup config read and `Connect`, that refusal fails the whole all-or-nothing `ConnectWith` (`internal/mcp/client.go:146`), which kills startup and takes every global server down with it. ADR 0104 decision 4 wants a mismatch to refuse that one server's run with a notice and re-propose it.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- A repo server whose pin no longer matches at connect is skipped with a notice naming the changed file, and is proposed again; the global servers and the other adopted repo servers still connect and startup proceeds.
- The same holds on a live-reload reconnect.
- A test in `internal/mcp/` or `cmd/apogee/` pins it.
