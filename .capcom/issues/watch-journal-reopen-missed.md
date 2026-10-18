---
title: Watch misses a journal reopened within one poll
status: open
type: bug
priority: 2
plan: 2026-10-18 - 00 - watch-mode-fixes-plan.md
created: 2026-10-17T21:42:10Z
---

`readJournal` (`internal/tui/watch.go:534`) spots truncation only by inode+size (`followedJournal`, `isSameFile` at `internal/tui/watch.go:600`; the `info.Size() < offset` check in `readJournal`). If the owner reopens the same id (`O_TRUNC`, or delete and recreate with a reused inode) and the journal grows past the old offset within one 100 ms poll, the new `run_started` at byte 0 is missed and the read resumes mid-file.

Deferred out of plan `2026-10-17 - 03 - live-session-watch-plan`.

## Acceptance

- A watch over a journal that is truncated (or replaced under a reused inode) and regrown past the follower's offset between two polls restarts at byte 0 and folds the new `run_started`.
- A test in `internal/tui/` pins that case.
