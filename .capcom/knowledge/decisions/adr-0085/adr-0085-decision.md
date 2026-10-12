---
title: ADR 0085 decision
---

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
(ADR 0031):
the TUI records it, headless emits it as an `upstream_attempt` line
(ADR 0075), and a bench can
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
