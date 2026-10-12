---
title: ADR 0024 consequences
---

- **The public Go surface gains `Agent.Rebind(RebindSpec)` and the `apogee.RebindSpec` alias** —
  additive (ADR 0010), so a **minor** bump. An embedder that never calls it is byte-identical to
  before, with one relaxation: `Config.Model` may now be empty at construction, and a `Submit`
  against an unbound Agent fails with `errNoModelBound` instead of `New` failing. An embedder who
  supplies a **pre-built** `Config.Mechanisms` registry cannot rebind (Rebind cannot rebuild hooks
  it did not build) and gets an honest error; the TUI wiring only ever sets `EnableMechanisms`, so
  it never meets that.
- **Startup no longer blocks or hard-fails on discovery** — the one behavioural change in this
  work. `apogee` against a dead endpoint now paints in well under a second and says `connecting…`,
  where it used to stall up to five seconds and, with `model:` unset, refuse to start at all.
- **`opts.contextWindow > 0` now means "pinned", exactly.** Nothing pre-fills it any more, which is
  what lets the rebind closure implement the pin rule with a single comparison.
- **A mid-Exchange model switch is narrated late.** The notice and the display move when the
  Exchange closes, not when the switch happens — the visible cost of never interrupting work.
- **`ctxUsed` survives a rebind unchanged.** The conversation is not reset by a model switch, so a
  fill measured against a larger window renders **clamped at 100%** against a smaller one until the
  next usage event or a compaction re-measures it. The gauge's percentage text now clamps with its
  bar, so this reads as "full", not as `137%`.
- **The gauge and the engine Budget still count different things** — the gauge uses the server's
  reported `TotalTokens`, the Budget a prompt-side estimate. That divergence is **documented and
  deliberately untouched** here; it long predates the heartbeat and is not a staleness bug.
- **llama.cpp's `/props` `n_ctx` is PER SLOT** — and `total_slots` was not read (**amended
  2026-08-08:** it is now, riding the same Beat as the discovery source for the **Parallel agents**
  cap,
  ADR 0039
  decision 2; the per-slot arithmetic below is unchanged, and is exactly why that cap and this
  window trade are documented as one thing). On a server started
  with `--parallel N` the runtime window a beat reports is one slot's share, which is the window a
  single conversation actually gets — the honest number for the Budget. The multi-slot arithmetic
  is recorded here rather than re-derived; if a server ever reports these two in a way that makes
  the per-slot value misleading, the `context-window:` pin is the answer until it is.
- **The picker work is now cheap and unblocked.** `TODO.md`'s "[P1] Server / model switching" loses
  its blocker: what remains is UI over `hb.models` plus, for a server switch, a new Monitor and
  client behind the same two seams — `Rebind` deliberately never touches `Endpoint`, and
  `errMissingEndpoint` stands.
- **`apogee probe` is unchanged and its output stays byte-identical**, which is the regression
  boundary between the diagnosis command and the monitor.
- **CONTEXT.md gains one term — *Heartbeat*** — worded to match this ADR and cross-referenced from
  *Probe*, so the two nouns cannot blur.
