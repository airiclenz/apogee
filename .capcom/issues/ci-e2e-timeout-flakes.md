---
title: "Driven e2e tests: approval golden encodes the live-star blink phase; slow-mode internal/tui exceeds the 30m alarm"
status: done
type: bug
priority: 2
plan: 2026-10-11 - 01 - p1-p2-bugs-plan
aliases: [apogee-ci-e2e-timeout-flakes]
created: 2026-10-11T06:07:45Z
closed: 2026-10-11T21:53:31Z
reason: "Resolved by plan '2026-10-11 - 01 - p1-p2-bugs-plan' (items 4-6: live-star redaction in pane goldens, mark-anchored arm wait, refreshed shard-timings seed; on top of 7c65df39 and 04d2d1a1), archived at 3fb76cff"
---

CI's `fmt / build / test` job (ubuntu-latest, `make test` with APOGEE_TEST_SHARDS=2, race on) intermittently fails driven e2e tests in cmd/apogee on screen-wait timeouts, on commits that cannot have caused them. Four of the last twelve CI runs on main went red this way; two of the commits were docs-only.

Observed (CI run head -> failing tests):

- 4d359634 (2026-10-08): TestE2EOutcomeSlotsCarryTheToolsVerdict; e2e_approval_test.go:335 timed out after 1m0s waiting for "Always allow this session" to leave the screen
- 3e215c2c (2026-10-10, docs-only): TestE2EHostileWrapsUnderItsOwnIndent, TestE2EWidthSurvivesAColourSchemeSwitch
- 10c23a5c (2026-10-10, docs-only merge): TestE2EApprovalForcesALookAtTheControlPlane (e2e_stream_test.go:369, timed out after 1m0s waiting for "40. Item 40."), TestE2EStreamRepaintCeiling, TestE2ELiveStateFollowsTheRunningSession, TestE2EHostileWrapsUnderItsOwnIndent (e2e_reactions_test.go:192, timed out waiting for "Appended the smoke test line"), TestE2EHooksFireFromTheTUI, TestE2EWidthSurvivesAColourSchemeSwitch, TestE2ESmokeInProcess
- da4d6a43 (2026-10-10): tests green; red only on govulncheck (since cleared by ae425873)

The failing set differs run to run, and the tests fail by timing out while waiting for something to appear on screen. That points to load or scheduling sensitivity on the 2-shard race runner, not to a logic defect. It is related to apogee-61g (closed 2026-09-23), which covered the same class on a slow local box under the raw multi-package go test shape; this is the supported `make test` path on CI.

Also seen locally on the Pi 5 during the 2026-10-10 windows-channels plan closeout: under APOGEE_TEST_SLOW=1, TestE2EApprovalKeysAreArmedAfterPaint failed under load and passed 3/3 alone, and the serial internal/tui shard now takes about 34m, past test-shards.sh's 30m TEST_TIMEOUT.

Done when: CI's test job passes on N consecutive main pushes with no e2e timeout, e.g. by finding what starves the screen waits (shard concurrency vs -parallel, wait budgets, a shared fixture) rather than raising timeouts blindly. The local slow-mode 30m alarm for internal/tui is in scope.

## Log

- 2026-10-11 Airic Lenz: Correction 2026-10-11: 3e215c2c and 10c23a5c were NOT docs-only. The 3e215c2c push carried the whole project-config/allow-rules plan (81b63f49..7eae5702). Since then the settings-pane e2e failures (HostileWraps, WidthSurvives, LiveState, SmokeInProcess: the settings list stops on the 'servers' row and section headers are missing) recur identically on every CI run (3e215c2c, 10c23a5c, 8f2b128f) but pass locally. So this is a deterministic CI-only regression, not load flakiness; diagnosis is in progress. The 4d359634 timeout and the local Pi slow-mode items stay as the genuine load-sensitivity part.
- 2026-10-11 Airic Lenz: Rescoped 2026-10-11: the deterministic CI settings-pane failures were a performance regression (tui.Model crossed the 128 KiB stack limit in be3b3149, so value receivers heap-copied the Model), fixed in 8abec1fc with a TestModelFitsTheStack guard. What remains here is genuine load sensitivity: (1) TestE2EApprovalForcesALookAtTheControlPlane's golden (e2e_approval_test.go:96) captures the live-star blink phase (on for the first 500 ms, then blank; layout.md ~1215), so a pane that paints late diffs on star vs blank. It fails under heavy load even at 9bfe92fb. Fix it by redacting the phase or waiting for it, not with a timeout. (2) The 2026-10-08 4d359634 approval-pane timeout. (3) Slow-mode internal/tui takes ~34m on the Pi 5, past test-shards.sh's 30m TEST_TIMEOUT.
