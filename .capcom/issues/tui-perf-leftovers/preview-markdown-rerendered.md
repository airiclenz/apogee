---
title: The streaming preview is re-rendered as markdown from scratch on every batch
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

Plan 08 item 6 cut a streamed batch to the preview tail, but within that tail `renderMarkdownBody`
runs cold each time (results doc §What stays open).

## Acceptance

- Appending to the preview renders only the changed tail of the markdown; the frame matches a cold render.
