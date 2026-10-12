---
title: Make the e2e tests genuinely parallel (t.Setenv + CheckLeaks)
status: done
type: task
priority: 3
plan: 2026-09-13 - 00 - headless-ci-hygiene-plan
aliases: [apogee-ed5]
created: 2026-09-06T10:33:30Z
closed: 2026-09-14T05:48:26Z
reason: cmd/apogee half delivered by plan 2026-09-13 - 00 items 5-7, commits ed6fe305 (APOGEE_* cleared once in TestMain), 1ec05352 (per-test leak attribution via pprof labels), 52e0d677 (t.Parallel sweep, 202s -> 66s) plus 4295f196 (per-shard -parallel bound); the internal/tui sweep is apogee-11s; CHANGELOG entries landed in f7d5f44b
---

Process sharding (scripts/test-shards.sh) works around two constraints rather than removing them. Removing them would let the 189 serial tests in cmd/apogee and the 1267 in internal/tui overlap inside one process, on top of the sharding:

1. `assertNoAmbientApogeeConfig` (cmd/apogee/e2e_support_test.go:542) calls `t.Setenv` on six APOGEE_* vars; the testing package panics if a test uses both t.Setenv and t.Parallel. Fix looks cheap: neutralise the environment once in TestMain with os.Unsetenv (TestMain already overrides HOME for the same reason) and leave the helper as a pure assertion. Tests that deliberately SET those vars (headless_test.go, keymigrate_test.go) stay serial, and they are fast.

2. `tuitest.CheckLeaks` (internal/tuitest/leak.go:75) snapshots the process-wide goroutine dump and reports anything new alive at cleanup, so under parallel tests one test's cleanup reads another's live goroutines as leaks. Needs per-test attribution (pprof labels on apogee's goroutines) or a separate serial leak-check pass. This half is a real refactor and is what makes the item non-trivial.

Measured baseline for whoever picks this up: cmd/apogee 189 serial tests / 168.1s vs 404 parallel tests / 5.0s; internal/tui 1267 / 98.3s vs 498 / 3.8s.

## Acceptance

The e2e tests run with t.Parallel without false leak reports, and the suite is measurably faster than sharding alone.
