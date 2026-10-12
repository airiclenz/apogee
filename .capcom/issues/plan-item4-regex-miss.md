---
title: "Plan 2026-09-16 - 01 item 4: acceptance regex misses TestStandingBlocks_ConfiguredRowsSeedAndRideAlongRowsDoNot"
status: done
type: task
priority: 3
aliases: [apogee-dak]
created: 2026-09-16T13:50:20Z
closed: 2026-09-19T17:41:35Z
reason: Already fixed by d5348d53 (item 4's Acceptance regex gained StandingBlocks; plan line 183 NOTES D4). Stale.
---

Deferred from the implement-plan run of docs/plans/2026-09-16 - 01 - engine-tools-and-provider-deepening-plan.md (item 4, commit 0df4cb6f). The item's acceptance command `go test ./internal/agent -run 'ContextSeam|Orientation|DelegateReport|TaskList|Fence|WrapUp|Fanout|Hook'` does not match TestStandingBlocks_ConfiguredRowsSeedAndRideAlongRowsDoNot (no 'Fence' in its name). The test ran green under the whole-package run and under -run StandingBlocks. Fix: widen the regex in the plan's acceptance line (add StandingBlocks) or rename the test so the existing regex catches it. Non-regression; recorded at verification, not litigated.
