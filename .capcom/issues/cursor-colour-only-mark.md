---
title: Block cursor highlight relies on colour alone
status: open
type: bug
priority: 3
created: 2026-11-08T21:33:39Z
---

The transcript block cursor marks its row with the selection tone only
(`highlightBlockCursor`, `internal/tui/blockcursor.go`), against ADR 0052 decision 4
("colour never carries a change alone"). ADR 0111 decision 6: both the transcript and the
panel cursor mark their row with a `▌` in the left gutter as well as the tone.

## Acceptance

- With colour stripped (a no-colour profile), the cursor's row is still distinguishable in a rendered frame, in the transcript and in the panel.
- The mark keeps its shape under `ui.glyphs: safe` or has a safe fallback.
