---
title: "A delegation's outcome stays typed in the TUI (architecture review 2026-10-14 #2)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#2), re-verified 2026-11-05 at f7ec72b6. The TUI turns the engine's typed DelegationOutcome into summary words (delegationOutcomeVerdict, internal/tui/toolregistry.go) and then reads its own words back through four matchers in internal/tui/toolleader.go (failedSummary, succeededSummary, endedWithoutReportSummary, stoppedSummary), while delegationNeverStartedVerdict, delegationStoppedByUser and entry.neverStarted (transcript.go) read copied engine prose. The review proposes that the branch summary carry the outcome kind and one delegation state answer queued / running / reported / never-started, with words only painted and only replay parsing them through one parser, so a new outcome kind is a one-file change. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched this shape; ADR 0083 §1 stands (the record keeps rendered verdicts and replay keeps its parser), and it composes with the child-run ending work filed separately (review #1).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
