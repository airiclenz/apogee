---
title: "headless: run_started.workspace and the firingConfig refusal exit are untested"
status: done
type: task
priority: 3
aliases: [apogee-bi9]
created: 2026-09-07T14:29:06Z
closed: 2026-09-07T17:22:49Z
reason: Closed by test-only additions; make check green
---

Deferred from plan 'docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md'. No test pins the `workspace` member of the `run_started` frame, and the firingConfig refusal exit path at cmd/apogee/headless.go:511 — one of ADR 0075's thirteen never-started refusals — has no test covering its run_finished frame.
