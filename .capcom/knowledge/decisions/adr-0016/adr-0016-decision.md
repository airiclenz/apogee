---
title: ADR 0016 decision
---

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
first Validated set: the pruned 15 (base minus `truncate_history`; 16 as recorded, minus
`grammar`, retired 2026-08-29 under the amendment below), on the Probe
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
