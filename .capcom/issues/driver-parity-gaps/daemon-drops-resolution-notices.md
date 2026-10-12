---
title: The daemon discards the Firing's resolution notices
status: done
type: feature
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.2]
created: 2026-09-03T18:41:25Z
closed: 2026-09-04T10:52:55Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 11 (the daemon narrates its Firing through log.line)
---

- [ ] **The daemon discards the Firing's resolution notices.** `cmd/apogee/daemonfire.go:217`
  calls `cfg, _, err := firingConfig(` and drops the `[]string` of validated-set / profile /
  roster notices that `cmd/apogee/headless.go:371` keeps and prints. The **daemon** resolves the
  same config and says none of it.
