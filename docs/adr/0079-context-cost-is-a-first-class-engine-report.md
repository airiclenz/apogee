---
Status: accepted
Amends: ADR 0062 call 13 (goldens for rendering surfaces only — second supersession, after ADR 0075 §14); ADR 0075 (the `v:2` `run_finished` frame gains additive keys)
---

# Context cost is a first-class engine report

## Context

apogee's hard invariant is that nothing it puts in front of a model may make that model perform
worse than the bare loop ([ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md)).
The bench measures the *quality* side of that invariant; the *cost* side — how many tokens apogee
itself spends before the user has said a word — has never been a number anyone could read.

What apogee injects at Turn 1 is spread across several producers: the rendered
[System prompt](../../CONTEXT.md#context-and-history) template, the Orientation block, the delegate
report block on delegations, the Task list block once the model has written one, the workspace
Context files, the Reactions' directives, and the tool menu — as native `tools` on a
tool-capable profile, or as the rendered tool-instruction block on one that is not
(`internal/agent/standingblocks.go`, `loop.go`'s `buildRequest` / `standingSystem` / `toolMenu`,
`wire.go`'s `toProviderRequest` / `toolInstructions`). The only figure the engine reports today is
`ContextFilesReport.StandingTokens` (`internal/domain/contextfile.go`), a chars/token estimate of
the whole standing string, surfaced to the host as an oversize warning and to headless as
`run_finished.context_files.standing_tokens`. It is one lump: it cannot say which piece grew, it
excludes the tool menu, and nothing ties it to the `prompt_tokens` the provider actually charges.

Three things follow from that gap. The bench cannot chart context cost per arm, so a Reaction
that earns its quality delta by spending a thousand standing tokens is indistinguishable from one
that spends ten. The tool-surface work (`docs/design/tool-surface-findings.md`) cannot price a
tool description. And growth in the injected bytes is silent: a sentence added to the Orientation
block or a tool description lands on every request of every session without any test noticing.

`apogee probe` already splits into a free host report and an explicit, paid model battery
([ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md));
the headless Event stream is a versioned protocol whose frames may grow additively under one `v`
([ADR 0075](0075-the-headless-event-stream-is-a-versioned-driver-protocol.md)); and the goldens
rule ([ADR 0062](0062-test-drivers-are-drivers.md) call 13, restated at
`docs/design/test-drivers.md` §Goldens and `internal/tuitest/golden.go`) confines goldens to
rendering surfaces, with ADR 0075 §14 as its one prior supersession for a documented wire format.

The owner ratified the calls below on 2026-09-17 (plan `docs/plans/2026-09-17 - 00`).

## Decision

1. **Context cost is a first-class engine report.** The engine produces one **Context cost**
   report: the tokens apogee itself puts in front of the model before the user's message — the
   standing system content plus the tool menu — as a **total** and a **per-piece breakdown**. The
   pieces are everything apogee injects: prompt, orientation, delegate report, task list, context
   files, tool menu, and the tool-instruction block on a non-native profile. The user's message
   is never part of it. It is `domain.ContextCost`, produced by `Agent.ContextCost()` and
   aliased on the facade as `apogee.ContextCost`; CONTEXT.md carries the term.

2. **Estimate and measured are two labelled sources, never blended.** The offline figure is the
   existing chars/token estimate (`context.TokenEstimator`, `domain.PromptChars`), always spelled
   with a `~`, produced idle after construction without a request. The measured figure is the
   provider's own Turn-1 `prompt_tokens` (and `cached_prompt_tokens` where reported), which
   exists only once a request has gone out. A surface says which it is showing; an estimate is
   never printed as if it were a measurement.

3. **The Reactions' directives count only when measured.** The pre-request cascade is not
   dry-run for an idle estimate: the offline report carries one column plus a note when advise or
   shape Reactions are armed, and the measured figure includes whatever the cascade actually
   placed on the wire. `apogee probe context --live` with Reactions armed sends twice — as
   configured, then under Bypass (ADR 0076) — and prints both measured columns so the Reactions'
   delta is read from two real requests. Probe gains no `--bypass` flag; Bypass stays a bench and
   session switch.

4. **Paid only on `--live` (ADR 0021).** `apogee probe context` is a free, offline host report:
   it composes the Agent under the resolved startup mode and estimates, and never Steps. `--live`
   is the explicit paid act: one fixed one-word Turn-1 request to the configured endpoint (two
   when Reactions are armed, per decision 3), stopped on the first Depth-0 usage event before any
   tool dispatch.

5. **Headless carries both, additively on `run_finished`.** Under `v:2` the `run_finished` frame
   gains `context_cost` (the estimate, taken idle after Agent construction, as a value beside
   `context_files` — never `omitempty`) and `turn1_prompt_tokens` / `turn1_cached_prompt_tokens`
   (measured, from the first Depth-0 usage event's per-call fields). `run_started` is untouched —
   it precedes Agent construction. Text mode gains one line, hidden when there are no rows. No new
   line kind: `eventjson.Kinds()` is unchanged, and the eventlines goldens redact `bytes` and
   `tokens` and assert `context_cost.tokens > 0` semantically.

6. **The tripwire pins bytes, not tokens.** A golden test pins the **bytes per piece** for a fixed
   fixture — a stub profile, a fixed workspace, `SetScratchDir(t.TempDir())` before rendering,
   workspace paths normalised — so that growth in what apogee injects is deliberate: it lands as a
   read diff under `-update`, not silently. Tokens are derived from those bytes by the estimator
   and are never pinned; an estimator change must not churn the golden.

   This is the **second supersession of ADR 0062 call 13**. That rule confines goldens to rendering
   surfaces — "a golden that pins behaviour fails on every unrelated wording change and is then
   updated without being read". ADR 0075 §14 superseded it once, for a documented wire format. This
   ADR supersedes it a second time, for the injected bytes: a wording change to a standing block is
   exactly the event the golden exists to make visible, and the reviewer reading the diff at
   `-update` is the mechanism. The warrant is as narrow as ADR 0075's: the bytes apogee itself
   injects, per piece, for one fixed fixture — not the rendered TUI notice, not a log line, not the
   estimate. `docs/design/test-drivers.md` §Goldens is not rewritten; this ADR is the record.

7. **The old field stays, derived.** `ContextFilesReport.StandingTokens` and
   `run_finished.context_files.standing_tokens` keep their meaning — the estimate of the whole
   standing string — and are derived from the same standing renders the new report reads.
   `ContextFilesReport()` is not rewritten.

## Considered and rejected

- **A tokenizer.** A real tokenizer per model would replace the `~`; it would also pin apogee to
  a vocabulary table per profile. The measured `prompt_tokens` is the truth and costs one request;
  the estimate is a labelled proxy. Out of scope.
- **Dry-running the Reactions cascade for the idle estimate.** The cascade reads a live Moment; a
  dry-run would need a fake one and would report a number no request ever carried. Measured-only.
- **Pinning tokens in the golden.** The estimator is a divisor; changing it would churn every
  golden without any injected byte having changed. Bytes are what apogee controls.
- **Putting the estimate on `run_started`.** The frame is emitted before the Agent exists; the
  report cannot be taken there. `run_finished` carries it beside `context_files`, which is already
  a post-construction fact.
- **A `--bypass` flag on probe.** Bypass is a session and bench switch; probe reads the
  configured session and, under `--live`, adds the Bypass column itself. One less flag to document.
- **A config budget ceiling or startup warning on the number.** The number must exist and be
  benched before a threshold means anything. A later plan.
- **TUI surfaces (`/usage`, the status line).** Same reason — the bench uses the number first.

## Consequences

- `internal/domain` gains `ContextCost`; `internal/agent` gains `Agent.ContextCost()` as a sibling
  producer over the `standingBlock` renders; the facade gains the `apogee.ContextCost` alias
  (`ContextCost` is not an Event, so `TestEveryDomainEventVariantIsAliased` is untouched).
- `internal/agent/testdata/contextcost.golden` is the bytes-per-piece golden; the eventlines goldens and the pinned
  `run_finished` line in `TestWriterEnvelopeOrderAndNulls` are refreshed.
- `apogee probe context` and `apogee probe context --live` ship; `docs/manual/probe.md` and
  `docs/manual/headless.md` describe the table and the frame keys.
- CONTEXT.md carries **Context cost** beside **Context files** and **System prompt**.
- Skill bodies and `@file` references stay outside the report: they are per-message, not Turn-1
  standing cost ([ADR 0061](0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md) D2).
