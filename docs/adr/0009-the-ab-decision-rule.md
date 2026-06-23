---
Status: accepted
---

# The A/B decision rule for Mechanism validation

## Context

Phase 4 keeps a Mechanism only if the bench says it earns its place. "Earns its place" was
never made precise, and the predecessor's practice (a two-sided Fisher test on pooled runs,
N≈40/arm, post-hoc power) was both underpowered and pseudo-replicated — a result that looked
significant but would not replicate. The hard constraint ("never worse than Bypass") must be
*proved*, not merely "not contradicted," and the catalogue is ~15–20 Mechanisms tested at
once, so multiplicity is real. This ADR fixes the statistical engine for the whole of Phase 4.

## Decision

**Two tests, two postures.** The decision rule is two distinct tests, not one:

- **The GATE — non-inferiority (one-sided, mandatory).** This *is* the hard constraint. A
  Mechanism ships only on **clear positive evidence it is not worse, down to the bench's
  measurement resolution δ**; the burden is on the Mechanism, so an inconclusive result
  **fails** the gate. (Rejected: treating a non-significant two-sided test as "no harm" — it
  cannot prove never-worse and leaks slow-bleed regressions at small N.)
- **The SELECTION — superiority (separate).** Does it actually help? This decides default-ON
  vs available-but-off; it does *not* govern whether the Mechanism may exist.

**δ is measured, not decreed.** δ is the bench **noise floor**, calibrated by an **A/A null
run** (two identical arms; δ = an upper quantile of the per-task null delta, e.g. 95th pct, or
k·SD). The A/A doubles as a rig self-test: a non-zero centre means the pairing is broken.
Calibrate at **production temperature**; re-calibrate per (suite × model × temperature).
(Rejected: a Bayesian `P(not worse) ≥ 0.95` posture — it needs a prior and drifts into "must
look better" near the boundary.)

**Unit of analysis = the task, blocked/paired on task.** N is the number of *distinct tasks*,
**not** tasks × runs — pooling runs into one 2×2 is pseudo-replication. Each task's per-arm
rate is estimated over R runs; the per-task ordinal-mean delta is the paired unit (Wilcoxon
signed-rank / paired-mixed across the T deltas). **Power comes from more distinct
*discriminating* tasks, not more reruns.** The suite is **frozen, Mechanism-agnostic, and
curated to the discriminating band** (where the model *sometimes* succeeds), pre-registered
once — per-Mechanism task hand-picking is bench-overfitting.

**Disposition — one confidence interval read against two lines (−δ and 0):**

| CI lower bound | verdict | disposition |
|---|---|---|
| `> 0` | superior | **default-ON** |
| `−δ < lower ≤ 0` | non-inferior, benefit unproven | **default-OFF, retained** |
| `≤ −δ` (or straddles −δ) | gate fails / inconclusive | **reject — cannot ship** |

Proven-neutral ⇒ **default-OFF**, because a Mechanism that is not-worse-but-not-better is pure
cost (latency, tokens, complexity, an ordering-graph node, an MC-budget slot) for zero measured
benefit. Retain it default-off with a **sunset rule** (retire after K suite/model refreshes of
persistent neutrality). **Off-ramps are the exception**: judged on their **firing
subpopulation** with a **recover-vs-dead-end** outcome (a full-suite average wrongly reads them
neutral), earning default-ON + exempt (see
[ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)).

**Asymmetric multiple-comparison discipline** (the dangerous error flips between the tests):

- **Selection → FDR (Benjamini–Hochberg, one-sided 0.05)** across the family. FDR controls the
  fluke-fraction of default-ONs; FWER/Bonferroni is too strict and kills the modest real wins
  small models need, and a selection false-positive is merely useless-not-harmful.
- **Gate → per-Mechanism, uncorrected, stricter one-sided 0.025.** A per-Mechanism *safety*
  claim — correcting it would make safety depend on batch size. Skipping FWER on the gate is
  safe via **three-layer defense**: a harmful Mechanism must also fluke FDR-controlled
  superiority to go ON *and* survive the aggregate Bypass floor.
- NI→superiority is a **closed/hierarchical** procedure: testing superiority on a gate-passer
  costs no extra α.
- Gate endpoint = **ordinal-mean only for v1**; the binary good-rate non-inferiority is held as
  an intersection-union **tightening** (no α penalty) if distribution-reshuffling pathologies
  appear.

**Aggregate vs per-Mechanism composition.** The **aggregate Bypass non-inferiority test is the
shipped guarantee**: full default-ON set vs Bypass, never-worse (+ ideally superior). The hard
constraint lives at this *system* level. Per-Mechanism **leave-one-out from the set** is
in-context attribution — it captures interactions and catches a harmful Mechanism **masked**
inside a net-positive set. The on-set is found by **greedy backward elimination** to a stable
set (linear in N per round, not 2^N), not by summing standalone A/Bs, because **the aggregate
is not the sum of the parts** (Mechanisms interact — tied to the order-sensitivity detection of
[ADR 0003](0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md)).

## Consequences

- The bench grows real statistical machinery it did not have: paired per-task analysis, A/A
  calibration, FDR across the family, a CI-based disposition. The predecessor's pooled-runs
  Fisher script is retired as the *decision rule* (it may survive as a quick eyeball).
- The frozen, pre-registered, discriminating suite is a deliverable in its own right — building
  it (and growing it until the A/A band is tight) is the main cost, replacing "just run more."
- Bench external-effect handling (deterministic stubs,
  [ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md)) is load-bearing *for
  this ADR*: live external flakiness would widen the A/A noise floor and pollute δ.
- This rule governs Phase 4 end-to-end; the Mechanism catalogue is whatever survives it.
