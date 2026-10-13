---
title: Esc×2 stop window is measured at fold time, not at the keypress
status: done
type: bug
priority: 2
plan: 2026-10-13 - 00 - open-bugs-sweep-plan
created: 2026-10-12T21:51:16Z
closed: 2026-10-13T09:16:00Z
reason: "Resolved by plan '2026-10-13 - 00 - open-bugs-sweep-plan' item 1 (archived at 77f00a79): esc×2 and ctrl+c×2 disarm only on their reset tick, so a late-folded second press still acts; ADR 0099 records the window."
---

The Esc stop gesture's second-press window is measured when the model folds each `esc` key, not when the human pressed it. `internal/tui/model.go` stamps `m.lastEsc = time.Now()` inside `Update` and compares the second press against `escStopWindow` (1 s) by `time.Now()` again, so a loaded program — a long render, a busy fold queue — can fold the two presses more than a second apart even when they were typed well inside it. The second press then re-arms the gesture instead of stopping the run.

The e2e helper `stopRun` (`cmd/apogee/e2e_smoke_test.go`) re-presses esc×2 up to `stopRunAttempts` times, which keeps tests green and hides the product edge: a human under the same load presses twice and the run keeps going.

**Acceptance:** esc×2 typed inside the window stops the run regardless of how late the model folds the second press (or the window is redefined on purpose, with a decision recorded); a test pins it without relying on `stopRun`'s retry.

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §4, "These were left open and never filed or done"; lever 11 of §8.
