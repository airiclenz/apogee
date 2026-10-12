---
title: "Event lines: the one-JSON-line-per-Event rendering `apogee headless --format json` writes"
---

**Event lines**:
The one-line-per-**Event** JSON rendering `apogee headless --format json` writes to stdout. The
**Event stream** it renders is the engine's sequence of Events on `EventSink`, in emission order,
whichever **Driver** reads it — that term means only that, and the lines are one rendering of it.
They are a Driver's protocol, not the engine's surface: the engine stays wire-silent and hands out
Go values, and headless composes the bytes. They carry every Event variant except the **Inspector**'s
raw-protocol one, each as one object with a shared envelope (`event`, `v`, `seq`, `time`, `session`,
`turn`, `depth`, `call_id`, `run_id`) and the variant's own members under `data`; its event names are
snake_case, deliberately distinct from a notice **Moment**'s kebab-case
vocabulary, because the same moment is not filtered the same way in both. They are bracketed by two
frames that are *not* Events — `run_started` and `run_finished`, the latter carrying the run's
summary and exit code and written on every exit path, including one whose run never started. Lossless and ordered: they never drop, the
deliberate contrast with an observe **Reaction**, which does — a stalled
reader stalls the loop instead. Versioned `v:2` and additive within it. See
ADR 0075.
_Avoid_: "Event stream" for the lines (that is the engine's sequence these render), "log" (apogee
owns no log files or retention policy — this is a contract), "API" / "server mode" (a wire surface
would be `apogee serve`, a separate **Driver**).
