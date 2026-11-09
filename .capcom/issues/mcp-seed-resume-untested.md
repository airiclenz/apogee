---
title: No e2e test seeds MCP state into a resumed session's journal
status: open
type: task
priority: 3
labels: [test-gap]
created: 2026-11-09T09:10:39Z
---

Item 17 of `2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan` (commit `f5a2b50a`) seeds each live Event journal with one `mcp_server` line per configured server right after `run_started`. The seed runs at every journal birth through `sessionHost.mcpSeed` (`cmd/apogee/wire_session.go:129`, called at `cmd/apogee/wire_session.go:403` from `openEventJournalLocked`). A resumed record's journal is born in `attachEventJournal` (`cmd/apogee/wire_session.go:325`). The e2e tests cover only a fresh start and the journal born again after `/clear`; no test drives a resumed session and checks that its journal opens on the seed.

## Why

A resumed session is the third birth path the item names. Without a test, a regression there would go unseen: a watcher of a resumed session would see no MCP state until a server next changes.

## Acceptance

- An e2e test in `cmd/apogee` resumes a saved session with at least one configured MCP server and asserts its Event journal carries one `mcp_server` line per server right after `run_started` and ahead of any live `mcp_server` line.
