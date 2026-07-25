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

## Amendment (2026-07-25) — a Mechanism's descriptor and ordering are catalogue data, not instance methods

**Why now.** The Decision above says a Mechanism *"supplies a Mechanism descriptor"* and *"declares
its ordering constraints"*. The first implementation read both clauses as **methods on the Mechanism
value** — `Descriptor()` and `Ordering()` on a self-describing `domain.Mechanism` interface — and 21
catalogued Mechanisms later that reading had a measured cost. Every Mechanism repeated a seven-part
registration ritual (ID const · a two-map `init()` · struct · constructor · package-level descriptor
var · `Descriptor()` · `Ordering()`), and for the **14** whose constructor is the one-liner `return
xMechanism{}, nil` the ritual **was** the module. Two parallel maps — `catalogue` (ID → constructor)
and `descriptors` (ID → descriptor) — were hand-synced by convention, not constraint. The descriptor
existed **three times** (the package-level var, the map row, the method's return value), agreeing only
by discipline, with a test whose entire job was proving they still agreed. The clauses were right;
where the metadata lived was not.

**(a) Clauses (2) and (3) are supplied at registration, not through methods.** A Mechanism module
still declares all three things, still in its own file — but as **one `register(row{descriptor,
ordering, construct})` literal** rather than two methods plus two map writes. The registry stores
`domain.RegisteredMechanism{Descriptor, Ordering, Hook}`: `Descriptor` and `Ordering` are catalogue
**data**, `Hook` is the value carrying the behaviour (implementing at least one of the five hook
interfaces, which the registry type-asserts). The descriptor and the behaviour are joined **once**,
in `mechanisms.Build`, so a Mechanism and the row describing it cannot disagree — the drift the guard
test policed is now **unrepresentable** rather than test-guarded.

**(b) Everything this ADR is actually about is preserved, byte for byte.** The registry semantics do
not move: the topological sort over declared constraints, the **stable tiebreak by canonical
Mechanism ID**, the deterministic total order, the loud startup failure on a constraint cycle, and
the descriptor's incompatibility/requirement gates all behave exactly as before — every existing
ordering, gate and soft-degrade assertion passed unchanged across the change. So does the property
this ADR exists to protect: **adding a Mechanism touches the registry + the new module, not the
loop.** What changed is the **authoring** seam (one row literal instead of a seven-part ritual) and
the **storage** seam (one table instead of two hand-synced maps). A catalogue row may now also
declare the construction-injected `Deps` it needs, which removes the engine's one hardcoded
`library`-only special case from the otherwise-uniform build loop, and with it a Mechanism ID that
had been re-declared as a raw literal in a second package.

**(c) The alternative was not a preference, it was impossible.** The obvious smaller move — keep the
metadata on the instance and synthesize the two methods from an embedded helper — cannot be written
in Go. Embedding a type parameter is illegal, and a concrete wrapper adding `Descriptor()`/`Ordering()`
would have to declare **all five** hook interfaces, making every Mechanism claim every hook point and
destroying `Ordered`'s per-hook-point filter. The metadata therefore either stays on the instance
(with its duplication and its guard test) or leaves it entirely. There is no third shape.

**(d) The public break, accepted deliberately.** `apogee.Mechanism` is **removed** in favour of
`apogee.RegisteredMechanism`, and `MechanismRegistry.Add` / `.Ordered` change shape. The alias is
removed rather than silently redefined: a removed alias is a loud compile error, a redefined one
could mislead. At `v0.8.3` the only external consumer is the bench
([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)), and
[ADR 0015](0015-catalogued-mechanisms-are-enabled-by-id-through-config.md) §5's "stable v1 API"
language predates the 0.x version reset — see that ADR's 2026-07-25 realisation note. No shipped
behaviour changes; the enable surface (`Config.EnableMechanisms`, `CataloguedMechanisms()`, the
`mechanisms:` config block) is untouched, and experimental hooks (ADR 0002) keep their bare-value
registration.

Implementation lives in
[`docs/plans/2026-07-25 - 01 - mechanism-registration-collapse-plan.md`](../plans/2026-07-25%20-%2001%20-%20mechanism-registration-collapse-plan.md);
CONTEXT.md's **Mechanism descriptor** entry carries the prose.
