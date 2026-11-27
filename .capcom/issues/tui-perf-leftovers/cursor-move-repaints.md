---
title: Moving the block cursor repaints an unchanged transcript
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

Moving the block cursor, or jumping from the side panel, repaints a transcript whose content did
not change (results doc §What stays open: "park without repainting").

## Acceptance

- A cursor move or panel jump repaints only the rows whose marker changed; a test pins that the transcript cache is not rebuilt.
