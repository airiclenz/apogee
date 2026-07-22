---
Status: accepted
---

# `apogee probe` is two halves: the host report is free, the model battery is an explicit act

## Context

`apogee probe` was promised twice, from two directions, and neither promise has a command to
live in — the CLI has exactly one Cobra root and, until Phase 5 item 1, no subcommand seam at
all.

- **The host/confinement diagnosis.** `TODO.md`'s deferred residue from the
  auto-confinement-degradation work asks for "reporting the confinement backend and its
  capability matrix as a subcommand, diagnosable **without running an agent**". `/confine
  status` covers the need from inside the TUI, which is precisely the wrong place when the
  question is *why does Auto gate every command on this box* — the user wants an answer before
  they start a session, in a script, over SSH, in a CI log.
- **The model capability probe.** `../apogee-sim/mission.md` item 3: a probe command that sends
  test prompts (native tool call, JSON/structured output, multi-step tool chain) and
  **auto-generates a profile**, eliminating manual profile tuning. Item 2 of the same mission
  wants *adaptive prompt complexity* — a pipeline transform that strips tool descriptions,
  shortens system prompts, and simplifies formatting per `capabilityTier`.
- **The merge plan adds a third job to the second one:** the same battery yields the
  **behavioral model fingerprint** — a fuzzy feature match, **not** a response hash, logprobs
  preferred where the server exposes them — which is the identity used for Library keying (and
  for Validated-set keying) when the weights file is unreachable.

Those three jobs have wildly different costs and different blast radii, and one of them has a
consequence the other two do not:
[ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md) §5 auto-applies a
matching Validated set at **≥ medium** fingerprint confidence and merely *offers* it below that.
`domain.ConfidenceMedium` is documented as "a behavioral-probe identity (`apogee probe`, Phase
5)" (`internal/domain/fingerprint.go:20`) and **no resolver produces it**
(`internal/library/fingerprint.go:40`). The product wire-time resolution is a pure, synchronous,
offline call — `library.ResolveFingerprint(opts.model)` at `cmd/apogee/validatedsets.go:38`,
reached on every startup before anything is dialled. A tier that must be *earned by live model
calls* can therefore only reach that call site **through something written down**. That single
fact decides Q2 below.

## Decision

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

The host report is [ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)'s
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
([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)), with `cmd/apogee` doing
the wiring — the same product-wire-time placement ADR 0016's realisation gave the validated-set
decision.

**2. Q3 — adaptive prompt complexity is NOT built here.** `probe model` reports a
**capability tier** as a field and that is the entire extent of it. A transform that strips tool
descriptions and shortens system prompts is, by definition, a **Mechanism** — model-facing,
gated, catalogued — and
[ADR 0009](0009-the-ab-decision-rule.md) says a Mechanism earns its place on the non-inferiority
gate, per model, with a bench campaign behind it. Building it now yields one of two bad things:
a Mechanism shipped default-on without evidence (the hard constraint violated at the source), or
a catalogue row that is dead on arrival with a `TODO` where its Table B bench entry should be.
The tier ships as a *signal* — an ordinal summary of what the model can be **asked** to do,
derived from the battery outcomes, carrying **no automatism of its own** — and the transform is
recorded as a named `TODO.md` follow-on with the design intact. Validated, not assumed.

**3. Q2 — the model probe DOES persist, and persistence is the point.** `probe model` writes a
**versioned probe record** under the apogee home carrying the behavioral fingerprint at
`ConfidenceMedium`, and `library`'s resolver consults it as the middle rung of the
best-available ladder (**High** weights-hash → **Medium** stored probe record → **Low**
metadata label) exactly as `fingerprint.go:40` reserves.

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
out loud, at the moment it happens.** This is the consequence that makes `probe model` an act
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

**6. The behavioral fingerprint is a fuzzy feature match, never a response hash.** Merge-plan
Phase 5 wording governs: the label is derived from *which capabilities the battery observed* —
native tool calls, structured/JSON output, a multi-step tool chain — with logprobs preferred
where the server exposes them, so that sampling noise, temperature, or a re-worded system prompt
do not produce a different identity for the same model. A hash over response text would be a
random-number generator wearing an identity's clothes.

## Considered options

- **One `apogee probe` that reports the host always and probes the model whenever the endpoint
  happens to be reachable** (Q1's alternative). Rejected: it makes a token-spending, disk-writing,
  automatism-enabling act fire on the basis of *whether a port answered*. The costs are too
  asymmetric to hide behind one noun, and "it wrote a fingerprint because your server was up" is
  not a sentence this project wants to have to explain.
- **`probe` as a bare parent with no own `RunE` (help only), everything in children.** Rejected
  as a papercut: the overwhelmingly common question ("what is this box doing to Auto?") would
  cost a second word for no gain, and the free half is exactly the half that should be reflexive.
- **Print-only v1, persistence as a recorded follow-on** (Q2's alternative). Rejected on the code
  above — see §3.
- **Writing the suggested profile into `config.yaml`** (with a backup, or behind `--write`).
  Rejected: `configwrite.go` exists and could do it safely, but the offer precedent is the right
  one — an automatic profile edit changes how the engine speaks the wire on the next run, from a
  command the user ran to *look* at something.
- **Building adaptive prompt complexity now, default-off and catalogued** (Q3's alternative).
  Rejected for this plan: a catalogued Mechanism owes the catalogue a bench-validation entry
  (ADR 0009), and manufacturing a row we cannot yet fill trades a real design for a placeholder.
  It stays a `TODO.md` follow-on with its design recorded — and if it is ever built, it is
  default-off and bench-gated, never a rider on the probe.
- **A `probe --json` machine format in v1.** Deliberately not decided here; the report is
  human-first and a stable machine format is additive. Named so a later ADR is not surprised.

## Consequences

- **`ConfidenceMedium` stops being a reserved constant** and becomes a tier the system can
  actually be in. Both consumers gain a middle rung without changing shape: the Library's
  confidence-gated injection and the Validated-set match, keyed identically.
- **`apogee probe model` is a privileged-feeling command in a tool with no other privileged
  commands.** It is the only user-facing act that *enables automatism*. It must therefore read
  like one — explicit output, named record path, `--no-save`, and a printed undo.
- **A new persisted surface joins `~/.apogee/`** (config, library, sessions, validated) — small,
  versioned, owner-private, individually deletable, soft on every defect.
- **`TODO.md:370`'s probe residue closes**, and the confinement-degradation residue that pointed
  at it is updated (the edit itself belongs to the Phase-5 docs roll-up item, not here).
- **`CONTEXT.md` gains three terms** — *Probe*, *capability tier*, *behavioral fingerprint* —
  worded to match this ADR.
- **Adaptive prompt complexity is a named, dated `TODO.md` follow-on** carrying the tier's
  intended use, so mission.md item 2 is not lost — merely not assumed.
- **Bench arms are unaffected by construction.** A Medium record is produced only by a human
  running a command; the resolver's ladder is otherwise unchanged, and ADR 0016's
  wire-time-not-engine placement means a Bypass control arm still enables nothing.
- **`probe host` is the first CLI surface for the confinement matrix**, which makes ADR 0012's
  degradation story diagnosable off-session for the first time — including on the Windows hosts
  Phase 5's remaining items are about, where "which backend answered?" is the first question
  anyone will ask.
