---
title: "cmd/apogee: several driven tests are load-sensitive under the raw multi-package go test shape on a slow box"
status: done
type: bug
priority: 2
aliases: [apogee-61g]
created: 2026-09-22T12:41:21Z
closed: 2026-09-23T06:02:50Z
reason: Closed
---

Observed 2026-09-22 on the Raspberry Pi 4 (aarch64, 4 cores, swap near full) while chasing the deferred flake from the 2026-09-20 - 03 plan.

Shape that exposes them (NOT the supported path - it bypasses scripts/test-shards.sh and APOGEE_TEST_SLOW):
  go test -count=1 ./internal/agent/... ./cmd/apogee/... ./internal/tui/...

Observed failures, one per run, 3 of 6 runs after the esc-stop fix landed:
- TestE2EFanOutCeilingRefusesTheOverflow - e2e_fanout_test.go:116, timed out after 5s waiting for "All ten reported." on screen.
- TestE2EApprovalKeysAreArmedAfterPaint - e2e_approval_test.go:185, 'the pane was answered by keys that arrived before it painted'; holdKey (e2e_approval_test.go, holdKeyMax = 12 presses at 5ms) can exhaust its presses before the pane paints under load.
- TestE2EOutcomeSlotsCarryTheToolsVerdict - e2e_outcome_test.go:127, 'the far edit's diff draws no elision rule between regions ten lines apart'.

Before the fix, the same shape produced TestE2EStreamCancelKeepsWhatArrived (1 of 5 runs); that one is fixed by the stopRun helper and did not recur in 6 runs.

These all share the shape the shard plan already describes: scripts/test-shards.sh says the default plan's ~11 concurrent driven tests turn the 5s waits and the 2s leak grace red on this box - load, not logic - and APOGEE_TEST_SLOW=1 exists for it. The open question for this bead is whether that is enough, i.e. whether the sharded path is reliably green here, or whether these three waits want budgets that scale the way submit's and awaitReply's already do.

Worth deciding alongside: agents verifying work keep reaching for the raw multi-package shape, so whatever the answer is should be stated where they will read it (docs/manual/building.md, AGENTS.md).

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af. All three named tests and the documentation requirement are covered — but note that the FIRST of the three was cured by a commit that predates the slow-box plan rather than by a plan item.

  • TestE2EFanOutCeilingRefusesTheOverflow (e2e_fanout_test.go) — no plan item touched this test, and none needed to. Its wait is a plain drv.WaitText, bounded by tuitest.DefaultTimeout, which 31f44f75 ('the kit's backstops tolerate a slow box', 2026-09-22 18:06, already in the plan's base 0fb289d5) raised from 5s to 60s. The same commit made leakGrace = DefaultTimeout, so the 2s leak grace this bead names is now 60s too. Both of the numbers the failure was reported against are gone; the observation in this bead was made earlier the same day, against the pre-cure kit.

  • TestE2EApprovalKeysAreArmedAfterPaint — 383c4f15 and 6f845561. holdKeyMax 12 -> 40, and holdKey is now bounded twice over (by the marker and by the ceiling) and RETURNS its press count, so a caller derives its follow-up from the presses actually sent instead of assuming the ceiling. More to the point, 6f845561 removed the race the test was chasing: the approval pane now arms on the drained input position rather than on a clock, so keys that arrived before the paint cannot answer it.

  • TestE2EOutcomeSlotsCarryTheToolsVerdict (the far edit's elision rule) — 495e0c96 (pre-plan) and 383c4f15. The transcript walk's repaint bound is now 'const repaintBudget = 40 * settled' (10s), derived from the kit's own unit of 'the screen has stopped moving' and kept well under tuitest.DefaultTimeout; its doc names the three full-budget spenders (scrollTranscript, waitForFrameChange, waitForScroll) and states that the seconds it costs a healthy run are the deliberate price of never reading a half-walked surface as the whole one.

  • The open question — whether the sharded path is enough or the waits want scaling — was answered as BOTH, and stated where agents will read it (1600c371): AGENTS.md now says 'make test is the supported way to run the suite' and explains why a raw multi-package go test is not (go test runs packages concurrently and gives each the whole box for its own t.Parallel fan-out, so the driven tests report their neighbours' load as a failure), with the narrow-to-one-package debug form; docs/manual/building.md carries the same and the APOGEE_TEST_SLOW plan; scripts/test-shards.sh no longer quotes the stale 5s/2s figures but names the budgets by their code identifiers.

  Re-run on this box (Pi, unraced — this host cannot run -race at all): all three tests green in one package run, 30.3s.
