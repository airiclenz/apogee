---
title: Job-object breakaway assertion on Windows
status: parked
type: task
priority: 3
labels: [owner-run, windows]
aliases: [apogee-2uh.4]
created: 2026-09-03T18:40:51Z
---

- **Job-object breakaway assertion on Windows** (folded here 2026-08-15 from the open-residuals
  sweep's residuals) — the Windows counterpart to the POSIX setsid escape
  (`internal/platform/teardown_unix.go:62`) is prose-only and untested: the job object "does not
  permit breakaway" (`internal/platform/teardown_windows.go:23`), and nothing asserts that a
  descendant cannot leave the job, so the POSIX side's pinned residual has no Windows twin.
  Needs a Windows host.
