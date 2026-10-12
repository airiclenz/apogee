---
title: "config template: seeded config.yaml documents tool-call-salvage but not undo-snapshots"
status: done
type: task
priority: 2
aliases: [apogee-4hn]
created: 2026-09-07T09:01:01Z
closed: 2026-09-07T09:36:36Z
reason: Closed
---

internal/config/defaults/config.yaml gained no 'undo-snapshots:' stanza (tool-call-salvage lands at internal/config/defaults/config.yaml:631). The seeded template is where a user discovers a file-only key, so the new setting is undiscoverable there. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
