---
title: Glossary section mapping the retired apogee-sim vocabulary forward to apogee's terms
aliases: [retired-terms]
---

These were canonical in `apogee-sim/CONTEXT.md` and are **deliberately dropped** because
they name a structure that no longer exists. Recorded here so a reader who knows the old
vocabulary can map forward:

- **Apogee Core** → there is no standalone transform engine + public facade; the loop
  is the source of behaviour.
- **Integration** / **Apogee Proxy** / **OpenCode Plugin** → retired; Apogee is a single
  integrated tool, not a Core exposed through peer integrations.
- **Coding tool** (the external client sense) → Apogee *is* the coding tool now; there is
  no external client to name.
- **OpenAI HTTP surface** / **Chat-completion shape** (as a public contract) → Apogee no
  longer promises a wire contract to external clients. (The provider still *speaks* the
  OpenAI chat schema to the Upstream, but that is an internal client concern, not a
  contract Apogee exposes.)
- **Transform** / **Response Analyzer** / **Pre-pipeline Injector** (as the three
  *kinds* of Mechanism) → retired as the taxonomy, with **Mechanism** itself; a
  Reaction is classified by its origin × class cell and the Moments
  it fires on. The distinctions that still matter survive as attributes (the **Outcome** a seam
  Reaction returns; `Outcome.Defer` vs a hint derived fresh at `pre-request`).

Four more were canonical here and retired by
ADR 0076, which folds
Floor guards, the Mechanism lab layer and Hooks into one **Reaction** core over **Moments** with an
origin × class policy matrix: **Mechanism** → Reaction (the lab layer
is deleted; the bench arms an engine-origin Reaction through the facade); **Hook point** → seam
Moment; **Hook event** → notice Moment (the
eleven post-hoc spellings, with the old waiting spelling renamed `approval-requested` and the
five seam-closing notices added in stage 2 — eleven then; `session-started` (ADR 0076 amendment
2026-10-19) and `compaction-finished` (adr-0076-amend-1021b) have since made it thirteen);
**Experimental hook** → bench-armed Reaction.
**Hook** is not retired but demoted to the colloquial alias for a user-origin Reaction, and **Bypass** now reads
"advise and shape Reactions off" rather than "Mechanisms off".

Two more retired with the **validated-set surface itself**, deleted in stage 2 of ADR 0076 on
ADR 0016's own
2026-09-08 amendment, which supersedes that ADR in full:

- **Validated set** → gone, together with the `validated-sets:` key, `internal/validated`, the
  shipped roster, both `/settings` rows and the manual section; the config migration strips the key
  out of a saved file. It named a **per-model** enable set of catalogued Mechanisms that had passed
  the aggregate non-inferiority gate against Bypass *on that model*
  (ADR 0009) — proven *safe* there, benefit deliberately
  not part of the claim — keyed on the confidence-tagged `ModelFingerprint`, resolved
  best-available, and applied whole or not at all. ADR 0071 retired the roster it selected from, so
  from **v0.20.0** the surface loaded, validated and armed nothing. The reasoning kept: curation is
  per model, and a fingerprint is how you key it — a per-model **Reaction** roster, if one is ever
  wanted, belongs on the **Model profile** axis rather than in a parallel mechanism of its own.
- **Curation** → gone with it. It named the operator decision layer above the evidence stream: what
  the global catalogue contained (membership, port verdicts, global defaults) and what each model's
  Validated set contained, held strictly separate from evidence — a completed **Campaign** appends
  a ledger entry only, and a curation action was a distinct, later decision citing those entries.
  With no catalogue and no per-model set there is nothing left to curate; the evidence discipline
  itself is untouched.

The rest were canonical in **this** glossary and retired with the mechanism wave of
ADR 0071,
which promoted six catalogue rows to Floor guards (a seventh, `tool-call-salvage`,
was admitted later by its 2026-09-07 amendment and was never a catalogue row) and retired the other
fourteen in **v0.20.0** on ratified verdicts rather than on ADR 0016's older "inert by construction"
precondition. Their source, tests and assets are deleted, and so is the roll that outlived them
(`internal/mechanisms`, deleted with the `mechanisms:` key itself — ADR 0076 A6); what survives is
the config migration's successor table (`internal/config/configmigrate.go`), so an old
`mechanisms:` block is stripped out of the file with a note naming each promoted row's Floor key
rather than earning an unknown-id failure, and the archived
[catalogue](../../../docs/design/archived/mechanism-catalogue.md) records the per-row verdict.

- **Off-ramp** (Exempt Mechanism) → retired as a shipped concept: a Mechanism exempt from Adaptive
  Suppression and the Turn Budget — both retired with it — because suppressing it would leave the
  model with no way out of a failed Turn. The two rows that carried the Capability —
  `empty_response_recovery` and `tool_use_enforcer` — are Floor guards now, on in every arm, so
  there is no D1 default-off rule left for them to be the exception to. The `off-ramp`
  **Capability value** went with the descriptor when the lab layer was deleted.
- **Library** (the cross-session, per-model **learning store** that observed completed Turns and
  injected qualifying observations through a pre-request Mechanism) → retired with the `library`
  Mechanism: nothing observes Turns or injects learned text any more, and `~/.apogee/library/` on
  disk is never touched. `internal/library` itself went too (2026-09-16), once its fingerprint
  resolver lost its last reader: the confidence-tagged `ModelFingerprint` lives in `internal/domain`
  and the persisted probe record beside its writer in `internal/probe`, serving `probe model`'s
  drift check alone — a **Model profile** and its aliases are keyed on the advertised label string,
  never on a resolved identity. The **Failure library** was always the bench's own term and is
  unaffected.
- **History truncation** (`truncate_history`) → retired unshipped: the cheap alternative to
  Compaction, mechanically dropping the middle of the conversation and keeping the last N
  exchanges. Compaction is the conversation-level reducer, with
  Pruning as the cheap structural pass beneath it.
- **Guided decomposition** (`guided_decomposition`) and **`decompose`** → retired. The first steered
  the model's own primary call to enumerate remaining subtasks under measured Budget pressure and
  converted that enumeration into paced `sub_agent` delegations; the second was a prompt-shaping
  nudge that steered wording, not delegation. A model still delegates through the `sub_agent` tool
  on its own initiative — nothing prompts it to.
  ADR 0014
  stands as history and binds nothing shipped. A **Workflow**
  (ADR 0087) is not
  its return: the model (one `fan_out` call) or a human (a **Recipe**) asks for the work, and the
  engine plans nothing.
- The rest of the nudge catalogue — `stall_nudge`, `list_nudge`, `tool_use_directive`, `filehint`,
  `read_loop`, `read_repeat`, `toolfilter`, `error_enrichment`, `syntax`, `autofix` → retired
  unshipped, their catalogue verdicts never having left `pending`. Two names survive elsewhere
  and are not these rows: the always-on **syntax trailer** the four writing tools append
  (`internal/tools/syntaxtrailer.go`) is unrelated to the `syntax` Mechanism, and the
  read cache is `cached_content_intercept` promoted, not `read_repeat`, whose twin
  it was.
