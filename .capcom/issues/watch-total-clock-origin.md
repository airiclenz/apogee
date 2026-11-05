---
title: Watch mode's [total …] clock counts from when watching began, not the watched session's start
status: open
type: bug
priority: 2
labels: [tui, watch-mode]
plan: 2026-11-05 - 06 - watch-total-clock-origin-plan
created: 2026-11-05T20:45:40Z
---

In **Watch mode** (the read-only view of a **Live session**, ADR 0103), the status line's
`[total …]` readout shows how long the *watcher* has been watching, not how long the watched
session's Exchange has been running. Attach to a session that has been working for 10 minutes and
the total reads `[total 00s]` and counts up from there.

Where it comes from: `totalSuffix` (`internal/tui/model.go`) measures `now` against the top-level
slot's run clock (`m.acts.at(runRef{}).since`), which `enterRunning` / `moveActivity` stamp with the
local wall clock when the slot opens. In a watch that slot opens when the watcher folds the
journal, so the origin is the attach moment.

The data to fix it is already on the wire: every **Event journal** line carries a `time` stamp in
its envelope (`internal/eventjson/writer.go`, `envelope.Time`), including the run's `run_started`
and the Exchange's first events. The watch could seed the total's origin from the journal's own
timestamp for the Exchange in flight instead of from `time.Now()` at attach.

Scope: only the `[total …]` readout is in question here. Whether the per-phrase clock
("thinking · 12s") should also take the journal's origin is a related call to make during design.

## Why

The total is there to tell the human how long the work has taken. In a watch it tells them how long they have been looking, which is wrong as soon as they attach to anything already running.

## Acceptance

- Attaching a watch to a Live session whose Exchange has been running N minutes shows [total ≈N…] at once, not [total 00s].
- The origin comes from the watched session's journal timestamps, not the watcher's wall clock at attach; a journal stamp ahead of the watcher's clock cannot render a negative or epoch-sized total.
- The owner's own (non-watch) [total …] readout is unchanged.
- A driven TUI test covers the attach-mid-Exchange case.
