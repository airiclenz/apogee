---
title: "cmd/apogee/settingsedit_test.go: the mechanisms: hunk passes for the wrong reason"
status: done
type: bug
priority: 3
aliases: [apogee-ta5]
created: 2026-09-08T14:00:27Z
closed: 2026-09-08T14:18:23Z
reason: "Fixed: the mechanisms: hunk retired and TestApplySettingValidatedSetsRideTheRebind renamed; hookrun_test.go renamed reactioncascade_test.go with TestReactionCascade."
---

Deferred from the Reaction core stage 1 run (docs/plans/2026-09-07 - 01, item 10/11).

cmd/apogee/settingsedit_test.go:433-441 — TestApplySettingMechanismBlocksRideTheRebind's mechanisms: hunk now passes because the settings table has NO mechanisms entry at all (so the apply is refused as cannotApply), not because the rebind is blocked; the hunk's comment states the old reason and is false. No plan item claimed the file, so the hunk and the test's name want retiring.
