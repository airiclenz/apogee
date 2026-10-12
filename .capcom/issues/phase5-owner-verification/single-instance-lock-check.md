---
title: The advisory single-instance lock, on Windows and on macOS
status: parked
type: task
priority: 3
labels: [macos, owner-run, windows]
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
