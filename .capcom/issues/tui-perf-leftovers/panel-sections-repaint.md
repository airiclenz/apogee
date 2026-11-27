---
title: The side panel's tasks, sub-agents, usage and MCP sections repaint every frame
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

Only the changed-files section is memoised (plan 07 item 6); the other four sections are
re-derived and repainted every frame (results doc §What stays open).

## Acceptance

- Each of the four sections repaints only when its inputs change; a test per section pins it.
