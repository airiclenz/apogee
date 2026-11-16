---
title: The advisory single-instance lock, on Windows and on macOS
status: open
type: task
priority: 3
labels: [macos, owner-run, windows]
plan: 2026-11-15 - 00 - phase5-ci-automation-plan
aliases: [apogee-2uh.5]
created: 2026-09-03T18:40:51Z
---

- **The advisory single-instance lock, on Windows and on macOS** (folded here 2026-08-22 from the
  residuals of `docs/plans/archived/2026-08-22 - 02 - daemon-plan.md` item 4) — the lock that keeps
  one `apogee daemon` per apogee home is `flock(2)` on POSIX (`internal/platform/lock_unix.go:29`)
  and `LockFileEx` translating a refused wait into `ERROR_LOCK_VIOLATION` on Windows
  (`internal/platform/lock_windows.go:47-58`). Both non-Linux halves are COMPILE-verified only from
  this host: `internal/platform/lock_test.go`'s contention and release cases run natively on Linux
  alone. What stays open is the runtime assertion on the other two — a second handle refused while
  the first holds it, the lock dropped when the process dies — which belongs with the live daemon
  activation pass `CHANGELOG.md` already defers to the owner.


## Update 2026-11-15: automated by plan [`2026-11-15 - 00 - phase5-ci-automation-plan.md`](docs/plans/2026-11-15%20-%2000%20-%20phase5-ci-automation-plan.md), item 1 (macOS: item 4)

- **Correction:** the "Linux alone" claim above is out of date. `lock_test.go` has no build tag,
  and `ci.yml`'s `test-windows` job has run all of `./internal/platform/...` since
  2026-08-29. So the contention and release cases already run on Windows.
- **What the plan adds:** `TestAcquireLockFreedWhenHolderDies`. A re-executed holder process
  takes the lock, a second acquire is refused and `ProbeLock` reports the holder's PID, the
  holder is killed, and the lock is then acquirable. It runs on Linux (`make check`), on Windows
  (`test-windows` in `ci.yml`) and on macOS (the new `macos.yml`).
- **Close when:** the first `ci.yml` run and the first `macos.yml` run after the plan's
  push are both green with that test passing. No owner-run step remains.
