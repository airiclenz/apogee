---
title: e2e tests type their prompts key by key where the claim is not about typing
status: open
type: task
priority: 4
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T12:28:48Z
---

Driven e2e tests enter their prompts one key at a time even when the test's claim has nothing to do with typing, which costs wall time in `make test`.

**Policy call first:** pasting prompts (bracketed paste) in those tests weakens coverage of the typed-key path. Do it only if profiling shows typing dominates the driven tests' time, and keep typed input in every test whose claim is about keys.

**Acceptance:** a profile justifies the change; tests whose claim is typing still type; suite wall time drops.

**Measurement (2026-10-12, plan item 27):** typing is **49%** of the measured e2e test time — 196.3 s of 400.4 s. Verdict: **paste**.

- Interval measured: from `submit`/`submitNow` calling `Type` to the prompt tail showing in the prompt box, summed over every call (94) with an atomic accumulator printed by `TestMain`. The instrumentation was temporary and is reverted.
- Tests measured: the 20 cmd/apogee e2e tests with the most `submit`/`submitNow` calls (every test with three or more): TestE2ESmokeInProcess, TestE2EConsolesDieWithTheirOwner, TestE2EEgress, TestE2EDelegationStepCap, TestE2EPopupFramesPrompts, TestE2EUsageReportsCachedTokensAndDelegateSpend, TestE2EClearResetsUsage, TestE2EForkKeepsThePrefixAndSurvivesAResume, TestE2EApprovalForcesALookAtTheControlPlane, TestE2EApprovalKeysAreArmedAfterPaint, TestE2EBackgroundSessionSwitchDoesNotHang, TestE2EBackgroundSubAgent, TestE2EClearEmptiesTheThinkingPane, TestE2EGuardControlPlane, TestE2ELiveStateFollowsTheRunningSession, TestE2ELiveStateLauncherMoveKeepsTheSessionWorking, TestE2EPopupClickLists, TestE2EPopupFramesLists, TestE2EQueuedCommandRunsAtIdle, TestE2ESmokePTY.
- Run: one `GOMEMLIMIT=2GiB go test -race -count=1 -json -run '<those 20>' ./cmd/apogee/` on a Raspberry Pi 5 (4 cores, go1.26.9), the tests under `t.Parallel` at the default parallelism; package wall time 151.3 s. The denominator is the summed per-test elapsed from `go test -json`, not the wall time.
- The share is an **upper bound** on what pasting saves: the interval is the program's per-key fold cost plus the paint that shows the tail, and a pasted prompt still pays one paint. The 10% threshold applies to it all the same, and 49% clears it with room to spare.

Source: option F of the archived handoff `docs/handoffs/archived/2026-10-11 - 01 - make-check-slowness-and-oom.md` (local only).
