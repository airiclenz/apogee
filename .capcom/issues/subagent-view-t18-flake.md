---
title: TestE2ESubAgentView t18 golden fails under full-suite load
status: open
type: bug
priority: 2
labels: [slow-box]
plan: 2026-11-10 - 00 - open-bug-fixes-plan
created: 2026-11-10T06:21:29Z
---

`TestE2ESubAgentView` (`cmd/apogee/e2e_subagent_view_test.go`) failed once during `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check` on the Pi 5 (2026-11-09). The failure was in the collapsed-row checks (~L193) and the `t18-run-view-finished` golden (~L215). It then passed 4 of 4 times on its own. This is not the t17 case that `subagent-view-golden-early` fixed.

Suspect, not confirmed: before it reads the frame, each of those scenes waits with `drv.WaitQuiet(settled)` (L186, L209). `docs/design/test-drivers.md` §Waiting rules out `WaitQuiet` as a readiness signal, so under load the frame may be captured before the last row has painted.

## Why

A slow machine makes apogee slower, never broken. The same holds for its tests: a flake under load fails `make check` for no real fault.

## Acceptance

- Each scene waits on the condition its assertions read, not on quiet
- The test passes 20 times under -race pinned to one core (`taskset -c 0`)
