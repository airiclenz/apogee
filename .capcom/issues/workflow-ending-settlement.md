---
title: "A workflow run's ending is settled once (architecture review 2026-10-14 #8)"
status: done
type: task
priority: 3
plan: 2026-11-23 - 03 - workflow-ending-settlement-plan
created: 2026-11-05T19:56:22Z
closed: 2026-11-23T12:22:46Z
reason: "Pursued and delivered by plan '2026-11-23 - 03 - workflow-ending-settlement-plan' (items 1-7, archived at 604f36ab): internal/workflow settles every exit and is the sole phase writer, one workflow.Ending feeds note/event/answer, the background manager ends a run through one path; three drifts fixed."
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#8), re-verified 2026-11-05 at f7ec72b6. A workflow run still ends along four separate paths — Runner.Run's stage-error exit (internal/workflow/runner.go), driveBackground, StopWorkflow and stopAllBackground (internal/agent/background.go) — each with its own step list, and the agent package still writes the run's phase itself (markStopped calls run.runner.Store.WriteStatus). The review proposes that the workflow library settle every exit and write its phase, with the background manager running one end path. Its symptoms were fixed by items 8 (a stage error records phase failed), 9 (stopping a queued workflow says it never started) and 10 (the re-run predicate renamed hasRerunnableItem) of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; the shape fits ADR 0088 (stop keeps finished work) and ADR 0089 D3 (the finish note).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): VALID — four end paths unchanged (workflow/runner.go:332; agent/background.go:606,324,633)
