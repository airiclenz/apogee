---
title: ADR 0110 consequences
---

- **Engine additions come first.** A typed task-list Event, a context-cost Event at Turn end and a
  start time on the sub-agent phase Event are additive Events or fields (the Event sum type is
  additive, so no existing reader breaks). Each needs an eventjson mirror for watch mode and resume.
- **The transcript slot gets two widths.** Paint, mouse hit tests and the slot's panes read the slot
  width, while the prompt box, status line and footer keep `m.width`. The horizontal join needs a
  new join measured by the width authority (ADR 0030), not `JoinHorizontal`. The cached frame
  re-keys only on a gate crossing or a toggle.
- **Model size.** Panel state sits behind a pointer or stays in small plain values, so the Model
  stays under `TestModelFitsTheStack`'s 32 KiB (ADR 0011).
- **Layout spec.** `docs/layout/side-panel-layout.md` holds the sketches, the numbers (width, gate,
  floor, linger, usage threshold), the fold lines and the empty state, and `layout.md` gains a
  section that delegates to it.
- **Split diff and the start-up card** keep their wide layouts in `auto`, because the gate leaves
  the transcript 100 columns. In `on`, between 80 and 100 columns, they drop to their narrow forms.
- **`mcp-live-status-view`** can become a panel section once MCP state has an Event. The issue stays
  open until then.
- **Docs.** The manual documents `ui.side-panel.show`, the section list and `/panel`, including that
  terminal writes are not shown.
