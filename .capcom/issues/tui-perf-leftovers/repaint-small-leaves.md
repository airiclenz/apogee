---
title: Smaller repaint costs left from audit item 11
status: open
type: task
priority: 4
created: 2026-11-27T18:35:35Z
---

From the audit's item 11 (results doc §What stays open): a tool group's shape is still worked out
several times per repaint; the line slices lack capacity hints; the detail tone (`detailTone`,
`paintRowMarker`) still goes through lipgloss, about 4% of the resize profile.

## Acceptance

- Each of the three costs is removed or recorded as not worth it, with the profile before and after.
