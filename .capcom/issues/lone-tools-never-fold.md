---
title: Settled lone tool calls, Sub-agent and Skill groups never auto-collapse; tool stars uncoloured
status: open
type: bug
priority: 2
created: 2026-11-20T15:43:21Z
---

Under `ui.tools-auto-collapse` (on by default, v0.35.6) a settled, untouched `✦ Tools` umbrella folds to its one Group summary line, but three other tool surfaces stay open when settled: a lone tool call (`✦ Read` + `└ README.md … 249 lines`, `✦ Terminal` + its row), the `✦ Sub-agents (N)` group and the `✦ Skill (N)` group. A transcript of finished work therefore reads half folded (owner report, 2026-11-20, screenshot of a v0.35.6 session). Separately, a folded umbrella's `✦` is the plain text colour, so a folded line is hard to tell apart from narration.

Owner decisions (2026-11-20), settling `settled-group-collapse-scope`:
- A settled, untouched lone groupable tool call folds to the Group summary line an umbrella of one would paint (`✦ Read 1 file ⋯ ▶`, `✦ Ran 1 command ⋯ ▶`); opening it shows the block as it paints today.
- Settled, untouched Sub-agent and Skill groups fold too, to their header line.
- The leading `✦` of every tool block — umbrellas, lone calls, Sub-agent and Skill groups — paints in the tools header colour (`th.toolHeader`), the live blink unchanged.

## Why

Auto-collapse exists to declutter finished work; leaving lone calls and the delegation/skill groups open defeats it, and an uncoloured star hides that a folded line is a tool group.

## Acceptance

- With auto-collapse on, a settled untouched lone tool call, Sub-agent group and Skill group each paint one folded line; a live one or one the reader touched paints as before; with auto-collapse off nothing changes.
- A reader can open each folded surface by click and ⏎, and it stays open (touched).
- Every tool block's leading `✦` renders in the tools header style.
- Driven tests (`internal/tuitest` + `internal/stubllm`) cover the fold; docs (`docs/layout/tool-layout.md`, `docs/manual/configuration.md`) and ADR 0035's amendment trail state the widened rule; `make check` green.
