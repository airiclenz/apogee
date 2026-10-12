---
title: example_test.go does not guard the three ApprovalPhase re-exports
status: done
type: task
priority: 3
aliases: [apogee-apz.1]
created: 2026-09-06T19:16:50Z
closed: 2026-09-06T20:08:58Z
reason: delivered by docs/plans/archived/2026-09-06 - 03 - hooks-deferred-findings-plan.md
---

The facade compile-guard in example_test.go was extended for item 1's re-exports but not item 2's: `_ apogee.ApprovalPhase`, `_ = apogee.ApprovalRequested` and `_ = apogee.ApprovalDecided` are missing, so those three symbols could be renamed or dropped from apogee.go without the guard failing. Add the three lines beside the existing `_ apogee.TurnEvent` block. Cite: example_test.go.
