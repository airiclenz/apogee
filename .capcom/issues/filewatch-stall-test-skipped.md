---
title: filewatch stall and burst tests skip under load instead of being fixed
status: open
type: task
priority: 3
plan: 2026-10-13 - 01 - test-readiness-and-model-size-plan
created: 2026-10-12T21:51:20Z
---

The last fix to the filewatch stall flake (`7929556d`, "skip the stall test when it arms past the Settle") made `TestWatchNeverSettlesOnASampleTakenBeforeAStall` (`internal/filewatch/filewatch_test.go`) skip when its pre-arm sleep (`testSettle/5`) overruns the whole Settle on a loaded runner, rather than making the test independent of how late it arms. `TestWatchCoalescesABurstIntoOneReport` skips the same way when its burst takes too close to the Settle. On exactly the boxes where the stall matters — loaded ones — the claim goes unobserved, and a skip reads as green.

The census counts four Linux CI failures in this family (Coalesces / TwoWatchers / NeverSettles, 09-18..10-01), classed "budget + product clock bugs".

**Acceptance:** the stall test arms on a signal from the watcher (or an injected clock) instead of a sleep, so it never needs to skip under load; the same for the burst test's skip.

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §2, the CI failure census row for filewatch.
