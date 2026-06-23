# Handoff — continue the Apogee merge-plan grilling

**Date:** 2026-06-23
**From:** a `grill-me` session stress-testing the implementation plan
**Next session focus:** finish the grilling (4 open items) and propagate the resolved
decisions into the ADRs/CONTEXT.

---

## What this session did

Ran `grill-me` against the merge plan
[`docs/plans/2026-06-22-implementation-plan-apogee-merge.md`](../plans/2026-06-22-implementation-plan-apogee-merge.md).
Walked the design tree one question at a time and resolved **ten new decisions** (#12–#21),
all recorded in **§6 "Resolved in the 2026-06-23 grill-me session"** of that plan. The plan
body (header callout, §3a, §4 phases, §5 risks, §7) was updated to match.

**Do not re-derive these — read the plan §6 first.** Headline outcomes:
- **Bypass mode reinstated** as the honest "Mechanisms-off" floor (keeps exempt off-ramps;
  orthogonal `Config` flag; = the bench's aggregate control arm).
- Hard constraint reframed: **bench-time ground-truth gate** (provable) vs **proxy-only**
  production self-regulation.
- **Step/Turn/Exchange defined**; `Step()` returns at a **quiescent boundary**.
- **Forking is a bench concern, not an Apogee feature** (Apogee exposes only snapshot/resume
  + clean-library hygiene).
- **Tools stateless across Turns**; MCP/network are non-forkable external effects.
- **API co-dev rules** (`replace` for dev, v1.0.0 at end of Phase 3, additive events).
- **Confinement is a capability matrix** (Auto needs fs-write **and** network confinement;
  Linux Auto ⇒ kernel ≥6.7; MCP gates through Approval even in Auto).
- **Deterministic Mechanism ordering**; bench detects order-sensitivity.
- **Library keys on a confidence-tagged `ModelFingerprint`** (pure-Go GGUF hash → behavioral
  probe → metadata label); validated by a **longitudinal** experiment.

## Authoritative artifacts (reference, don't duplicate)

- Plan: `docs/plans/2026-06-22-implementation-plan-apogee-merge.md` (decisions in §6; open
  items + pending doc work in §8)
- Glossary: `CONTEXT.md`
- ADRs: `docs/adr/0001`…`0005`

## Grounding already done (so you don't repeat it)

- `apogee-code/src/tools/terminal-tool.ts` and `python-exec-tool.ts` are **one-shot/stateless**
  (fresh process per call; process-group kill; no persistent shell/REPL). Confirms the
  stateless-tool contract is a port, not a change.
- `apogee-sim/internal/library/` already has TTL (7d), `evictExcess()`, Bayesian
  confidence with counter-evidence, a min-confidence injection gate, and **curated-detector
  provenance** (injected content is templated/canned strings, not free text — injection
  vector largely closed). It keys on the **model-name string + `ModelPattern`** — that was
  the real gap, now addressed by the fingerprint decision (#21).

---

## Open items for the NEXT session (grill these)

From plan §8. In priority order:

1. **The A/B decision rule** *(the last load-bearing gap)* — the statistical bar for "earns
   its place": effect size, task count, what happens to an **inconclusive** A/B (drop vs
   default-off), multiple-comparison discipline across many Mechanisms, and the asymmetry
   between the **one-sided** "never worse" gate and the **two-sided** "is it better"
   question. This governs Phase 4's entire engine.
2. **Record/replay vs. disable-MCP/network-for-v1** in the bench (the §7 open choice;
   recommendation leaned record/replay but it was never picked).
3. **Lower-leverage, never grilled:** (a) Phase 1 has *no human interface* — UX discovery is
   deferred to Phase 2; is that ordering right? (b) `processing/` is the "riskiest port" but
   its parse-spec gate (`project-research`) is only *suggested*, not a Phase-3 prerequisite
   the way the hook-point mapping is for Phase 4. (c) In-process bench fragility — a `panic`
   in a Mechanism/tool can abort a long counterfactual sweep; add a recover-per-Step
   boundary, or accept?

## Then: apply the "Pending doc propagation" (plan §6)

The ten decisions are recorded in the plan but **not yet in the authoritative docs**:
- **`CONTEXT.md`:** reword hard constraint (#12); add **Bypass** entry (#13); per-tool
  confinement invariant (#19); update **Library** entry for fingerprint keying (#21).
- **ADR 0001:** split "what Apogee exposes" vs "what the bench composes" (#16); add co-dev /
  versioning consequences (#18).
- **ADR 0003:** deterministic ordering + bench order-sensitivity detection (#20).
- **ADR 0004:** rewrite around the capability matrix / kernel ≥6.7 / per-tool invariant (#19).
- **New ADRs:** Bypass mode (#13); Step/Turn/quiescent-boundary (#15); stateless-tool
  contract + non-forkable external effects (#17).

---

## Working style that worked this session

- One question at a time, each with a **recommended answer**; walk the decision tree.
- Ground claims in the actual repos before asserting (apogee-code, apogee-sim).
- Be willing to **reverse a recommendation** when reasoning shifts (e.g. off-ramps in
  Bypass) — the user values that over false consistency.

## Suggested skills

- **`grill-me`** — to grill the four open items above (primary task).
- **`grill-with-docs`** — if you'd rather grill *and* fold conclusions into CONTEXT/ADRs
  inline (covers the "Pending doc propagation" step too).
- **`coding-standards`** — mandatory once any code is written (Standing Requirement 1).
