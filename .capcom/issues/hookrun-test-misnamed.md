---
title: internal/agent/hookrun_test.go is named after a file that no longer exists
status: done
type: task
priority: 4
aliases: [apogee-tsx]
created: 2026-09-08T14:00:34Z
closed: 2026-09-08T14:18:23Z
reason: "Fixed: the mechanisms: hunk retired and TestApplySettingValidatedSetsRideTheRebind renamed; hookrun_test.go renamed reactioncascade_test.go with TestReactionCascade."
---

Deferred from the Reaction core stage 1 run (docs/plans/2026-09-07 - 01, item 9).

internal/agent/hookrun_test.go survived the deletion of internal/agent/hookrun.go: item 9 rewrote its contents into Reaction dispatch tests but no plan item renamed the file, so internal/agent carries a _test.go twin of a file that is gone. Rename it after what it now tests (the Reaction dispatcher).
