---
title: Driver-parity gaps — behaviour only the TUI can reach (architecture review 2026-08-30)
status: done
type: epic
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0]
created: 2026-09-03T18:41:24Z
closed: 2026-09-04T10:53:06Z
reason: all remaining children delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md (items 2-12); the revert half, apogee-kk0.7, is detached and stays open
---

**Status:** recorded 2026-08-30 by the architecture review, parked at the close of plan
`docs/plans/2026-08-30 - 02 - tui-host-hoist-plan.md` (Group C: the review's parity findings that
are FEATURE work on a Driver, not a hoist of a shared rule). Each line is a capability the
interactive session has and a headless run, a daemon Firing, or both do not — with the TUI site
that owns it and the Driver that lacks it. None is a regression: every one has been absent since
the Driver was built. The hoists the same review proposed and this plan did NOT take (A4–A8,
B3–B5) are recorded by the report and by that plan's out-of-scope list, not here.


The full findings, with the candidates this plan took and the ones it left, are in
[`docs/reviews/architecture-review-2026-08-30.html`](docs/reviews/architecture-review-2026-08-30.html).
