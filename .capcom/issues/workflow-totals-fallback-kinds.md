---
title: TUI workflow totals fallback counts verify and merge items
status: open
type: bug
priority: 4
plan: 2026-10-17 - 00 - quit-keys-stage-kinds-daemon-layer-plan.md
created: 2026-10-14T15:26:49Z
---

When a workflow block has no tally (running, failed, or an older record), `workflowView.totals()` (`internal/tui/workflowblock.go`) counts every item line, verify and merge stages included. `workflow.TallyOf` counts fan-out stage items only (ADR 0089, 2026-09-30 amendment), so the fallback's "items N" disagrees with the tally the same workflow shows once it finishes.

Found by the 2026-10-14 architecture review (handoff `docs/handoffs/2026-10-14 - 00 - architecture-review-in-passing-defects.md`, minor list) and confirmed at 313f03e3. Split out of the `2026-10-14 - 01 - architecture-review-defects-plan` because the fix is wider than it looks: no stage kind reaches the TUI today.

Design sketch from that plan's regression check: carry the stage kind from `workflow.StageEvent.Kind` through `workflowObserver.StagePhase` onto `WorkflowStageStarted` (or alongside `Stages` on `WorkflowStarted`) in `internal/domain/events.go`, into `workflowStage` and `session.WorkflowStage` (omitempty) via `toWireWorkflow`/`fromWireWorkflow`, and name the member in `internal/eventjson/encode.go`'s in-process-only comment. A record written before the kind was kept has none and keeps counting every item.

## Acceptance

- A failed workflow with a verify stage shows "items N" equal to its fan-out item count.
- `TestWorkflowBlockWithoutATallyCountsItsItems` pins the fan-out count; an older record without stage kinds still counts every item (`TestWorkflowBlockReplaysTheEndPhaseTally`).
- Session transcripts written before the change still load.
