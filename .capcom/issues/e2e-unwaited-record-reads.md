---
title: Two e2e tests read the session record after a quiet screen with no wait on it
status: done
type: bug
priority: 3
plan: 2026-10-13 - 00 - open-bugs-sweep-plan
created: 2026-10-13T04:54:08Z
closed: 2026-10-13T09:16:00Z
reason: "Resolved by plan '2026-10-13 - 00 - open-bugs-sweep-plan' item 3 (archived at 77f00a79): the umbrella-fold resume test waits idle and on the recorded wrap-up before Quit, and the fork test waits for both session records before parentAndChild."
---

Two backend-state reads still follow a quiet screen with no wait on the session record. They are the class `waitquiet-readiness-proxy` covers, but the 6-line window that items 24 to 26 of the test-suite-stability plan swept did not reach them:

- `cmd/apogee/e2e_toolsfold_subagent_test.go:168`: `TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume` quits straight after the trailing `drv.WaitQuiet(settled)` of `toolsFoldDelegatedRun` (`cmd/apogee/e2e_toolsfold_subagent_test.go:56`), then relaunches with `--continue`. That wait guards a save but does not wait on the record, and it sits one helper-return away from the Quit.
- `cmd/apogee/e2e_fork_test.go:107`: in `TestE2EForkKeepsThePrefixAndSurvivesAResume`, the record read after `forked from` (`parentAndChild`) still has no wait on the record. It falls outside the 6-line window, so items 25 and 26 both missed it.
