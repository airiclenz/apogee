---
title: No Windows label pre-warm on headless or daemon runs
status: done
type: feature
priority: 2
labels: [driver-parity, residue, windows]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.4]
created: 2026-09-03T18:41:26Z
closed: 2026-09-04T10:52:57Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, items 5 and 12 (headless and the daemon pre-warm the Windows label walk)
---

- [ ] **No Windows label pre-warm on headless or daemon runs.** The same function
  (`cmd/apogee/wire_boot.go:315`) runs `platform.PrewarmLabelWalk` on the TUI boot path only, so
  on Windows the first confined write in a **headless or daemon** run pays the label walk mid-run
  instead of at startup.
