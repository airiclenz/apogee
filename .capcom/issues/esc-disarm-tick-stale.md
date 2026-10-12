---
title: "esc stop: the disarm tick is unversioned, so a late tick can disarm a fresher arm"
status: done
type: bug
priority: 2
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-ukw]
created: 2026-09-22T12:15:36Z
closed: 2026-09-23T06:56:00Z
reason: "Item 7: esc/ctrl+c/flash reset ticks carry generations; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Found while diagnosing the cmd/apogee load flake (TestE2EStreamCancelKeepsWhatArrived), 2026-09-22.

internal/tui/model.go:1612-1613 arms the esc stop by setting m.lastEsc = now and scheduling tea.Tick(escStopWindow, ... escStopResetMsg{}). The message carries no generation or stamp, and model.go:1031-1035 zeroes m.lastEsc for ANY escStopResetMsg it folds. So a tick scheduled for arm N but delivered late - which a backed-up fold queue causes - clears a FRESHER arm N+1 that was set in the meantime, and the user's second press then re-arms instead of confirming. ctrlCResetMsg has the same shape and the same hazard.

Note also that the window is measured at fold time (now := time.Now() at model.go:1603 runs when the KeyPressMsg is dequeued), so two presses well inside a second in the user's fingers do not stop the run if the second sits behind a backlog longer than escStopWindow.

Not a regression: the stop path is byte-identical at 1e112316, e2f3b944, 2327276a and HEAD (verified by hashing handleKey's esc case, the claim walk and finishWorker).

User-visible effect: under heavy load, esc x2 can silently fail to stop an in-flight turn; the hint stays up and a third press is needed. The driven tests no longer notice this because cmd/apogee's stopRun helper re-presses, so the register is now the only record.

Possible fix: stamp escStopResetMsg (and ctrlCResetMsg) with the arm it belongs to and ignore a tick whose stamp is stale.
