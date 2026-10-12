---
title: ADR 0079 decision
---

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
