---
title: ADR 0016 consequences
---

- A new persistent surface exists to design and build: Validated-set storage (shipped
  entries for bench-validated models; user-local entries under `~/.apogee/` for user-run
  validations), the fingerprint match, the per-session notice event, and the config
  off-switch. Until that lands, Validated sets live as catalogue records + a config recipe.
- The analyzer **engagement guard is promoted from housekeeping to product prerequisite** —
  user-run validation cannot exist while engagement is checked by hand.
- The catalogue gains a curation record ("Validated sets") separate from the append-only
  evidence ledger; L9's "ledger entries only" discipline for campaign disposition stands —
  this ADR defines the *separate* curation step those entries license.
- The next campaign is freed from "confirm the TH claim": candidate purposes are now
  transfer tests on a second model, superiority hunting for a future Recommended tier, or
  nothing (rig work first).
