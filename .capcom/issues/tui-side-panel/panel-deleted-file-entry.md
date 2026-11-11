---
title: Side panel keeps listing a file after it is deleted
status: done
type: bug
priority: 2
plan: 2026-11-11 - 03 - junction-save-and-panel-deleted-file-plan
created: 2026-11-11T20:07:12Z
closed: 2026-11-11T21:28:36Z
reason: "Resolved by plan '2026-11-11 - 03 - junction-save-and-panel-deleted-file-plan' items 2-4 (archived at d3b7806a): delete_file folds and replays as a row removal, and rows whose file is gone from disk are hidden."
---

When a file tracked in the side panel's changed-files list is deleted from the
workspace, the panel keeps listing it.

Reproduced in this session: two scratch files under `probe2/` were deleted
from the workspace, but they remained in the side panel's changed-files list
afterwards.

## Acceptance

- Deleting a file that appears in the side panel's changed-files list removes
  it from the list (immediately, or on the next refresh of the panel's file
  state — whichever the panel's change-watching design already uses).
- Files that still exist keep their entries; no unrelated churn.
