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

## Log

- 2026-11-07 Airic Lenz: Plan docs/plans/2026-11-07 - 00 - mcp-reconnect-off-loop-plan.md written (whole-set async reconnect, unexecuted). Owner then chose a per-server MCP connection redesign instead (needs an ADR superseding ADR 0037 D7 all-or-nothing); the plan is to be rewritten. Known: the live MCP holder is one mcp.Client per set with no per-server close.
- 2026-11-07 Airic Lenz: 2026-11-07: per-server design grilled and recorded as ADR 0109 (amends ADR 0037 via adr-0037-amend-1107); the whole-set plan is being rewritten for it. mcp-swap-midturn-race lands first. /mcp status view split out as mcp-live-status-view.
