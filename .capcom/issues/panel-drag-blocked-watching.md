---
title: The side panel separator does not drag while watching a session
status: open
type: bug
priority: 2
plan: 2026-11-18 - 00 - settled-tools-and-watch-panel-width-plan.md
created: 2026-11-18T11:22:07Z
---

Reported by the owner (2026-11-18, apogee v0.35.4): while apogee is **watching** another live
session, a drag on the **Side panel**'s separator does nothing. Over SSH and locally it works when
not watching (an earlier report blaming SSH was wrong).

It is blocked on purpose, but the reason does not hold. `adr-0110-amend-1108` (c) says only "In
watch mode the drag and `/panel width` are inert, as `/panel` already is there", and the code
follows it: `panelGripAt` (`internal/tui/sidepanel.go`) returns no grip while `m.watch.isActive`,
so the press is claimed and starts nothing. The manual says the same
(`docs/manual/configuration.md`, "the separator does not drag").

But the width is the viewer's own preference, not the watched session's state: it lives in the
global key `ui.side-panel.width` and writes nothing back to the watched session (ADR 0111). The
code already draws that line for a section fold — "like a task-list card's, it is the viewer's
own preference, so it toggles and writes while watching too" (`followPanelTarget`) — while a pin
stays refused. The width belongs with the fold.

## Wanted

- The separator drag, its double-click reset and `/panel width <n>|reset` work while watching,
  exactly as outside a watch (live reflow, limits, write on release).
- Whatever is genuinely the watched session's stays refused (pins, as now).

## Where

- `panelGripAt` and the `/panel width` watch refusal (`internal/tui/command.go`).
- A dated amendment `adr-0110-amend-1118` superseding (c)'s watch-mode sentence, named on
  ADR 0110's Status line and reading-order list.
- `docs/manual/configuration.md` watch-mode paragraph; `layout.md` if it repeats the rule.

## Why

Watching a live session is when the transcript and panel need room the most, and the width is the viewer's own preference — blocking it protects nothing.

## Acceptance

- Dragging the separator while watching resizes the panel live and persists ui.side-panel.width on release; double-click and /panel width reset restore 36
- Pins stay refused while watching
- A driven test (internal/tuitest + internal/stubllm) pins the drag in watch mode, replacing the test that pins it inert
- adr-0110-amend-1118 records the change; manual and layout docs updated
