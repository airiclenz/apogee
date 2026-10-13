---
title: e2e tests use WaitQuiet(settled) as a readiness signal
status: open
type: bug
priority: 2
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:10:59Z
---

216 sites; a quiet screen stood in for 'pane armed' or 'turn folded' — the root of the 7-commit pane-arming saga — and costs >=54 s per run. Review §4, class B.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.

## Log

- 2026-10-13 Airic Lenz: Plan '2026-10-12 - 02 - test-suite-stability-plan' items 24-26 (archived at c6b5e859) converted the pre-Quit, armed-pane and backend-state-read WaitQuiet sites in cmd/apogee; still open: toolsFoldDelegatedRun's trailing WaitQuiet before the umbrella-fold resume test's Quit, the fork test's parentAndChild record read after 'forked from' with no record wait, and the reaction-gate/stopAtTheConfirm keys typed after a WaitQuiet with no arm wait.
