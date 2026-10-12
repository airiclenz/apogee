---
title: ADR 0082 consequences
---

- `internal/provider`: `Stream` gains the idle cut (`errStreamIdle`, `WithStreamIdleTimeout`,
  `Client.StreamIdleTimeout`), `isTransientReadError` gains the third class. `internal/config`:
  `stream-idle-timeout:` and `re-stream-budget:` registry rows with their validators;
  `domain.Config` gains `StreamIdleTimeout` and `RestreamBudget`. `internal/agent`:
  `restreamBudget`, `restreamHoldoffFor`, `holdOffRestream`, `turnRun.restreamsSpent`;
  `finishAtFault`, `draftOutputSurvives`, `draftOutputNoteFormat`; the compaction summary's
  counter. Each was its own plan item under `docs/plans/2026-09-20 - 04`.
- `docs/manual/configuration.md` states both keys and the combined worst case; `CONTEXT.md`
  names the budget in *Turns and stepping* and the faulted retention in the P6 paragraph. ADR
  0013, ADR 0039 and ADR 0046 carry dated notes pointing here; the `[0.18.0]` and `[0.14.0]`
  `CHANGELOG.md` records are superseded as history.
- A `continue` after a fault composes over ONE fold: a child that faults again is retained anew
  under the same name, over the original task, never a fold of a fold — the cap path's rule,
  unchanged.
- Bead `apogee-60x` closes on this record.
