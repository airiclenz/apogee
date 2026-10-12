---
title: ADR 0094 consequences
---

- `sub_agent` gains an optional `background` field on gated classes. Every other class, and every
  headless and daemon run, sees the tool unchanged.
- A Workflow now has three sources: `fan_out`, a Recipe, and a background `sub_agent`. Not every
  item's child calls `finish`: a `sub_agent` item's Receipt is the engine's.
- The TUI paints the call as a background-workflow call, and its finish note may span many lines.
- CONTEXT.md's **Workflow**, **Background workflow** and **Sub-agent** entries speak it.
