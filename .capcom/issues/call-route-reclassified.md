---
title: "A call's route is classified once (architecture review 2026-10-14 #18)"
status: parked
type: task
priority: 4
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#18), re-verified 2026-11-05 at f7ec72b6. Speculative. "Is this a delegation?" is still asked several times per call (isDelegationCall from partitionDispatch, the run-id mint, preempt and ceiling in internal/agent/dispatch.go) against two call values — the call as emitted and the call after a pre-tool-exec Reaction, which may rename it through ToolCallEdit.SetTool (internal/domain/tooledit.go). The review proposes classifying the route once after pre-tool-exec (leaf, delegation, background workflow, control), carrying it on the dispatch slot and partitioning by it, so an embedder's Go Reaction that redirects a pooled sub_agent into a leaf could not run that leaf on a pool worker; latent, no in-tree caller does this. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched it. It must be checked against ADR 0039 D11 before planning: the emitted-order partition must hold, and moving the partition after pre-tool-exec changes when that Moment fires relative to the leaf group.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
