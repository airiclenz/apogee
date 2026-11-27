---
title: The blinking live star repaints the whole transcript twice a second
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

A spinner tick with a live call walks every block: 2.32 ms against 1.66 ms settled at N=300
(results doc §What stays open). Splicing only the live blocks would remove the walk.

## Acceptance

- A spinner tick re-renders only the blocks with a live call; a test pins that settled blocks are not repainted.
