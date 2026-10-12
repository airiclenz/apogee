---
title: "internal/tui: t.Parallel sweep over the driver tests"
status: done
type: task
priority: 3
aliases: [apogee-11s]
created: 2026-09-13T20:53:50Z
closed: 2026-09-17T00:10:00Z
reason: t.Parallel sweep groups A–C (c10b1c27, d8b941d2, aaa49572) + tui seam guard (48f8e374)
---

Follow-up to apogee-ed5 (its cmd/apogee half shipped under docs/plans/2026-09-13 - 00 - headless-ci-hygiene-plan.md, item 7). Sweep internal/tui's driver tests the same way: t.Parallel() as the first statement of every test that does not itself t.Setenv/t.Chdir or swap a package-level seam, transitively through helpers. Leak attribution is already per-test (internal/tuitest/leak.go pprof labels). scripts/test-shards.sh and docs/manual/building.md still describe internal/tui as the serial package sharding bounds — rewrite them when the sweep lands.

## Log

- 2026-09-15 beads: triage of docs/reviews/architecture-review-2026-09-14.html candidate 17: the internal/tools half — 10 package vars swapped by tests (terminal.go, python_exec.go x2 incl. interpreterVersion, run_tests.go, diagnostics.go, git.go lookGit, exec_common.go shellHost, console_open.go openConsole, lookGo) keep ~13 tests serial; an execHost{run, LookPath, Host} injected via HostTools would let them run parallel. Fact-check: 'forbids t.Parallel' is false (598 calls in tools tests) — win is ~13 tests. Plan 2026-09-15 - 01 item 15 deletes the subprocessSpec mirror first; do the seam after.
