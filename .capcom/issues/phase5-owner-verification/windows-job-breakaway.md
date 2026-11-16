---
title: Job-object breakaway assertion on Windows
status: open
type: task
priority: 3
labels: [owner-run, windows]
plan: 2026-11-15 - 00 - phase5-ci-automation-plan
aliases: [apogee-2uh.4]
created: 2026-09-03T18:40:51Z
---

- **Job-object breakaway assertion on Windows** (folded here 2026-08-15 from the open-residuals
  sweep's residuals) — the Windows counterpart to the POSIX setsid escape
  (`internal/platform/teardown_unix.go:62`) is prose-only and untested: the job object "does not
  permit breakaway" (`internal/platform/teardown_windows.go:23`), and nothing asserts that a
  descendant cannot leave the job, so the POSIX side's pinned residual has no Windows twin.
  Needs a Windows host.


## Update 2026-11-15: automated by plan [`2026-11-15 - 00 - phase5-ci-automation-plan.md`](docs/plans/2026-11-15%20-%2000%20-%20phase5-ci-automation-plan.md), item 2

- **What the plan adds:** `TestTeardownJobRefusesBreakaway` (Windows-tagged, in
  `internal/platform/teardown_windows_test.go`). A process contained through
  `NewProcessTeardown` calls `CreateProcess` with `CREATE_BREAKAWAY_FROM_JOB` and must get
  `ERROR_ACCESS_DENIED`.
- **Where it runs:** `ci.yml`'s `test-windows` job already runs `./internal/platform/...`,
  so no CI change is needed.
- **Close when:** the first `ci.yml` run after the plan's push is green with that test passing.
  No owner-run step remains.
