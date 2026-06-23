---
Status: accepted
---

# Bypass mode is the honest "Mechanisms-off" floor

## Context

The hard constraint is that Apogee's Mechanisms must never make a model perform worse than
the same agent *without* them. To prove that, the bench needs a control arm — a
"without Mechanisms" baseline — and the constraint's wording needs an unambiguous referent
for "without Apogee."

The naive referent, *the naked model*, is wrong: a naked model with no Budget and no
Compaction simply overflows its context window on any non-trivial task and scores
catastrophically. Comparing against it would let almost anything look like an improvement —
the gate would pass trivially. The structural reducers (Budget, Compaction) are load-bearing
parts of the agent, not Mechanisms, and must stay on in the baseline.

apogee-sim already had a proxy-header bypass (`X-Apogee-Bypass`) that skipped the transform
pipeline; the merged Apogee has no proxy, so this must be re-expressed as a first-class
configuration of the loop.

## Decision

**Bypass mode is a `Config` flag, orthogonal to Agent mode**, that turns Apogee's Mechanisms
off while leaving the agent's structure intact:

- it disables the `proactive-nudge` and `response-repair` Mechanisms;
- it makes the **Library inert** — no inject, no observe, no write;
- it **keeps the exempt off-ramps** (e.g. `empty_response_recovery`), because a baseline that
  quit at the first stumble would pass the hard constraint trivially — the floor must be
  *functional*;
- Budget, Compaction, tool dispatch, and the rest of the loop **still run**.

Bypass is **orthogonal to Agent mode** (Plan / Ask-Before / Auto): any mode can run with or
without Bypass.

Bypass is the **same code path** users can run *and* the bench's **aggregate control arm**:
the hard-constraint non-inferiority gate (see [ADR 0009](0009-the-ab-decision-rule.md)) is
proved as "full default-ON Mechanism set vs Bypass, never worse." Because it is the literal
product floor — not a synthetic baseline — the guarantee transfers to what users actually
run.

Off-ramps keep their exempt-from-suppression status, but exempt-from-suppression is **not**
exempt-from-validation: each off-ramp still earns its place by its own leave-one-out A/B,
judged on the **subpopulation where it fires** (see ADR 0009).

## Consequences

- The Mechanism descriptor's `Capability` field (`off-ramp` / `proactive-nudge` /
  `response-repair`) is what Bypass switches on: Bypass = "disable proactive-nudge +
  response-repair, keep off-ramp." The descriptor is the single source of truth for what
  Bypass turns off.
- The Library must support an **inert** state distinct from "empty" — present but
  non-observing — so a Bypass run never pollutes the store.
- "Without Apogee" in the hard constraint is reworded throughout to mean **Bypass**, not the
  naked model (see `CONTEXT.md`).
