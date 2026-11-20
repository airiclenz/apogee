---
title: "A lone tool call is no tool group: it cannot be re-folded, and the group header paints white"
status: open
type: bug
priority: 2
plan: 2026-11-20 - 00 - tool-group-of-one-plan.md
created: 2026-11-20T20:00:13Z
---

Under `ui.tools-auto-collapse` a settled lone call folds to its Group summary, but once the reader opens it, it cannot be folded again: the open lone call paints as a plain tool block with no fold header (`blockFoldBy` returns `foldByNone` for a touched lone call). A `✦ Tools` umbrella, by contrast, re-folds on click. Lone delegations and lone skill fetches are not groups at all. Separately, the Group summary phrase on a group header paints in the terminal default (white) — `th.toolHeader` has no foreground — instead of the tools header colour every individual tool header uses (`th.toolLabel`).

Owner decisions 2026-11-20: every call is a tool group — a group of one — including a lone delegation (`Sub-agents (1)`) and a lone skill fetch (`Skill (1)`); an open group of one shows the call's own block directly under the group header (no type row); with auto-collapse off it renders as an open group of one; the Group summary paints in `toolLabel` (bold + `tool-header` colour).

## Why

Unified fold behaviour: every block folds, opens and re-folds the same way, and the group header reads as a tools header.

## Acceptance

- A lone call, once opened, folds again on click or ⏎, auto-collapse on or off
- A lone delegation and a lone skill fetch render as `Sub-agents (1)` / `Skill (1)` groups and fold like any group
- The Group summary phrase paints in the tools header colour
