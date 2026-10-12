---
title: "Data race on the runOnce seam: a scheduler fire goroutine outlived its test (seen once, CI 2026-09-11)"
status: done
type: bug
priority: 4
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-5tn]
created: 2026-09-17T10:46:23Z
closed: 2026-09-19T17:35:24Z
reason: "Race evidence gathered 2026-09-19. This box cannot run TSan (arm64, 'FATAL: ThreadSanitizer: unsupported VMA range — Found 47 - Supported 48'), so the evidence is CI (ubuntu-latest, x86-64): the 20 ci.yml runs since the fix landed (2026-09-17T17:28Z 45040a0e .. 2026-09-19T16:11Z d75d37da) each ran go test -race over cmd/apogee in one process — 20 serial trials of the leaked-predecessor scenario — with zero 'DATA RACE' lines. The three failed runs in that window (35365775427 heartbeat, 35369373495 filewatch, 35393039500 tui TestTeaSinkEmitsEventsInOrder) were in-flight test failures in other packages, none a race and none in cmd/apogee. CHANGELOG [Unreleased] already records the fix."
---

Run 34590415624 (c061e029): WARNING: DATA RACE — write of runOnce by TestScheduleFiringCarriesTheSessionsSyncLane (schedule_test.go swap of the seam) vs a previous read in scheduleWiring.fire from a Scheduler.tick goroutine created by a prior test. Cascaded into 9 other failures in the same process. Not reproduced since; schedule code refactored 2026-09-15/16 and the seam guard (55d6630f) only covers PARALLEL tests, not a leaked goroutine from a serial predecessor. Check every schedule test closes its Scheduler (Close joins wg) before the runOnce restore Cleanup runs, and whether any test hands a scheduler through Options without closing it.

## Acceptance

Every schedule test that swaps runOnce owns a scheduler.Close that runs before the seam restore; go test -race -count=20 ./cmd/apogee -run 'Schedule|Firing' clean.

## Log

- 2026-09-19 beads: Fix landed 2026-09-17 via docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md item 10 (Close joins every fire goroutine; every test registers scheduler.Close after its runOnce swap). Stays open only for the race evidence: run 'go test -race -count=20 ./cmd/apogee/ -run "Schedule|Firing"' on a TSan-capable host and close on a clean grep for DATA RACE.
