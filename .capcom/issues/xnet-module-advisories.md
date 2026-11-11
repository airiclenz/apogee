---
title: govulncheck lists x/net advisories GO-2026-6866 and GO-2026-6860 against the pinned golang.org/x/net v0.60.0
status: open
type: task
priority: 3
labels: [security]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-11T06:13:59Z
---

`govulncheck` (the `make check` step, Makefile:307) reports golang.org/x/net advisories GO-2026-6866 and GO-2026-6860 as module-level findings that apogee's code does not call, against `golang.org/x/net v0.60.0` (go.mod:33). Both are fixed in golang.org/x/net v0.62.0. Because no call path reaches them the gate passes, but the dependency still ships a vulnerable version. Observed during the audit-defects run's closeout (docs/plans/archived/2026-11-10 - 02 - audit-defects-plan.md); pre-existing, not introduced by it.

## Why

A not-called finding becomes a called one the moment new code reaches the affected package; bumping now keeps the gate from turning red mid-run.

## Acceptance

- go.mod requires golang.org/x/net v0.62.0 or later and govulncheck no longer lists GO-2026-6866 or GO-2026-6860
