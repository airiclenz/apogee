---
title: "run: TestOnceWithNoApogeeHomeKeepsTheInMemoryJournal asserts too little"
status: done
type: task
priority: 2
aliases: [apogee-4r0]
created: 2026-09-07T09:01:01Z
closed: 2026-09-07T09:36:37Z
reason: Closed
---

internal/run/run_test.go:2101 — the test only asserts the Firing survives with no apogee home. Neither the 'no apogee home' reason carried on the journal note nor the test's own 'nothing written outside the workspace' claim is checked. Deferred out of plan 'docs/plans/2026-09-06 - 02 - undo-snapshots-and-tool-call-salvage-plan.md'.
