---
title: "ADR 0071 decision 2: The ADR 0009 A/B gate binds lab rows only"
---

**ADR 0009's per-Mechanism non-inferiority gate, and catalogue rule
D1 with it, bind catalogued Mechanisms — the lab rows. They do not bind Floor guards.** This is the
one thing this record supersedes in ADR 0009, and it supersedes nothing else: the statistical
engine (measured δ, task-blocked units, one-sided non-inferiority, the burden on the Mechanism)
stands unchanged for everything still in the catalogue.

The aggregate constraint is untouched and is what actually protects the floor: the hard invariant
is proved as "full default-ON set vs Bypass, never worse" (ADR 0006), and Bypass now runs *with*
every Floor guard, so a guard that regressed the floor would show up in the control arm itself.
Clause (c) of the Floor-guard test is that constraint restated as an admission criterion.
