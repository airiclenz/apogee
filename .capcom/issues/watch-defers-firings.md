---
title: Watching a running session defers Firings
status: done
type: bug
priority: 2
plan: 2026-10-18 - 00 - watch-mode-fixes-plan.md
created: 2026-10-17T21:42:10Z
closed: 2026-10-18T06:05:48Z
reason: "Resolved by plan 2026-10-18 - 00 - watch-mode-fixes-plan (item 1, archived at 413b60be): watch mode reports this session's own activity to the schedule Gate; TestWatchReportsTheWatchersOwnActivity pins it."
---

`reportActivity` (Update's defer, `internal/tui/schedule.go:252`) reads the watch view's state: watch mode mirrors the owner's run into `m.state` (`internal/tui/watch.go:713`, `stateRunning`), so while watching a running session `quiescent()` is false and `Options.ReportActivity(true)` tells the schedule Gate the owner is busy. This session's `/schedule` Firings are therefore deferred until esc, though no engine of this session is in flight.

Deferred out of plan `2026-10-17 - 03 - live-session-watch-plan`.

## Acceptance

- While watch mode follows a running session, `ReportActivity` reports this session's own activity, not the watched owner's, so a due Firing is not deferred by the watch.
- A test in `internal/tui/` pins it.
