---
Status: accepted
---

# Curation is per-model: Validated sets keyed by the model fingerprint

## Context

Phase 4's bench evidence forced the question this ADR answers. On gemma-4-e4b-it-qat the
full 17-Mechanism stack failed the non-inferiority gate twice (the 20260706 aggregate,
replicated in-bundle by the 20260708 Screen); the Screen convicted no single Mechanism
("underpowered for diffuse harm"); the 20260714 Probe then showed the stack **minus
`truncate_history` is non-inferior** to Bypass (W+ = 102.0, p = 0.0003, N = 14) — but the
Probe is exploratory by construction (its hypothesis was data-suggested), so under L9 it
licensed a ledger claim only. No convicted set exists, so the Screen → Confirmation path is
closed, and the question "what evidence licenses which curation action?" was left open.

Meanwhile the evidence says models differ: the stack demonstrably hurts gemma, and the qwen
campaigns (once the engagement post-mortem landed) say nothing either way. New models arrive
constantly. Any curation rule that requires proven *benefit*, or that makes global claims
from single-model evidence, either throttles curation into a full-time job or claims more
than the data supports.

There is also a statistical trap worth recording: "the harm sits in `truncate_history`" was
never tested directly. Stack-with-TH failing the gate while stack-without-TH passes it is a
comparison of two verdicts, not a test of the difference (the Screen's direct removal
contrast gave p = 0.1104 — not significant). Any rule that needed the *attribution* claim
confirmed would need a new, adequately-powered head-to-head design; the suite's N = 14 tasks
already failed to power it once.

## Decision

**1. Curation is per-model.** The global catalogue stays what it is — the inventory of every
Mechanism in the binary, with port verdicts and global defaults. Single-model evidence never
deletes a catalogue row and never flips a global default (those remain cross-model
decisions). The per-model curation object is the **Validated set** (CONTEXT.md): an enable
set that passed the aggregate non-inferiority gate against Bypass *on that model*.

**2. Non-inferiority is the bar — deliberately.** A Validated set claims *safe on this
model*, not *helpful*. Rationale: the hard constraint is a safety floor, and a superiority
bar would make set curation unscalable as models multiply. Accepted consequence: a Validated
set may carry no proven benefit (ADR 0009's "pure cost" reading). ADR 0009's SELECTION test
(superiority) is untouched — it still governs any global default-ON ambition.

**3. The key is the confidence-tagged `ModelFingerprint`, exactly as the Library keys its
observations.** Evidence attaches to the precise model measured. Transfer to a sibling
quant, size, or family member is an explicit human/config alias, never automatic — a 4B
result must not silently claim a 12B.

**4. Qualifying evidence** is a completed, pre-registered aggregate-Protocol campaign on
that model that passes the non-inferiority gate **with engagement verified** (the qwen
lesson: a non-engaged campaign grades the seeded workspace and its verdict is void).
*Who* runs the campaign does not matter — operator bench runs and future user-run
set-validation tooling meet the same bar. This is what makes user-built sets for unknown
models possible without weakening the claim.

**5. Runtime semantics: auto-enable with a notice.** At ≥ medium fingerprint confidence, a
matching Validated set enables itself, with a visible per-session notice and a config
off-switch; below medium confidence nothing auto-enables (mirroring the Library's "prefer
not to inject under uncertainty"). A model with no Validated set runs the D1 floor
(structure + off-ramps). The shipped "default-ON set is non-inferior" guarantee thereby
becomes per-model, backed by exactly that model's campaign.

**6. First application — retroactive, and named openly.** gemma-4-e4b-it-qat receives the
first Validated set: the pruned 16 (base minus `truncate_history`), on the Probe
`gemma-4-e4b-it-qat-20260714-minus-truncate-history` (NI p = 0.0003, engagement
hand-verified in `runs.jsonl`), with the Screen's descriptive convergence as supporting
context. This rule was authored *after* that evidence existed. That is acceptable because
the entry rests on the set-level gate test — pre-registered, fresh δ, fresh runs — not on
the data-suggested TH-attribution hypothesis, which remains an exploratory ledger claim and
**needs no confirmation for curation purposes**.

Rejected alternatives: global `truncate_history` deletion on gemma evidence alone (models
differ; the sim's Conviction rule already says findings never transfer across models — TH
may yet validate elsewhere); a superiority bar for Validated sets (unscalable, and safety is
the constraint's posture); family/architecture keying (over-claims across sizes);
offer-only or record-only runtime semantics (the evidence's value would never reach users;
the Library sets the in-domain precedent for confidence-gated automatism).

## Consequences

- A new persistent surface exists to design and build: Validated-set storage (shipped
  entries for bench-validated models; user-local entries under `~/.apogee/` for user-run
  validations), the fingerprint match, the per-session notice event, and the config
  off-switch. Until that lands, Validated sets live as catalogue records + a config recipe.
- The analyzer **engagement guard is promoted from housekeeping to product prerequisite** —
  user-run validation cannot exist while engagement is checked by hand.
- The catalogue gains a curation record ("Validated sets") separate from the append-only
  evidence ledger; L9's "ledger entries only" discipline for campaign disposition stands —
  this ADR defines the *separate* curation step those entries license.
- The next campaign is freed from "confirm the TH claim": candidate purposes are now
  transfer tests on a second model, superiority hunting for a future Recommended tier, or
  nothing (rig work first).
