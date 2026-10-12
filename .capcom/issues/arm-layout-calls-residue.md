---
title: Update arms still call layout()/refreshViewport() themselves
status: done
type: task
priority: 4
aliases: [apogee-arm-layout-calls-residue]
created: 2026-09-24T09:16:55Z
closed: 2026-10-01T17:31:42Z
reason: "Resolved by plan '2026-10-01 - 00 - compaction-notice-and-layout-residue-plan' items 2-6 (archived in 6c0756a2): arms outside model.go leave layout to Update's tail; TestArmsLeaveLayoutToTail guards it."
---

internal/tui/doc.go says an arm mutates and Update's tail lays out (dirty mark), but ~35 layout()/refreshViewport() belt calls remain in ~22 files outside model.go. Remove them where the dirty mark already covers the arm. Found 2026-09-24 (architecture-review card #17 follow-through).
