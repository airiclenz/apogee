---
title: ⌃c⌃c mid-answer lost the whole session record
status: done
type: bug
priority: 1
aliases: [apogee-944]
created: 2026-09-08T05:49:13Z
closed: 2026-09-08T05:49:29Z
reason: "fixed in this run: finishWorker flushes the settled boundary before the deferred exit"
---

A quit requested while a worker was in flight exited without ever writing the session record: finishWorker's quitting branch returned tea.Quit before the idle flush, on the reasoning that the per-Turn snapshots had already captured every completed Turn. stepToBoundary only snapshots at StatusTurnComplete, so an Exchange the model answers in ONE Step emits no turnSnapshotMsg at all — a ⌃c⌃c during that first answer left nothing on disk and the conversation was gone. Surfaced as a CI flake: TestE2EPopupFramesLists quits straight after the reply's text lands, and on a loaded runner the keypress beat exchangeDoneMsg, so the relaunched /sessions browser found an empty store (run 34155743192).

## Design

finishWorker's quitting branch now runs the closing flush for a settled (stateIdle) end and defers the exit to pumpOrQuit, exactly as quit() does for a clean quit. Safe for the same reason the idle save is: the worker has unwound and the engine is the Update loop's again (C1). A quit landing on a loop fault still leaves the record where the last save put it.

## Acceptance

A ⌃c⌃c while the model is still answering leaves a resumable record; TestE2EQuitMidAnswerStillSavesTheSession (held reply via stubllm await gate) and TestModelDoesNotSaveWhileBusy both pin it.
