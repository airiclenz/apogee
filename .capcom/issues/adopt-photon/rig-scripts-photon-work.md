---
title: graphics/demo rig scripts read PHOTON_WORK
status: done
type: task
priority: 3
plan: 2026-10-17 - 01 - photon-adoption-prep-plan.md
created: 2026-10-17T10:34:01Z
closed: 2026-10-17T11:20:24Z
reason: "Resolved by photon-adoption-prep-plan (2026-10-17 - 01), archived at 211384d7, item 4: setup.sh/reset.sh resolve APOGEE_DEMO_WORK, else PHOTON_WORK, else default."
---

photon hands its commands the work dir as `PHOTON_WORK`; `graphics/demo/{setup,reset}.sh` read
`APOGEE_DEMO_WORK`. They must read `PHOTON_WORK` — and keep working under `cmd/demorig` until it
is retired (ADR 0101: no regression in between), so `APOGEE_DEMO_WORK` keeps precedence until
`film-hero-retire-demorig` drops it.

## Acceptance

- Both scripts resolve `APOGEE_DEMO_WORK` (demorig always sets it), else `PHOTON_WORK`, else
  `~/.cache/apogee-demo`; `photon.yaml` runs them with `APOGEE_DEMO_WORK` unset.
- `demorig record` still runs a take unchanged.
