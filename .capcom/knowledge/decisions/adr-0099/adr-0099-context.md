---
title: ADR 0099 context
---

The TUI's two double-press gestures — `esc` twice stops the running worker, `ctrl+c` twice quits —
were introduced with a one-second window (`docs/plans/archived/2026-08-29 - 01 - double-esc-abort-plan.md`).
The first press stamps `Model.lastEsc` / `Model.lastCtrlC` and schedules a generation-stamped reset
tick (`escStopResetMsg`, `ctrlCResetMsg`) one window later. The second press then compared
`time.Now()` against the stamp and stopped or quit only when the difference was inside the window.

That compare ran when the second press was FOLDED, not when it was typed. Under load — mid-reply,
with a parallel test suite on the same cores — Bubble Tea's fold queue can hold a key for a second
or more, so two presses typed 70 ms apart were folded more than a window apart. The second press
then re-armed the gesture instead of acting: the run carried on, and the e2e `stopRun` helper had
to retry presses to get through (issue `esc-disarm-fold-time`).

The reset tick already bounds the window. Bubble Tea delivers messages in order, so a second press
typed before the tick fired reaches the model ahead of the tick, however far behind the fold
queue is. The wall-clock compare added nothing but the failure.
