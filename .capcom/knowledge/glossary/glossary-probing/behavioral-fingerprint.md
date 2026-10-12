---
title: "Behavioral fingerprint: the medium-confidence model identity a completed battery earns"
---

**Behavioral fingerprint**:
The model identity a completed **model battery** earns — the model's own advertised label, at
**medium** confidence. The battery raises an identity's *tier*; it never re-spells it (ADR 0021,
Amendment 2026-07-22), because that label is the key **Model profile** overrides and user aliases
are filed under, and a probe that renamed the model would orphan every one of them. It is the
*only* source of `ConfidenceMedium`: the `ModelFingerprint` tiers keep their names, but nothing
resolves identity at startup any more — the offline resolver that read the record as the middle
rung of a weights-hash → probe → label ladder had one reader, the Validated-set match, and went
with it (ADR 0076 A9;
ADR 0021 §3 amended 2026-09-16). The claim is kept **as a persisted probe record** (versioned,
owner-private, keyed on endpoint + advertised label + probe timestamp; any defect is skipped with
a warning), and what the record buys is **drift detection**: the next `probe model` of the same
endpoint + label compares its signature against the stored one and names the date the model
behind the label changed. Since Validated sets retired, the medium tier
switches **no automatism** on — the one it used to (a matching set auto-applying instead of being
offered) went with them; deleting the record (or `--no-save`) drops the comparison, nothing else.
What the battery observed is recorded beside the claim as the **behavioral signature** — a **fuzzy feature
match over battery outcomes** (which capabilities were observed; logprobs preferred where the Upstream exposes them), **never a
hash of response text**, so sampling noise or a re-worded prompt does not move it. The signature
is *evidence*, never a match key: comparing it across probes is what makes a swapped model behind
an unchanged label detectable.
_Avoid_: "response hash" / "output signature" (explicitly rejected — noise, not identity),
"model detection" (it identifies the model *behind* a label, it does not name it), calling the
signature a fingerprint (the signature is the evidence; the fingerprint is the identity).
