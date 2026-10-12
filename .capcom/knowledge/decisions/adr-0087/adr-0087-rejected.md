---
title: ADR 0087 considered options
---

- **Extend `sub_agent` with optional `over` / `returns` / `then` fields.** One tool, but two result
  shapes under one name, and a small model sees ten optional fields it may fill wrongly. Rejected
  for D2.
- **A per-workspace workflow folder** (`~/.apogee/runs/<workspace>/`). It survives sessions and
  enables incremental re-runs, at the cost of a new writable location and a pruning policy. Deferred;
  D4 can move later without touching the rest.
- **Retiring the fan-out ceiling outright.** Plain helpers would queue in waves too, and an eager
  model could again flood its context with dozens of full reports. Rejected for D7.
