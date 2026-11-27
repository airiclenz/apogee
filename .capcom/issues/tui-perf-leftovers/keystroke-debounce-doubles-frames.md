---
title: Each keystroke arms its own debounce tick, doubling frames while typing
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

Every keystroke schedules its own debounce tick, so typing produces about twice the frames it
needs (results doc §What stays open).

## Acceptance

- A burst of keystrokes arms at most one pending debounce tick; a test counts the frames.
