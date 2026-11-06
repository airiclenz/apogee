---
title: MCP reconnect freezes the TUI on the Update loop
status: open
type: bug
priority: 2
deps: [idle-wait-no-spinner]
created: 2026-11-06T20:34:31Z
---

An MCP reconnect runs synchronously on the Bubble Tea Update loop, so the whole TUI freezes while the servers dial (seconds; a stdio spawn or a dead remote can be longer), and no busy indicator can tick:

- a settings-pane `mcp-servers` edit — `Settings.Apply` (`internal/tui/settingsapply.go`) → `reconnectMCP` (`cmd/apogee/wire_settings.go`)
- a config file save — `ReloadConfig` (`internal/tui/settingswatcher.go`)
- a project-rule adoption — `applyMovedKinds` (`cmd/apogee/projectrules.go`)

Wanted: the reconnect runs off the loop as a Cmd, its reply folded as a Msg, and the wait shown through the busy indicator `idle-wait-no-spinner` adds.

## Why

A frozen frame on a slow box is indistinguishable from a hang (AGENTS.md: a slow machine makes apogee slower, never broken).

## Acceptance

- None of the three paths calls the MCP reconnect on the Update loop
- The status line shows the busy indicator while a reconnect is in flight
- A test drives a slow reconnect and sees keys still handled meanwhile
