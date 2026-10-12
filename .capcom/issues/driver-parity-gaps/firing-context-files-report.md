---
title: The session's context-files report reaches no Firing
status: done
type: feature
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.1]
created: 2026-09-03T18:41:24Z
closed: 2026-09-04T10:52:55Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, items 2-4 (one composer for the context-files notices; run.Result carries the report; headless prints it)
---

- [ ] **The session's context-files report reaches no Firing.** `noteContextFiles`
  (`internal/tui/model.go:742`) reads `Agent.ContextFilesReport()` and tells the human what the
  workspace context files contributed and what was dropped for budget. `run.Result`
  (`internal/run/run.go:67-111`) carries no such field, so a **headless or daemon** run never
  learns that a context file was skipped or truncated.
