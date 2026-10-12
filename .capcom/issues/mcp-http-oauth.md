---
title: MCP OAuth for HTTP MCP servers
status: done
type: task
priority: 3
plan: 2026-10-08 - 02 - mcp-oauth-plan
aliases: [apogee-rw6]
created: 2026-09-07T10:48:56Z
closed: 2026-10-08T18:49:37Z
reason: MCP OAuth shipped by plan 'docs/plans/archived/2026-10-08 - 02 - mcp-oauth-plan.md' (archived at 743f331a), all eight items; callback hardening follow-up tracked in apogee-mcp-callback-first-wins
---

Gap 10 of the 2026-09-06 contender gap assessment — the small table-stakes row, filed as one hygiene bead. DEFERRED by the owner 2026-09-07 — not selected for wave 2.

Five independent small items, each S:
- USD cost display, driven by a per-server 'price:' key.
- MCP custom headers / OAuth.
- ConPTY consoles on Windows.
- /help (there are 23 slash commands in internal/tui/command.go and no /help).
- pi-style structured compaction summary (goals / progress / decisions / next steps).

Each can be taken independently; split into children if more than one is picked up at once.

## Log

- 2026-09-19 beads: plan 'docs/plans/2026-09-19 - 01 - ten-urgent-beads-plan.md' item 14 owns the /help row only; the other four rows (USD cost, MCP headers/OAuth, ConPTY, structured compaction summary) stay open here.
- 2026-10-03 beads: plan 'docs/plans/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' shipped four rows — priced usage (root currency: + per-server price:, cost in /usage, the footer, /sessions and headless output), the structured compaction summary, MCP custom headers (headers: / headers-env: on sse and streamable-http), and ConPTY consoles on Windows. /help shipped earlier under plan 2026-09-19-01 item 14. Only MCP OAuth remains open here.
