---
Status: accepted
---

# Mechanisms are a constraint-declared registry, not a fixed pipeline

## Context

apogee-sim runs its Transforms in a **fixed order enforced in code**
(`cot → library → codeinfo → filter → decompose → compress`). The merged Apogee must be
**modular** — adding a Mechanism (or promoting a bench experimental hook to one) should be
easy and should not require editing the loop, because the catalogue is evidence-driven and
expected to churn.

## Decision

A Mechanism is a **self-contained module** that (1) implements the interface for one
[Hook point](../../CONTEXT.md), (2) supplies a Mechanism descriptor, and (3) declares its
**ordering constraints** relative to other Mechanisms. The loop discovers Mechanisms from a
**registry** and orders them at each hook point from their declared constraints — it does
**not** hardcode a sequence. The result is a **deterministic total order**: a topological
sort of the declared constraints with a **stable tiebreak by canonical Mechanism ID**, never
Go's randomized map iteration — so a given Mechanism set always fires in the same order and
runs are reproducible. apogee-sim's existing `OrderingConstraints` is the seed of this; the
descriptor's incompatibility set governs stacking.

Adding a Mechanism touches the registry + the new module, not the loop.

## Consequences

- This modularity is **internal** extensibility. It does not contradict
  [ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md): the public
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
