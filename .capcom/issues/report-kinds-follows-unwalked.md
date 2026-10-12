---
title: TestReportKindsResolveDistinctly does not walk reportKind.follows
status: done
type: task
priority: 2
labels: [residue, tests]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-4w7]
created: 2026-09-03T18:41:29Z
closed: 2026-09-04T10:53:08Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 18 (TestReportKindsResolveDistinctly walks follows)
---

**Status:** recorded 2026-09-02 at the close of
`docs/plans/2026-09-02 - 05 - reasoning-field-spelling-plan.md`. The gap below is left open by
that plan's own delivered work — the report panes' follow (item 4). It is not a regression: the
kind-switch it names did not exist before this plan.

- [ ] **`TestReportKindsResolveDistinctly` does not walk `reportKind.follows`.** The walk over the
  declared kinds (`internal/tui/reportpane_test.go:44-70`) resolves each kind through
  `reportKind.pane`, `Model.reportState` and `Model.reportContent`, but not through the module's
  fourth kind-switch, `reportKind.follows` (`internal/tui/reportpane.go:132-141`). Its panicking
  default is therefore reached by no test, and a fourth report added later inherits the walk's guard
  for three of the four resolvers only — the follow answer it forgot to state would panic at first
  paint rather than fail the build's own guard test.
