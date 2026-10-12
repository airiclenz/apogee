---
title: headless registers neither --server nor --bypass
status: done
type: feature
priority: 1
labels: [bench, driver-parity, residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-kk0.8]
created: 2026-09-03T18:41:27Z
closed: 2026-09-04T10:52:58Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 6 (headless registers --server and --bypass)
---

- [ ] **`headless` registers neither `--server` nor `--bypass`.** Both are root-command flags
  (`cmd/apogee/root.go:105` and `:117`); `headless` registers `--endpoint`, `--model`, `--mode`,
  `--workspace`, `--config` and `--no-save` only (`cmd/apogee/headless.go:190-200`). So a
  **headless** run reaches a `servers:` entry through `APOGEE_SERVER` alone — the fallback
  `root.go:107-108`'s comment already names — and cannot run Bypass at all.
