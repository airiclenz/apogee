---
title: url-safety deny keeps denied MCP sessions when the config fails to load
status: open
type: bug
priority: 2
plan: 2026-11-07 - 01 - url-safety-mcp-deny-gaps-plan
created: 2026-11-07T13:23:47Z
---

When the layered config fails to load, `denyMCP` returns `mcpReadmit{loadErr: err}` (`cmd/apogee/wire_settings.go:1686-1688`) before any drop, so a live MCP session on a newly denied endpoint keeps serving until the file parses again. `liveMCP.dropDenied` (`cmd/apogee/wire_mcp.go:308`) needs no config, so it could run on that path too. Pre-run ceb331e4's `readmitMCP` also returned on the load error before disconnecting anything.

## Why

A url-safety edit that closes a host should close the MCP connection on it on the keystroke (ADR 0109 decision 5), whether or not the config file currently parses.

## Acceptance

- With the layered config unreadable, a url-safety edit that denies a live session's endpoint closes that session
- A test covers the load-error path
