---
title: ADR 0006 consequences
---

- The Mechanism descriptor's `Capability` field (`off-ramp` / `proactive-nudge` /
  `response-repair`) is what Bypass switches on: Bypass = "disable proactive-nudge +
  response-repair, keep off-ramp." The descriptor is the single source of truth for what
  Bypass turns off.
  (Amended 2026-09-30: no descriptor or `Capability` field exists any more; the Reaction's
  class decides, per ADR 0076 decision 9.)
- The Library must support an **inert** state distinct from "empty" — present but
  non-observing — so a Bypass run never pollutes the store.
  (Amended 2026-09-30: the Library retired with the Mechanism surface; there is no store to
  keep inert.)
- "Without Apogee" in the hard constraint is reworded throughout to mean **Bypass**, not the
  naked model (see `CONTEXT.md`).
