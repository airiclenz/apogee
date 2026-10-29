---
title: A repo MCP pin mismatch at connect aborts every server
status: done
type: bug
priority: 2
created: 2026-10-18T18:44:11Z
closed: 2026-10-18T20:56:55Z
reason: resolved by plan 2026-10-18 - 02 - exec-pin-followups-plan (archived at 86afb7c1), item 7
---

A repo MCP server's pinned files are checked again before launch (`internal/mcp/transport.go:383` `resolveStdioProgram`, `changedPinRefusal`). If a pinned file changes in the window between the startup config read and `Connect`, that refusal fails the whole all-or-nothing `ConnectWith` (`internal/mcp/client.go:146`), which kills startup and takes every global server down with it. ADR 0104 decision 4 wants a mismatch to refuse that one server's run with a notice and re-propose it.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- A repo server whose pin no longer matches at connect is skipped with a notice naming the changed file, and is proposed again; the global servers and the other adopted repo servers still connect and startup proceeds.
- The same holds on a live-reload reconnect.
- A test in `internal/mcp/` or `cmd/apogee/` pins it.

## Log

- 2026-10-29 Airic Lenz: 2026-10-18: rootWiring.announce (cmd/apogee/wire_boot.go) is startup-only — stderr plus startupNotices, handed to the TUI once at boot. A mid-session call would write over the alternate screen and never reach the transcript, so a mid-session notice (a reconnect's MCP skip, an adoption) is relayed on the surface showing then: the returned error or row note (applyMovedKinds in projectrules.go, readmitMCP in wire_settings.go). Source: docs/plans/archived/2026-10-18 - 02 - exec-pin-followups-plan.md item 7.
