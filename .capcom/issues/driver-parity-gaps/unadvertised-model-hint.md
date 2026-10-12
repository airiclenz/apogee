---
title: The "model not advertised" hint is TUI-only
status: done
type: feature
priority: 2
labels: [driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.6]
created: 2026-09-03T18:41:26Z
closed: 2026-09-04T10:52:58Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 10 (the 'model not advertised' hint reaches the unattended path)
---

- [ ] **The "model not advertised" hint is TUI-only.** `hintNotice`
  (`cmd/apogee/upstream.go:559`) says so when a session binds a model id the server never
  advertised; its one caller is the TUI rebind seam (`cmd/apogee/wire_verbs.go:58`,
  `rootWiring.rebind` → `tui.RebindResult`). A **headless or daemon** Firing binding the same id
  is told nothing.
