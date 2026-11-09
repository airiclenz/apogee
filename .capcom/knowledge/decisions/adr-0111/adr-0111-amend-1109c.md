---
title: "ADR 0111 amendment of 2026-11-09 (c): the mcp section's facts right-align, and the panel speaks the transcript's outcome tones"
refs: [docs/layout/side-panel-layout.md, internal/tui/sidepanel.go, internal/tui/mcppane.go]
---

**Why now.** Decision 10 gave the `mcp` section rows that read `name · state · N tools`, laid out
flush left behind the row's lead, while every other section right-aligns its facts at the column's
edge. And the panel painted its facts in one muted tone where the transcript colours the same facts
by outcome: a file's `+N −M` in `tool-marker`, a finished delegation's `done` in the `success` role,
a failure in the error tone. `/mcp` painted `failed` plainly, so one failure read three ways across
the panel, `/mcp` and the transcript. The side-panel visual alignment (plan
[`2026-11-09 - 04 - side-panel-visual-alignment-plan.md`](../../../../docs/plans/2026-11-09%20-%2004%20-%20side-panel-visual-alignment-plan.md),
issue `panel-fact-tones`) routes them through one vocabulary; the owner ratified it on 2026-11-09
("Failure: `errorText` everywhere" in the plan's ratified design calls).

**What this supersedes.** Decision 10's sentence "Rows read `name · state · N tools`". The rest of
Decision 10 — last in the default order, `auto` while any server is not connected, appended to a
list that leaves it out, a click or `⏎` opening `/mcp` — holds.

**(a) The mcp row.** A server's row is its name behind the blank body lead, then `state · N tools`
right-aligned at the content's edge, as every section's facts are; the name is clipped first so the
facts stay whole. The count is left off and kept exactly as before.

**(b) The tones.** A changed file's `+N −M` is in the `tool-marker` role; a finished sub-agent's
phase word `done` in the `success` role; every failure — a sub-agent's `✗` and `failed`, an MCP
server's `failed` — in the error tone the transcript's outcome slot paints a failure in. The
unfinished phase words and the facts around them stay faint.

**(c) `/mcp`.** A failed server's row in `/mcp` paints its `failed` in the same error tone, as a
run of its own beside the row's faint runs; the table's cells stay plain text, so the error run's
reset strips no faint from the cells after it.
