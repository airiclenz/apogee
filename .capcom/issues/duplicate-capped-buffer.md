---
title: "tools: cappedBuffer duplicates subprocess.CappedBuffer"
status: done
type: task
priority: 3
aliases: [apogee-h59]
created: 2026-09-07T09:01:02Z
closed: 2026-09-07T09:36:36Z
reason: Closed
---

internal/tools/exec_common.go:423 — after the internal/subprocess extraction, tools' unexported cappedBuffer is a near-duplicate of subprocess.CappedBuffer. Item 7's regression guard required tools keep its own unexported-field version for internal/tools/console_common.go:250; a later change could give console_common.go the exported one and delete the copy. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
