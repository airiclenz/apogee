---
title: Workflow stage row reads 'done' for a failed merge stage or a skipped stage
status: done
type: bug
priority: 3
aliases: [apogee-stage-row-outcome-unknown]
created: 2026-09-28T20:29:58Z
closed: 2026-09-29T06:49:49Z
reason: "Fixed in 554261e0: StageFinished carries the stage outcome and the TUI row paints it"
---

Deferred from plan docs/plans/2026-09-28 - 00 - enterable-workflow-stages-plan.md (item 5). WorkflowStageFinished carries no stage outcome (internal/domain/events.go:711; emitted by workflowObserver.StagePhase, internal/agent/workflowcall.go:637/651), so the TUI derives it (workflowStage.endState, internal/tui/workflowblock.go:326): a merge stage that fails on a missing report, and a skipped stage, both read 'done' on their row. Fixing this needs the engine to put the stage's outcome on the event in StagePhase.
