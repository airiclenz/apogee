---
title: "Side panel: the fixed right-hand column that keeps live session state in view"
refs: [docs/layout/side-panel-layout.md]
---

**Side panel**:
The fixed column on the right of the TUI's transcript slot that keeps live session state in view on
a wide terminal: sub-agents, the task list, files changed this session and usage, each a
**section** that is `pinned`, `auto` (shown while live) or `hidden`. It is a glance, never a
workspace: a click on a section opens its detail view (**Run view**, the task-list card, `/usage`),
and it takes no keyboard focus. The column itself appears or vanishes only when the window crosses
a width threshold or `ui.side-panel.show` changes, so live content never re-wraps the transcript. Every
section folds from engine **Events**, so **Watch mode** and a resumed session show the same panel.
Ratified 2026-11-07 (ADR 0110); not yet implemented.
_Avoid_: "sidebar" (the docs' word is side panel), "dashboard" (it is display state beside the
transcript, not a separate screen).
