# Plan: the stop answer for a queued re-run

**Goal:** The `workflow` tool's `stop` answer for a queued background workflow says what its folder
actually holds: a queued first run is marked stopped; a queued `RerunFailed` keeps its folder's
`done`/`failed` phase and is never claimed stopped.
**Date:** 2026-10-17
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 08d06170
**Closes:** queued-rerun-stop-answer

**Regression check (2026-10-17, 08d06170):**
- all items SAFE

**Sources:**
- `.capcom/issues/queued-rerun-stop-answer.md` (the defect and its Acceptance)
- ADR 0088 (stop keeps finished items), ADR 0089 D4 (the workflow control call's answers)
- `internal/agent/background.go` — `stopWorkflow`, `markStopped`, `RerunFailed`

**Ratified design calls:**
- **Answer wording (queued re-run):** `workflow %s: its queued re-run never started and was dropped from the line; its folder stays %s from its last run, a later re-run still accepts it, and no result follows` — the second verb is the folder's phase (`done` or `failed`). Decided by the plan writer, 2026-10-17, under the issue's Acceptance.
- **Public surface:** `Agent.StopWorkflow(id) error` keeps its signature; only the unexported `stopWorkflow` reports more.

**Standing requirements:**
- skills: coding-standards
- Pi 5 box: run tests only narrowed to one package and one test, `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/agent/`; never two test runs at once; never a package pattern.

**Out of scope:**
- The TUI's `^x` stop path (`workflowStoppedMsg` carries only an error; it words no phase).
- `stopAllBackground`'s best-effort `markStopped` on the way out.

## 1. Word a queued re-run's stop answer by its folder's kept phase — ✅ DONE (2026-10-17)

NOTES (2026-10-17): gofmt realigned the whole workflow-control const block in internal/agent/workflowcall.go for the longer new name workflowControlStoppedQueuedRerun (whitespace only on the other lines).
NOTES (2026-10-17): the Done test now stops the re-run through controlCall and the fresh first run still through StopWorkflow; both re-run tests share a new assertRerunStopAnswer helper in background_test.go.

**What:** fix for issue `queued-rerun-stop-answer`: stopping a queued `RerunFailed` answers `workflowControlStoppedQueued` ("… marked stopped …") though `markStopped` leaves a `done`/`failed` folder as it was.

**Goal:** `workflow{action:"stop"}` on a queued re-run of a `done` or `failed` workflow answers with the ratified re-run wording naming that phase, never "marked stopped"; a queued first run still answers `workflowControlStoppedQueued`; a running one still answers `workflowControlStopped`.

**Approach (assumed at the header base):**
- `markStopped(run)` returns the phase the folder is left in (`workflow.PhaseStopped` when it wrote it, else the kept `PhaseDone`/`PhaseFailed`) alongside its error.
- `stopWorkflow` reports that phase for a queued run (e.g. `(queued bool, kept workflow.Phase, err error)`); `StopWorkflow` discards it. `stopAllBackground` ignores the new return as it ignores the error.
- `workflowStopResult` (`internal/agent/workflowcall.go`) picks `workflowControlStoppedQueued` when the kept phase is stopped, else a new constant `workflowControlStoppedQueuedRerun` in the same const block holding the ratified wording.
- Update the `stopWorkflow`/`workflowStopResult` doc comments to say a queued re-run's answer names its kept phase.

**Files:** internal/agent/background.go; internal/agent/workflowcall.go; internal/agent/background_test.go
**Read first:** internal/agent/background.go — stopWorkflow, markStopped; internal/agent/workflowcall.go — workflowStopResult, workflowControlStoppedQueued;
internal/agent/background_test.go — TestBackground_StoppingAQueuedRerunLeavesTheFolderDone, TestBackground_StoppingAQueuedRerunLeavesTheFolderFailed;
internal/agent/workflowcall_test.go — controlCall; internal/workflow/store.go — PhaseStopped

**Tests:**
- In `internal/agent/background_test.go`, extend `TestBackground_StoppingAQueuedRerunLeavesTheFolderDone` and `TestBackground_StoppingAQueuedRerunLeavesTheFolderFailed` (or add one table test beside them) to stop the queued re-run through `controlCall(a, `{"action":"stop","id":"…"}`)` and assert: not an error; contains `its queued re-run never started` and `its folder stays done` (resp. `failed`); does not contain `marked stopped`; the folder phase is unchanged and a later `RerunFailed` succeeds.
- Keep `TestBackground_StoppingAQueuedWorkflowSaysItNeverStarted` asserting the first-run answer still contains `marked stopped`.

**Acceptance:**
- `go build ./internal/agent/ && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBackground_StoppingAQueued' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBackground_RerunFailed' ./internal/agent/`

**Commit:** `fix(agent): word a queued re-run's stop answer by the phase its folder keeps`
