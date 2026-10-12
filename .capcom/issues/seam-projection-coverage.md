---
title: Runner-level coverage for four of the five seam value projections
status: done
type: task
priority: 3
aliases: [apogee-fso]
created: 2026-09-09T03:23:30Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): only `projectSeamValue` unit tests (`internal/reactions/payload_test.go`) plus one history-rewrite case exercise the `value` projection. Four of the five seams have no end-to-end Runner-level test proving the projected shape reaches a fired entry's stdin (`internal/reactions/payload.go`, `internal/reactions/match.go`).
