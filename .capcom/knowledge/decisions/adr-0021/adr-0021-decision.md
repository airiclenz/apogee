---
title: ADR 0021 decision
---

**1. Q1 — `probe` is a parent command that reports the host, plus two children.** The shape is:

| invocation | what it does | cost |
|---|---|---|
| `apogee probe` | the **host report** (the parent's own `RunE`) | free, offline, read-only |
| `apogee probe host` | the same host report, as a named child for scripts | free, offline, read-only |
| `apogee probe model` | the **capability battery** + behavioral fingerprint | live model calls **and a disk write** |

The asymmetry is the whole point: the host half is a description of the machine the user is
already on — no endpoint required, nothing executed, nothing written — so it is what the bare
noun means and it must never make the user pay to ask. The model half spends real tokens on a
live Upstream *and* records identity that changes later sessions' behaviour (§3), so it is an
**explicit act, never a side effect of typing `probe`**. `probe host` exists so a script never
has to rely on a bare parent's semantics staying put.

The host report is ADR 0012's
world, *reported and never re-decided*: OS/arch, the Confiner backend name, its capability
matrix, the `AutoEligible()` verdict, the effective `confine-to-workspace` after the host
acknowledgement is resolved, the workspace root and config home, endpoint reachability and the
`/v1/models` + llama.cpp `/props` discovery outcome. It runs **no agent and no tool**. Because
`/confine status` (TUI) and `apogee probe` (CLI) answer the same question, the selection and
notice logic is **extracted and shared, never duplicated** — two renderings of one verdict
cannot drift.

`probe` adds subcommands and nothing else: bare `apogee` stays byte-identical (the Phase-5
settled design), `maybeDispatchConfinedExec` stays the first thing `main` does, and the probe's
logic lives under `internal/` depending on `internal/domain` downward only
(ADR 0010), with `cmd/apogee` doing
the wiring — the same product-wire-time placement ADR 0016's realisation gave the validated-set
decision.

**2. Q3 — adaptive prompt complexity is NOT built here.** `probe model` reports a
**capability tier** as a field and that is the entire extent of it. A transform that strips tool
descriptions and shortens system prompts is, by definition, a **Mechanism** — model-facing,
gated, catalogued — and
ADR 0009 says a Mechanism earns its place on the non-inferiority
gate, per model, with a bench campaign behind it. Building it now yields one of two bad things:
a Mechanism shipped default-on without evidence (the hard constraint violated at the source), or
a catalogue row that is dead on arrival with a `TODO` where its Table B bench entry should be.
The tier ships as a *signal* — an ordinal summary of what the model can be **asked** to do,
derived from the battery outcomes, carrying **no automatism of its own** — and the transform is
recorded as a named `ISSUES.md` follow-on with the design intact. Validated, not assumed.

**3. Q2 — the model probe DOES persist, and persistence is the point.** `probe model` writes a
**versioned probe record** under the apogee home carrying the behavioral fingerprint at
`ConfidenceMedium`, and `library`'s resolver consults it as the middle rung of the
best-available ladder (**High** weights-hash → **Medium** stored probe record → **Low**
metadata label) exactly as `fingerprint.go:40` reserves.

> **Amendment (2026-09-16, plan `2026-09-16 - 01` item 7).** The middle rung had exactly one
> reader — the Validated-set match, which ADR 0076
> A9 deleted with `validated-sets:` — so `library`'s resolver, and with it `internal/library`, is
> gone and nothing resolves identity at startup any more; `ConfidenceMedium` survives as the tier
> the battery stamps on its claim, not as a rung anything climbs. Persistence stays, and its reason
> has narrowed to what this section already carried: the record is the stored behavioral signature
> the next `probe model` of the same `endpoint + label` compares against, and drift — "the model
> behind this label changed since &lt;date&gt;" — is what it buys. The record lives in
> `internal/probe`, beside the battery that writes it. The "identity is resolved by a pure offline
> function at startup" argument below described the reader that is gone; it is left as the
> reasoning of its day.

Print-only was considered and rejected on the evidence of the code: identity is resolved by a
pure offline function at startup, so a Medium tier that is never written down can never be
observed by anything, and `ConfidenceMedium` stays a constant no code path can produce — a
capability whose value never reaches users. That is the *precise* defect ADR 0016's 2026-07-19
amendment named when it refused offer-only semantics ("the evidence's value would never reach
users"); re-introducing it one ADR later would be a joke at our own expense.

The record's shape and posture:

- **Keyed on the triple `endpoint + advertised model label + probe timestamp`.** The endpoint
  because "the model at `:8080`" is the thing actually measured; the advertised label because
  that is what a later session has in hand offline; the timestamp because the record is a
  **dated claim**, not a permanent truth. A model swapped behind an unchanged label is thereby
  *detectable* rather than silent: the surfaces that use the record name the date it was taken,
  and a re-probe of the same `endpoint + label` that yields a different feature vector says so
  in as many words ("the model behind this label changed since <date>"). No expiry horizon is
  invented here — identity is not learning, so the Library's TTL posture does not transfer; if
  drift proves to be a real operational problem, an expiry is a purely additive follow-on.
- **Versioned, and soft on every defect.** A schema `Version` plus the battery version that
  produced the features (a record from an older battery is not comparable to a newer one). An
  unreadable, malformed, newer-than-this-build, or battery-stale record is **skipped with a
  one-line warning** and identity resolves as it does today (High if the weights are reachable,
  else Low). Never a blocked startup, never a crash — the same soft-degrade posture ADR 0016's
  realisation gives a defective Validated-set entry, for the same reason: this is a convenience
  layer above a safe floor, over data the user did not hand-write.
- **Owner-private on disk:** `0o700` directory, `0o600` file — the posture `internal/library`
  and `internal/session` already take for a private per-model record.

**4. Writing a Medium fingerprint switches Validated-set automatism ON for that model — say so,
out loud, at the moment it happens.** (Amended 2026-09-30: Validated sets are gone
(ADR 0076 A9), so a probe
record promotes nothing and nothing auto-applies. What stands of §4 is the posture: `probe model`
says it writes, `--no-save` writes nothing, and the printed path is the undo
(`cmd/apogee/probemodel.go`). `cmd/apogee/validatedsets.go` no longer exists.) This is the consequence that makes `probe model` an act
rather than a report. Under ADR 0016 §5 a model at Low confidence gets an **offer** (the
`offerNotice` paste-the-alias line, `cmd/apogee/validatedsets.go:104`); the same model at Medium
gets the set **auto-applied**. So **running `apogee probe model` is the act that promotes a
model from "offered" to "auto-applied"** for any Validated set matching its behavioral label.
Three requirements follow, and they are binding on the implementation:

- `probe model` **states this in its output before/while it writes** — naming the record path,
  and naming any Validated set that will now auto-apply as a consequence.
- **`--no-save` is the off-switch**: it runs the full battery and prints the full report
  (capability findings, suggested profile knobs, the behavioral label) and **writes nothing**.
  Deleting the record file is the equally supported undo, and the path is printed so it can be.
- The gate itself is **not** weakened. ADR 0016 §5's "automatism at ≥ medium" stands untouched;
  what makes this legitimate is that reaching Medium now requires a **deliberate human command**
  — the same shape as §3's alias, where the enabling act below medium is always an explicit
  human decision. Nothing auto-probes: no startup path, no TUI action, and no bench arm ever
  produces a Medium record as a side effect.

**5. Suggested profile knobs are PRINTED as paste-ready YAML — never written into the user's
config.** The battery's other output is a suggested `model-profile` block (`tool-call-format`,
`thinking.style`, …). It is emitted as a copy-paste-ready YAML fragment and `~/.apogee/
config.yaml` is **not touched**, following the `offerNotice` precedent verbatim
(`cmd/apogee/validatedsets.go:104-109`): the config file is the user's own document, a probe
produces *evidence*, and turning evidence into a preference is the user's move. This also keeps
the one write `probe model` performs down to a single, deletable, purpose-built file.

**6. The behavioral fingerprint is a fuzzy feature match, never a response hash.**
*(Amended 2026-07-22 — see the Amendment section below: the feature match is what the probe
record **records as evidence**, not how the fingerprint's label is **spelled**. The label stays
the advertised model label; the battery raises its confidence tier.)* Merge-plan
Phase 5 wording governs: the label is derived from *which capabilities the battery observed* —
native tool calls, structured/JSON output, a multi-step tool chain — with logprobs preferred
where the server exposes them, so that sampling noise, temperature, or a re-worded system prompt
do not produce a different identity for the same model. A hash over response text would be a
random-number generator wearing an identity's clothes.
