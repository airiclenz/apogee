---
title: Driver lane pre-check skips the engine's reserved-id and scope refusals
status: done
type: bug
priority: 2
plan: 2026-10-29 - 00 - p2-fixes-plan
created: 2026-10-29T08:33:23Z
closed: 2026-10-29T11:56:23Z
reason: "Resolved by plan 2026-10-29 - 00 - p2-fixes-plan (items 3-4), archived at 979a6d7d: Drivers pre-check a seed with agent.ValidateSeed; validatePendingGeneration and validateLanes removed."
---

The Drivers validate a seeded Generation before handing its observe list to the Runner with `validatePendingGeneration` (cmd/apogee/wire_engine.go) and `validateLanes` (internal/run/run.go) — two copies of `Generation.Validate` + `reactions.ValidateAll`. The engine's own arming check (`validateGeneration`, internal/agent/agent.go) also refuses a sync entry that takes a builtin's id (`refuseReservedIDs`: a Floor guard or the context-fill notice), and construction then scopes the sync lane (`scopeSync`); both are unexported, so the Drivers skip them. A `reactions:` sync entry named after a Floor guard therefore passes the Driver check, the Runner is handed the changed observe list, and only `New` refuses (`*SeedRefusedError`); `lateEngine.Bind` undoes the hand-over with `restoreRunner`, `run.Once` does not. The doc comments on seedGeneration and seedLanes promise that a list construction would refuse never reaches the Runner. Found 2026-10-27 (docs/plans/archived/2026-10-27 - 00 - session-briefing-and-construction-seed-plan.md item 6 NOTES).

## Acceptance

- One exported lane validator in internal/agent (the whole of `validateGeneration`, and the scope check if it can refuse) is the only check the Drivers run before `Runner.Replace`; `validatePendingGeneration` and `validateLanes` are gone.
- A test seeds a sync entry whose id is a Floor guard's and asserts the Runner's `Replace` is never called, for both `lateEngine.Bind` and `run.Once`.
