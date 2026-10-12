---
title: "ADR 0071 decision 5: Six config keys and a zero-value-on `FloorConfig`"
---

**Each Floor guard gets exactly one top-level, file-only boolean config key, defaulting to `true`:**
`tool-use-enforcer`, `empty-response-recovery`, `tool-call-repair`, `tool-loop-breaker`,
`tool-result-cap`, `read-cache`. File-only: no flag, no `/settings` toggle. An operator debugging
their own upstream can still get a genuinely silent floor — ADR 0070 rejected a lock for that
reason and this keeps the escape hatch — but turning one off is a deliberate edit, not a posture
anyone reaches by accident.

**`domain.FloorConfig` holds six `Disable…` booleans, so its zero value keeps every guard on.** A
library embedder handing `New` a bare `Config` gets the whole floor, which is exactly what ADR
0070's empty-`EnableMechanisms` engine floor was for — the same guarantee, now structural rather
than reconstructed from a Capability column, and the ADR 0031
benchable-all-the-way-up door held open the same way.

**An old `mechanisms:` entry naming a promoted ID earns a notice naming its new key; there is no
silent mapping.** `tool_use_enforcer: false` in a config written against v0.20.0 does not quietly
become `tool-use-enforcer: false` — the key moved from a per-Mechanism map to a top-level
structural setting, and a user who wants the guard off should say so in the new place.
ADR 0015's "`EnableMechanisms` is
the one enable path" is amended accordingly: it remains the one enable path for **catalogued
Mechanisms**, and Floor guards are simply not enabled through it — they are on, and the six keys
disable them. ADR 0014
is amended by removal: `guided_decomposition` retires under Decision 3, so the record stands as
history and its `Requires`/batch-pacing contract binds nothing shipped.
