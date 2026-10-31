---
title: Watch mode shows no context gauge, and measures a watched fill against the watcher's window
status: open
type: bug
priority: 2
labels: [tui]
plan: 2026-10-31 - 00 - empty-answers-name-their-cause-plan.md
created: 2026-10-31T08:16:56Z
---

Watching a live session (ADR 0103 decision 5, `internal/tui/watch.go`) usually shows no context gauge in the status line, and when it does show one, it measures against the wrong window.

The gauge (`Model.contextGauge`, `internal/tui/model.go`) is `contextUsage{Used: m.ctxUsed, Limit: m.opts.ContextWindow}`, and `contextUsage.view` paints nothing while either is 0. In a watch:

- `clearWatchView` zeroes `liveStats` on entry and on every repaint, and `paintWatchStart` never relights `ctxUsed` from the record's `Meta.CtxUsed`, although a resume does (`replayResumed`, `resumeLoaded`). The `~` estimate is gone too, because `inertEngine.ContextEstimate` returns 0. So the gauge stays dark until a depth-0, non-maintenance `usage` line lands in the journal. A journal born at a resume or an Activate (`sessionHost.attachEventJournal`, `cmd/apogee/wire_session.go`) has none until the owner's next model reply, which means an owner sitting idle never lights it.
- `Limit` is the watcher's own `m.opts.ContextWindow`, not the watched run's. The journal's `usage` lines already carry `context_window` (`UsageEvent.ContextWindow`), but `foldStats` reads it only for sub-agent heads. A watcher on another model or server, or one watching a session in a watched home, gets a wrong percentage, or no gauge at all when its own window is unknown.

Neither ADR 0103, `docs/manual/sessions.md` nor `layout.md` says the watch hides the gauge, and no test in `watch_test.go` looks at it. It is a gap, not a decision.

## Why

The fill is the one number that says how close the watched run is to compaction or the window's edge, and the watcher is often keeping an eye on a long headless or daemon run for exactly that reason. The record and the journal already hold both halves of the reading.

## Acceptance

- Entering a watch of a session whose record carries `CtxUsed` lights the gauge from it at once, before any journal `usage` line folds.
- A depth-0, non-maintenance `usage` line in the journal moves the gauge. Its `context_window`, when present, is the gauge's limit in the watch, and the watcher's own window is the fallback only when the line names none.
- Maintenance readings and sub-agent readings leave the gauge as they do outside a watch.
- esc restores the watcher's own gauge reading and window exactly as parked, and the watcher's own sessions never see the watched window.
- A test in `internal/tui` drives a watch over a record with `CtxUsed` and a journal holding a `usage` line with a `context_window` different from the watcher's. It asserts the gauge's text in the rendered status line both before and after the line folds, and again after esc.
