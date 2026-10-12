---
title: No unconfined-Auto warning on a daemon run
status: done
type: feature
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.3]
created: 2026-09-03T18:41:25Z
closed: 2026-09-04T10:52:56Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 12 (the daemon warns on unconfined Auto, latched per process)
---

- [ ] **No unconfined-Auto warning on a daemon run.** `announceConfinement`
  (`cmd/apogee/wire_boot.go:278`) warns once per session when Auto runs with
  `confine-to-workspace: false` — ADR 0012's one blanket loosen. It is reached only from `runRoot`
  (`cmd/apogee/wire.go:120`), so a **daemon** Firing takes that posture unannounced.
