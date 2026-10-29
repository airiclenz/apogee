---
title: ADR 0006 decision
---

**Bypass mode is a `Config` flag, orthogonal to Agent mode**, that turns Apogee's Mechanisms
off while leaving the agent's structure intact:

- it disables the `proactive-nudge` and `response-repair` Mechanisms;
- it makes the **Library inert** — no inject, no observe, no write;
- it **keeps the exempt off-ramps** (e.g. `empty_response_recovery`), because a baseline that
  quit at the first stumble would pass the hard constraint trivially — the floor must be
  *functional*;
- Budget, Compaction, tool dispatch, and the rest of the loop **still run**.

(Amended 2026-09-30: Mechanisms, the Library and the `Capability` field are gone. The off-ramps
became Floor guards (ADR 0071),
and Bypass now turns off every `advise` and `shape` Reaction of user or bench-armed origin, plus
the engine's context-fill notice; the seven Floor guards, `observe` and `gate` stay on
(ADR 0076 decision 9,
`internal/agent/reactions.go`). The floor principle and the control-arm role below stand.)

Bypass is **orthogonal to Agent mode** (Plan / Ask-Before / Auto at the time; amended 2026-10-29:
Allow-Edits has since joined them, `domain.ModeAllowEdits`): any mode can run with or
without Bypass.

Bypass is the **same code path** users can run *and* the bench's **aggregate control arm**:
the hard-constraint non-inferiority gate (see ADR 0009) is
proved as "full default-ON Mechanism set vs Bypass, never worse." Because it is the literal
product floor — not a synthetic baseline — the guarantee transfers to what users actually
run.

Off-ramps keep their exempt-from-suppression status, but exempt-from-suppression is **not**
exempt-from-validation: each off-ramp still earns its place by its own leave-one-out A/B,
judged on the **subpopulation where it fires** (see ADR 0009).
