---
title: e2e picker keys and resumed openLastRun typed after a quiet screen
status: done
type: bug
priority: 3
plan: 2026-10-13 - 01 - test-readiness-and-model-size-plan
created: 2026-10-13T08:58:27Z
closed: 2026-10-13T15:53:56Z
reason: "Resolved by plan '2026-10-13 - 01 - test-readiness-and-model-size-plan' items 1-2 (archived at 3ce94203): /fork and /sessions picker keys gate on the painted title/hint and moved cursor; openLastRun waits on waitIdle and the run view's crumb"
---

Three e2e waits still take a quiet screen as proof of readiness before typing a key, left outside the scope of plan '2026-10-13 - 00 - open-bugs-sweep-plan' item 3 (which converted the sites it named):

- `cmd/apogee/e2e_fork_test.go:84` — `drv.WaitQuiet(settled)` before the `/fork` picker's `Down` (:85).
- `cmd/apogee/e2e_background_clear_test.go:86` — `drv.WaitQuiet(settled)` before the `/sessions` picker's `Down` (:87).
- `cmd/apogee/e2e_toolsfold_subagent_test.go:181` — the resumed half's `next.WaitQuiet(settled)` before `openLastRun(next)` (:182) in `TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume`.

Each key is typed after a quiet screen rather than after a signal that the picker (or resumed transcript) is ready for it — the same readiness-proxy class as `waitquiet-readiness-proxy`. `docs/design/test-drivers.md` §Waiting: `WaitQuiet` only settles a frame about to be captured, never stands in for readiness.

## Acceptance

- Each of the three sites waits on a concrete readiness signal (a picker row or cursor paint, the resumed record or prompt) before its key, and no `WaitQuiet` precedes the key as its only gate.
