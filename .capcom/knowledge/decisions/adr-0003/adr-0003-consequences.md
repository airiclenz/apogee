---
title: ADR 0003 consequences
---

- This modularity is **internal** extensibility. It does not contradict
  ADR 0002: the public
  Mechanism *catalogue* is still curated and carries no third-party stability promise. Easy
  to add internally ≠ open public extension point.
- Ordering bugs move from "wrong hardcoded sequence" to "missing/contradictory constraint";
  a cycle in declared constraints must fail loudly at startup.
- **The bench detects order-sensitivity** among *undeclared* co-firing pairs: when two
  Mechanisms with no constraint between them produce different outcomes under swapped order,
  the bench surfaces the missing constraint (evidence-driven, not exhaustive
  pre-declaration). The stable tiebreak keeps runs reproducible *until* such a pair is found
  and a constraint is added.
- The detailed mapping of which Mechanism sits at which hook point (with constraints) is
  deferred to a dedicated session driven by sim data, as a prerequisite to Phase 4.
