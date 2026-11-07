---
title: ADR 0109 context
---

Issue `mcp-reconnect-freezes-tui`: an MCP reconnect after a settings or config change runs
synchronously on the Bubble Tea Update loop. While it dials, the TUI takes no keys and draws no
frames. On a slow box, or with a slow stdio server or a remote endpoint, that freeze lasts seconds,
which breaks the house rule that a slow machine makes apogee slower, never broken (AGENTS.md).

What the code had when this was decided (2026-11-07, base `4163c85d`):

- **One client covers the whole set.** `mcp.Client` (`internal/mcp/client.go`) holds every server's
  session in config order, plus one flat tool list. It has no per-server close, and it is not safe
  for concurrent use. `connectOne` already dials a single server. Each tool carries its own session
  and its server alias, so one server's tools can be separated from the rest by alias.
- **Live apply is all-or-nothing** (ADR 0037 decision 7). `liveMCP.reconnect`
  (`cmd/apogee/wire_mcp.go`) dials the whole new set. On success it rebuilds the registry, swaps it
  in with `SwapTools`, and then closes the old set. On any failure it keeps the old set. It has three
  callers, all on the Update loop: the `mcp-servers` settings row, a watched config save, and a
  Project config adoption. A fourth path, `readmitMCP`, handles `url-safety` edits.
- **`SwapTools` replaces the whole registry and only while idle** (`internal/agent/swaptools.go`).
  ADR 0037 decision 2 allows no second registry-mutation path. A watched save that lands mid-Turn
  calls it while the worker runs: that is a data race, filed as `mcp-swap-midturn-race`.
- **Startup is fatal on any MCP failure** (ADR 0037 decision 7, ADR 0095 decision 7).
- **The engine never sees a client**, only the tool registry built from the live set (ADR 0031).

A first plan made the whole-set reconnect asynchronous (draft plan
`2026-11-07 - 00 - mcp-reconnect-off-loop-plan`). Two rounds of review found holes, and each fix
added machinery: masking a removed server's tools while the whole set re-dials, generation counters
so a stale dial cannot reinstall a removed server, cancelling a dial when a newer one starts, and
stashing a finished set until idle. Every one of those holes exists only because each change
re-dials and swaps the whole set. The owner chose a per-server lifecycle instead on 2026-11-07, and
settled its open questions in a grill the same day.
