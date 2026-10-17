---
title: Stopping a queued re-run claims its folder was marked stopped
status: done
type: bug
priority: 2
plan: 2026-10-17 - 00 - queued-rerun-stop-answer-plan
created: 2026-10-14T17:50:59Z
closed: 2026-10-17T10:22:10Z
reason: "Resolved by plan 2026-10-17 - 00 - queued-rerun-stop-answer-plan (archived at 6853932f): a queued re-run's stop answer names its folder's kept done/failed phase; a queued first run still answers marked stopped."
---

Found by the closeout of the plan `2026-10-14 - 01 - architecture-review-defects-plan` (item 9).

The `workflow` tool's `stop` on a background workflow still waiting in line answers
`workflowControlStoppedQueued` (`internal/agent/workflowcall.go:960`, returned at
`internal/agent/workflowcall.go:1054`): "was queued and never started; it was dropped from the line
and marked stopped, and no result follows". For a queued `RerunFailed` the folder is not marked
stopped: `markStopped` (`internal/agent/background.go:737`) leaves a folder whose phase is `done` or
`failed` as it was, so the answer tells the model a phase the folder does not carry.

## Acceptance

- Stopping a queued re-run of a `done` or `failed` workflow answers in words that match its folder's
  phase (it keeps `done`/`failed`, and a later re-run still accepts it), and never claims it was
  marked stopped.
- Stopping a queued first run still answers that it was marked stopped.
- A test in `internal/agent/background_test.go` pins both answers.
