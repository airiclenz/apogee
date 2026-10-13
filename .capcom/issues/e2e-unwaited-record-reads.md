---
title: Two e2e tests read the session record after a quiet screen with no wait on it
status: open
type: bug
priority: 3
created: 2026-10-13T04:54:08Z
---

Two backend-state reads still follow a quiet screen with no wait on the session record. They are the class `waitquiet-readiness-proxy` covers, but the 6-line window that items 24 to 26 of the test-suite-stability plan swept did not reach them:

- `cmd/apogee/e2e_toolsfold_subagent_test.go:168`: `TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume` quits straight after the trailing `drv.WaitQuiet(settled)` of `toolsFoldDelegatedRun` (`cmd/apogee/e2e_toolsfold_subagent_test.go:56`), then relaunches with `--continue`. That wait guards a save but does not wait on the record, and it sits one helper-return away from the Quit.
- `cmd/apogee/e2e_fork_test.go:107`: in `TestE2EForkKeepsThePrefixAndSurvivesAResume`, the record read after `forked from` (`parentAndChild`) still has no wait on the record. It falls outside the 6-line window, so items 25 and 26 both missed it.
