---
title: "e2e typing waits: fixed 5s budget for long prompts + 5ms full-frame pane watcher make the announced tests time out on CI"
status: done
type: bug
priority: 2
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-pqs]
created: 2026-09-17T10:46:22Z
closed: 2026-09-19T16:00:10Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

6 of the last 11 red CI runs: TestE2EAnnouncedScratchDirIsReadableByTheReadTools 'timed out after 5s waiting for the typed prompt to appear in the prompt box' with 56 of 79 chars landed (~90ms/key on the 4-vCPU runner vs ~10ms locally under -race; 20ms on one pinned core). Two contributors: (1) submit() gives every prompt the same DefaultTimeout (5s) regardless of length — this test types the suite's longest prompt (announcedScratchReadPrompt, 79 chars); (2) watchApprovalPanes (e2e_announced_test.go:1102, paneWatchInterval = 5ms) rebuilds the full Frame every 5ms — Screen.Snapshot is 25% of the test's CPU and holds Screen.mu against the program's writes; measured locally on one core the typing phase drops 1.6s -> 1.0s with the watcher slowed. The 11 tests using that watcher are exactly the announced tests that flake. Fix: gate the watcher on Screen.BytesWritten() like stepSettings does (read a frame only after a paint) and/or widen its interval; scale submit()'s wait with the prompt length (tuitest.Within(DefaultTimeout + per-char allowance)) or type in chunks and wait per chunk. Do not weaken the assertion.

## Acceptance

Announced e2e tests pass 20x under -race pinned to one core; watchApprovalPanes no longer samples on a fixed 5ms tick.
