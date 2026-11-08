---
title: "Side panel: the right-hand column that keeps live session state in view"
refs: [docs/layout/side-panel-layout.md]
---

**Side panel**:
The column on the right of the TUI's transcript slot that keeps live session state in view on
a wide terminal: sub-agents, the task list, files changed this session and usage, each a
**section** that is `pinned`, `auto` (shown while live) or `hidden`. It is a glance, never a
workspace: a click on a section opens its detail view (**Run view**, the task-list card, `/usage`),
and it takes no keyboard focus. The column itself appears or vanishes only when the window crosses
a width threshold or `ui.side-panel.show` changes. It is 36 columns wide by default, and its width
changes only when the user drags its separator (at least 24 columns, and never so wide that the
transcript drops under its floor), double-clicks the separator to reset it, or sets
`ui.side-panel.width` (`/panel width <n>|reset`, `/settings`), so live content never re-wraps the
transcript. Every
section folds from engine **Events**, so **Watch mode** and a resumed session show the same panel.
Ratified 2026-11-07 (ADR 0110); shipped 2026-11-08, set with `/panel` and the `ui.side-panel` keys;
the width made user-set 2026-11-08 (`adr-0110-amend-1108`).
_Avoid_: "sidebar" (the docs' word is side panel), "dashboard" (it is display state beside the
transcript, not a separate screen).
