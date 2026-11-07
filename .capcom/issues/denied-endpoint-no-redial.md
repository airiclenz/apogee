---
title: url-safety deny of a server's old endpoint leaves it un-redialled
status: open
type: bug
priority: 2
created: 2026-11-07T13:23:47Z
---

When a `url-safety:` deny closes a live MCP session on a server's OLD endpoint (`liveMCP.dropDenied`, `cmd/apogee/wire_mcp.go:308`) while the layered config's new endpoint stays admitted, the admitted set under the old and new lists names the same servers (`sameServerNames`, `cmd/apogee/wire_settings.go:1691`), so `readmitMCP` returns at `if !plan.isMoved` (`cmd/apogee/wire_settings.go:1735`) and queues no request. The server is therefore not re-dialled at its admitted endpoint. Companion to the item 4 `requestServerLocked` finding. Deferred from plan item 9 (5ff32123); pre-run ceb331e4 had the same `sameServerNames` skip.

## Why

A server whose configured endpoint is still admitted should end up connected there, not silently dropped until some unrelated reconnect.

## Acceptance

- A url-safety deny that drops a session on a server's old endpoint, with the configured endpoint still admitted, queues a dial to the admitted endpoint
- A test drives that edit and sees the server connected at the admitted endpoint
