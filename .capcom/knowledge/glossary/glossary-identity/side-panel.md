---
title: "Side panel: the right-hand column that keeps live session state in view"
refs: [docs/layout/side-panel-layout.md]
---

**Side panel**:
The column on the right of the TUI's transcript slot that keeps live session state in view on
a wide terminal: sub-agents, the task list, files changed this session, usage and the MCP servers, each a
**section** that is `pinned`, `auto` (shown while live) or `hidden`. It is a glance, never a
workspace: a click on a section opens its detail view (**Run view**, the task-list card, `/usage`, `/mcp`).
Each section's header leads with its pin glyph: a click there toggles the section between `pinned`
and `auto` (`ui.side-panel.sections`), and a click anywhere else on the header folds the section to
that header or opens it again (`ui.side-panel.folded`). The keyboard reaches the same targets as the
second region of **Navigation mode** (ADR 0111), whose `⏎` on a stop is that click — a header being
two stops, its pin and its title (`adr-0111-amend-1109b`). The column itself appears or vanishes only when the window crosses
a width threshold or `ui.side-panel.show` changes. It is 44 columns wide by default, and its width
changes only when the user drags its separator (at least 24 columns, and never so wide that the
transcript drops under its floor), double-clicks the separator to reset it, or sets
`ui.side-panel.width` (`/panel width <n>|reset`, `/settings`), so live content never re-wraps the
transcript. Every
section is folded from **Events**, so **Watch mode** and a resumed session show the same panel; a
successful `delete_file` removes its changed-files row, and the TUI alone hides a listed file it
finds gone from disk until it exists again (`adr-0110-amend-1111`); the `mcp` section folds the `MCPServerEvent`s the composition root emits (ADR 0111 decision 8), which belong to
the process rather than to a conversation, so it survives `/clear` and a resume.
Ratified 2026-11-07 (ADR 0110); shipped 2026-11-08, set with `/panel` and the `ui.side-panel` keys;
the width made user-set 2026-11-08 (`adr-0110-amend-1108`); reached from the keyboard 2026-11-09
(ADR 0111, superseding ADR 0110 decisions 3 and 8); the `mcp` section added 2026-11-09 (ADR 0111
decision 10); deleted and missing files dropped from the changed-files rows 2026-11-11
(`adr-0110-amend-1111`, amending ADR 0110 decisions 4, 10 and 13).
_Avoid_: "sidebar" (the docs' word is side panel), "dashboard" (it is display state beside the
transcript, not a separate screen).
