---
title: ADR 0099 decision
---

1. **The window runs from the first press's fold until its reset tick folds.** A second press
   folded while the gesture is armed (`lastEsc` / `lastCtrlC` non-zero) stops the worker or quits,
   however much wall-clock time passed between the two folds. `handleKey` makes no time compare.
2. **Only the gesture's own reset message disarms it** — the one whose `gen` matches the current
   arm (`escGen`, `ctrlCGen`); a stale tick from an earlier arm stays a no-op. The esc gesture is
   also disarmed when the worker unwinds (`finishWorker`), since nothing is left to stop.
3. **The stamps stay `time.Time`, zero meaning disarmed.** The status line's armed hint keys on
   the same state, so the hint shows exactly while a second press would act.
4. `escStopWindow` and `ctrlCQuitWindow` remain, as the delay of the reset tick only.

The owner ratified this on 2026-10-13 in `docs/plans/2026-10-13 - 00 - open-bugs-sweep-plan.md`
(item 1).
