---
title: Phase-5 verification leftovers — the owner-run passes this machine cannot perform
status: parked
type: epic
priority: 3
labels: [owner-run]
aliases: [apogee-2uh]
created: 2026-09-03T18:40:49Z
---

**Status:** carried forward 2026-07-22 from the "Owner-run checklist" of the archived
[`docs/plans/archived/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`](docs/plans/archived/2026-07-22%20-%2000%20-%20phase5-cross-platform-hardening-plan.md)
(read it for full context). What the checklist's runnable passes covered is in `CHANGELOG.md` and
the archived plans; what remains open needs hardware this environment does not have:


**Not repeated here:** the "pre-existing, NOT Phase 5 scope" group (Linux/macOS live-run
variants already tracked elsewhere) lives in the CHANGELOG's **"Known post-release verification
(owner-run / CI)"** note — that note, not this file, is its tracking home.


## Split, 2026-11-15

The six children now fall in two groups. Estimated owner time is about 1.5–2 h plus
set-up on each machine.

- **Automated by plan [`2026-11-15 - 00 - phase5-ci-automation-plan.md`](docs/plans/2026-11-15%20-%2000%20-%20phase5-ci-automation-plan.md):** `windows-job-breakaway`,
  `single-instance-lock-check`, and the `/dev/null` half of `macos-confined-live-run`.
  The new tests run in CI: the Windows ones in `ci.yml`'s `test-windows` job, the macOS ones
  in the new non-gating `.github/workflows/macos.yml`. The owner closes each issue after the
  first green run following the push.
- **Owner-run, at the machine:** `macos-confined-live-run` (live session), `windows-confined-live-run`,
  `windows-conpty-console-check`. Each child's `## Owner-run steps` section says what to do.
- **Only if such a host turns up:** `windows-below-floor-notice`.
