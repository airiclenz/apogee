---
title: A watched config save mid-Turn calls SwapTools from Update while the worker runs
status: open
type: bug
priority: 2
created: 2026-11-07T09:09:06Z
---

A watched config save that changes `mcp-servers` folds mid-run by design (`foldConfigChanged`, `internal/tui/settingswatcher.go`, no idle gate), so the reconnect dials on the Update loop and then calls `engine.SwapTools` → `agent.SwapTools` (`internal/agent/swaptools.go`). That reads `a.turns.inExchange`, a plain bool the worker goroutine writes, with no lock — a probable data race (found by reading, not yet reproduced under `-race`). The swap is then refused as engine-busy, so the dial is also wasted.

Context: found while planning `mcp-reconnect-freezes-tui`; the per-server MCP redesign being designed for that issue may subsume it, but this race is the silent-damage half and can be fixed alone (defer the reconnect until the engine is idle).

## Why

A data race on the Agent breaks the ADR 0011 single-goroutine contract; it corrupts silently rather than failing loudly.

## Acceptance

- A driven test saves an `mcp-servers` change while a Turn runs, under `-race`, and reports no race
- No `SwapTools` call reaches the Agent while a worker holds it
