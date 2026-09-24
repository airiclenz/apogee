---
Status: accepted
Amends: ADR 0024 §6 (a configured `model` hint is trusted as configured, never replaced when the server stops listing it)
---

# Apogee measures Upstreams and passes routing through

## Context

On 2026-09-21 an apogee session running through OpenRouter was very slow, and apogee could not
say why. It knew token counts (`domain.UsageEvent`) and a TUI-only tokens/s, and nothing else:
no time to first byte, no time to first token, no per-request duration, no per-server fault
rate. Whether the cost was a slow provider, a starved queue or a ten-minute stall cut by
`stream-idle-timeout` ([ADR 0082](0082-a-silent-stream-is-cut-and-a-transient-fault-is-ridden-out-under-a-budget.md))
was a guess.

The owner asked whether apogee could route dynamically across providers by cost, speed and
reliability — with the hard constraint that apogee must not become an OpenRouter-tuned tool: a
local llama.cpp user, an OpenRouter user and a user behind any other gateway must all benefit
equally. The brainstorm (`docs/handoffs/2026-09-21 - 00 - provider-routing-speed-brainstorm.md`)
settled on four layers — an opaque body passthrough, per-server measurement, explicit fallback
pools, a dynamic scoring router — and the owner's 2026-09-23 grilling session ratified the
design of the first two, which `docs/plans/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md`
implements. This ADR records those calls, rejects the fourth layer, and names the third as future
work.

Two facts shaped it. Apogee has no OpenRouter-specific Go code, and keeps none: the only
OpenRouter awareness is documentation and the effort-dialect discovery that reads a
`reasoning.mandatory` flag. And a configured `model` id that the server does not advertise —
an OpenRouter variant slug such as `vendor/model:nitro` — has been used verbatim since
b6e51496 (`provider.HintResolution`: `exact`, `base-slug`, `trusted`), which made "sort by
throughput" a zero-code answer; that change landed with no ADR, and ADR 0024 §6 still says the
binding "follows observed reality" once the hint vanishes from `/v1/models`.

## Decision

**1. Apogee measures and passes routing hints through; it never routes.** Routing belongs to the
thing that is a router. Where a provider, gateway or vLLM front accepts routing hints in the
request body, apogee carries them opaquely and learns none of that provider's vocabulary. What
apogee adds of its own is measurement — neutral, useful to a local-server user as much as to a
hosted one, and the prerequisite for any smarter behaviour later.

**2. `request-extra:` is a per-entry, opaque body passthrough.** A `servers:` entry may carry a
`request-extra:` mapping. It **overlays** the encoded request body as an
[RFC 7396](https://www.rfc-editor.org/rfc/rfc7396) JSON Merge Patch: objects deep-merge, scalars
and arrays replace, `null` deletes. The keys apogee owns — `model`, `messages`, `stream`,
`stream_options`, `tools`, `system` — are **reserved** and refused at config load, even as
`null`. The overlay is applied in the entry's provider Client encode, so every body-carrying
request through that entry carries it: Turns, delegations and compaction alike, plus the
title-naming call and the `apogee probe` model battery's completion client. With no
`request-extra:` configured the body is byte-identical to today's. No provider name appears in
Go code for this; the manual carries the provider examples.

**3. A configured `model` hint is trusted as configured.** *(Amends ADR 0024 §6.)* A `servers:`
entry's `model` is the active model verbatim whenever it is set: an exact advertised match or a
base-slug match (the part before the first `:`) supplies only the context window, and an id the
server does not list runs as configured with the window unknown and a transcript notice. It is
never replaced by another advertised model when unlisted; only an empty `model` falls back to the
first advertised one. The rest of ADR 0024 §6 — `context-window:` is a pin the heartbeat never
overrides — stands.

**4. Every upstream HTTP attempt is measured in the provider Client.** One sample per attempt,
including the pre-first-byte retries: the attempt index, the request id when the server returns
one, and two clocks, both timed from send — **`ttfb`** to the first body byte (keepalive comments
count) and **`ttft`** to the first model delta of any kind (reasoning, content or tool call) —
plus `last`, to the last model delta, and the total duration. The outcome is a closed vocabulary:
`ok`, the fault class, or `cancelled`. **tok/s** is the reported output tokens ÷ (`last` −
`ttft`); with no reported usage there is no tok/s, never an estimate.

**5. The sample travels the event path, never a side channel.** The Client puts a `DeltaAttempt`
on the Delta stream it already returns; the loop turns it into a `domain.UpstreamAttemptEvent`
carrying the server identity; a Driver-side subscriber writes it where that Driver wants it. The
engine stays wire-silent and Driver-agnostic
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
the TUI records it, headless emits it as an `upstream_attempt` line
([ADR 0075](0075-the-headless-event-stream-is-a-versioned-driver-protocol.md)), and a bench can
count it. Attempts at every delegation depth and in compaction are counted.

**6. Server stats persist across sessions in `~/.apogee/server-stats.jsonl`.** One line is
appended (`O_APPEND`) per attempt, keyed by server name plus the redacted endpoint, and each line
records the model the attempt served. At startup a file past four times the cap is rewritten
through temp + rename, keeping the last 50 samples per (name, endpoint, model). A lost concurrent
append from two apogee processes is acceptable — the store is a summary source, not a ledger.

**7. Privacy is structural.** The endpoint is stored as scheme + host + path — userinfo, query
and fragment are stripped. No prompt text, no request or response body, no key names, no
`request-extra:` content is stored. A root `server-stats: off` stops the writes and the picker
summary; the events still fire, because other Drivers and the headless stream consume them.

**8. The summary is surfaced in the pickers, `/inspect` and headless — not the status bar.** The
`/server` and `/sub-agents-server` picker rows read
`name — endpoint · ttft 1.8s · 42 tok/s · 2/20 failed`, filtered to the model bound on that
entry, else the last recorded one, labelled. Under five carrying samples the row reads
`· no data`, and tok/s reads `— tok/s` under five samples that carry it. Cancelled attempts are
excluded from the percentiles and from the failure rate. At narrow widths the endpoint drops
first, then the summary truncates; the name never does. `/inspect` lists the individual attempts.
No status-bar change: a live TTFT is not part of this decision.

**9. Keepalive starvation stays out of the idle timer.** ADR 0082's idle-timer semantics are
unchanged — a keepalive comment line still counts as activity. A provider that keeps a stream
alive with keepalives while never producing a delta is exactly what the `ttfb`/`ttft` split now
makes visible; whether to cut it is a separate question, tracked as bead
`apogee-keepalive-starvation`. A per-server idle timeout is likewise out of scope
(`apogee-per-server-idle-timeout`).

## Considered and rejected

- **A dynamic cost/speed/reliability scoring router** (the brainstorm's Layer 4) — rejected, for
  three reasons. Scoring by cost needs pricing data, and pricing tables are provider-tuned: the
  per-provider tables are precisely the OpenRouter-tuned trap this ADR exists to avoid. The
  typical user runs one or two servers and has nothing to choose between. And above a provider
  that already scores and routes, apogee would double-route — two schedulers second-guessing each
  other over a sample apogee sees only a sliver of.
- **Typed, provider-specific routing fields** (`provider.sort`, `order`, …) — rejected: it is the
  vocabulary-learning Decision 1 refuses, and every gateway spells it differently. The opaque
  overlay serves them all.
- **`request-extra:` that only adds keys, never overrides** — rejected in favour of the Merge
  Patch overlay: a passthrough that cannot adjust a key apogee already sends (a sampling field, a
  `reasoning` block) forces a typed option for each one. The reserved keys fence off the ones
  whose override would break the protocol.
- **A side-channel observer on the Client** — rejected: it would hand the provider a Driver
  dependency and bypass the event stream every Driver already consumes.
- **Persisting stats per session** — rejected: a session is too short to carry a meaningful
  percentile, and the question a user asks at the picker ("which server is fast?") spans sessions.
- **Estimating tok/s from delta text** when the server reports no usage — rejected: a figure
  apogee cannot stand behind is worse than none.

## Future work — fallback pools

Explicit **fallback pools** (the brainstorm's Layer 3) remain acceptable and are not built here:
a server entry would name `fallback:` entries, and when the per-Turn re-stream budget of
[ADR 0082](0082-a-silent-stream-is-cut-and-a-transient-fault-is-ridden-out-under-a-budget.md) is
exhausted the Turn would continue on the next one. It needs its own ADR, written against three
settled decisions: [ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)
binds a session to one server and model, so a pool must be limited to entries serving an
equivalent model or define mid-Turn rehoming explicitly (context window, effort dialect,
prompt-cache loss); [ADR 0047](0047-api-keys-resolve-through-a-per-entry-key-source.md) rejects a
fallback chain across key sources — not across servers, which is no conflict but must be cited;
and ADR 0082's budget is the trigger. A pool would sit behind the `provider.Responder` seam, so
the engine's wire-silence (ADR 0031) is untouched. The measurement recorded here is the evidence
that decides whether it is worth building: pools pay off when stalls, not slow providers, are the
cost.

## Consequences

- **The config surface gains two keys:** `request-extra:` on a `servers:` entry and a root
  `server-stats:`. Neither is model-facing content a Reaction shapes, so neither is a Reaction and
  neither needs a bench gate ([ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md));
  what `request-extra:` sends is the user's own configuration, applied verbatim.
- **The headless protocol gains an `upstream_attempt` event kind** — additive under ADR 0075.
- **A new file under `~/.apogee`** (`server-stats.jsonl`), bounded by the startup trim, carrying
  no content beyond endpoint, model and timings.
- **The vocabulary stays apart:** *Server stats* are wall-clock timings and outcomes per Upstream
  attempt; ADR 0079's *Context cost* is the bytes apogee itself injects. Neither is money.
- **ADR 0024 §6's "binding follows observed reality" clause is superseded** by Decision 3, which
  records the behaviour shipped since b6e51496.
