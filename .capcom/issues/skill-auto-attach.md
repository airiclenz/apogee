---
title: "Model-facing skill discovery: B1 auto-attach — deferred by ADR 0061"
status: parked
type: feature
priority: 4
labels: [bench]
aliases: [apogee-rms]
created: 2026-09-03T18:41:08Z
---

**Status:** deferred 2026-08-27 by
[ADR 0061](docs/adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md) decision 4,
and left deferred by
[ADR 0065](docs/adr/0065-shipped-skills-and-the-load-skill-door.md) §8, which superseded that
decision for **B2 alone**. **B1** attaches a matched skill's body to a message carrying no `/id`.
**Deferred because** it puts catalog-derived prompt text in front of the model that the user did not
ask for, which changes the model's input on apogee's own initiative — a **Mechanism** in the exact
sense of ADR 0003, so it enters as a **lab row** on the registry, gated per model and bound by the
Bypass never-worse floor. **Before it is built:** its own grill, an ADR **explicitly superseding ADR 0061 decision 4
for B1**, and a bench arm against Bypass. **The reusable half exists:** `skills.Catalog.Suggest`
(plan `docs/plans/archived/2026-08-27 - 01 - skill-suggestions-band-plan.md`) is engine-level and
model-free, so B1 would consume the matcher rather than grow a second one — as `Catalog.Lookup`,
B2's half, now does.
