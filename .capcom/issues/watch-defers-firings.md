---
title: Watching a running session defers Firings
status: open
type: bug
priority: 2
created: 2026-10-17T21:42:10Z
---

`reportActivity` (Update's defer, `internal/tui/schedule.go:252`) reads the watch view's state: watch mode mirrors the owner's run into `m.state` (`internal/tui/watch.go:713`, `stateRunning`), so while watching a running session `quiescent()` is false and `Options.ReportActivity(true)` tells the schedule Gate the owner is busy. This session's `/schedule` Firings are therefore deferred until esc, though no engine of this session is in flight.

Deferred out of plan `2026-10-17 - 03 - live-session-watch-plan`.

## Acceptance

- While watch mode follows a running session, `ReportActivity` reports this session's own activity, not the watched owner's, so a due Firing is not deferred by the watch.
- A test in `internal/tui/` pins it.
