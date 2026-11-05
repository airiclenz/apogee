---
title: "A workflow run's ending is settled once (architecture review 2026-10-14 #8)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#8), re-verified 2026-11-05 at f7ec72b6. A workflow run still ends along four separate paths — Runner.Run's stage-error exit (internal/workflow/runner.go), driveBackground, StopWorkflow and stopAllBackground (internal/agent/background.go) — each with its own step list, and the agent package still writes the run's phase itself (markStopped calls run.runner.Store.WriteStatus). The review proposes that the workflow library settle every exit and write its phase, with the background manager running one end path. Its symptoms were fixed by items 8 (a stage error records phase failed), 9 (stopping a queued workflow says it never started) and 10 (the re-run predicate renamed hasRerunnableItem) of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; the shape fits ADR 0088 (stop keeps finished work) and ADR 0089 D3 (the finish note).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
