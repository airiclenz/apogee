---
title: A --no-save-only host never applies its retention policy
status: done
type: bug
priority: 2
labels: [residue]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-1ov.2]
created: 2026-09-03T18:41:28Z
closed: 2026-09-04T10:53:07Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 16 (--no-save still applies the retention policy)
---

- [ ] **A `--no-save`-only host never applies its retention policy.** `apogee headless --no-save`
  leaves `store` nil (`cmd/apogee/headless.go:447-450`) and `gcSessions` returns immediately on a
  nil store (`cmd/apogee/wire.go:501-503`), so a host that only ever runs `--no-save` headless —
  the case the sweep was placed here to cover (`cmd/apogee/headless.go:452-457`) — sweeps nothing,
  and `sessions.max-age` / `max-count` never bind. The sweep could run off `roots.sessions`
  directly rather than off the record-writing store.
