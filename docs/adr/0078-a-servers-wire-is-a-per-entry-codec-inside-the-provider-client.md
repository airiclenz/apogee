---
Status: accepted
Amends: ADR 0036 (a `servers:` entry gains `wire:`); ADR 0060 decision 3 (the `effort-dialect:` key does not compose with `wire: anthropic`)
---

# A server's wire is a per-entry codec inside the provider Client

## Context

apogee has spoken exactly one wire since the proxy was shelved: OpenAI chat-completions —
`POST /v1/chat/completions`, `Authorization: Bearer`, the OpenAI SSE stream, `GET /v1/models` and
llama.cpp's `/props` for discovery (`internal/provider/wire.go`, `client.go`, `discovery.go`). Every
Upstream apogee has been run against — llama.cpp, Ollama, LM Studio, vLLM, OpenRouter, OpenAI
proper, Groq — reads that shape, which is why the `servers:` list ([ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md))
never had to say *which* protocol an entry speaks.

The 2026-09-06 contender gap assessment (gap 4, bead `apogee-6fp`, accepted by the owner
2026-09-07 as wave 2) named the cost of that: the Anthropic Messages API — the wire Claude is
served on, and one Ollama has also spoken since January 2026 — is unreachable, while the
contenders speak it natively (pi speaks four wire formats; Crush and goose ship typed providers per
vendor). A user with an Anthropic key, or a Messages-speaking proxy, has no entry to write.

Three facts bounded the shape:

- **The engine is wire-silent** ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)).
  `internal/agent` builds a `provider.Request` and reads a `provider.Response`; it neither knows
  nor may learn what bytes carried them. A second wire is therefore a provider-package concern
  from end to end, and the Bypass invariant needs no bench gate — no model-facing text changes.
- **The wire is a property of the endpoint, not the model.** The same reasoning that put
  `effort-dialect:` on the `servers:` entry rather than in a model profile
  ([ADR 0060](0060-effort-is-detected-passively-dialected-per-server-and-picked.md) decision 3, after
  [ADR 0050](0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md) decision 2)
  applies with more force here: a URL speaks one protocol whatever it serves.
- **The Messages wire already spells the effort dial.** ADR 0060's three dialects exist because the
  chat-completions wire has three ways to say "think harder"; the Messages API has one,
  `output_config.effort`, so a dialect word on an anthropic entry names a shape the request never
  travels in.

## Decision

1. **The key.** A `servers:` entry gains `wire: openai | anthropic`. It is validated as an enum
   the way `effort-dialect:` is: any other word is a `ValidateServers` refusal naming the entry, the
   key and the two words (`apogee: servers: entry N ("name"): wire: "bogus" is not a wire — …`).
   The word names a **protocol family**, not a vendor: `openai` is the chat-completions wire
   llama.cpp and OpenRouter read as much as OpenAI does, and `anthropic` is the Messages wire
   whoever serves it. CONTEXT.md gains **Wire** for the term; "dialect" stays the effort word.

2. **The zero value is `openai`.** An absent key — and the empty string it decodes to — is the
   chat-completions wire, so every existing config means today what it meant yesterday and the
   struct field carries `omitempty`: the legacy-migration renderer marshals `ServerEntry`
   (`renderServerEntry`) and must not start writing `wire: ""` into a file it folds.

3. **The wire implies the effort mapping.** On the anthropic wire the engine's Effort
   (ADR 0060 decision 9's channel, unchanged) is encoded as `output_config.effort`; there is no
   dialect to detect and none to force. A non-empty `effort-dialect:` on an `anthropic` entry —
   `auto` and `off` included — is a `ValidateServers` refusal (`wire: anthropic sets the effort
   spelling itself — drop effort-dialect`), rather than one key silently outranking the other.
   Discovery on that wire reports the dial as present, so `/effort` and the footer segment behave as
   they do on a forced dialect.

4. **v1 never requests `thinking`.** The Messages API's extended thinking returns signed thinking
   blocks that must be replayed verbatim on the next request; apogee's transcript carries no such
   carrier, and a request that omits them after a thinking reply is refused by the API. The codec
   therefore never turns thinking on — no request enables a `thinking` configuration, so the model
   answers without it — and folds nothing. A signed-thinking carrier on the session record is a
   follow-up bead, not a v1 promise.

5. **Auth is `x-api-key` only.** The entry's key source ([ADR 0047](0047-api-keys-resolve-through-a-per-entry-key-source.md))
   is unchanged — `api-key`, `api-key-cmd`, `api-key-env`, exactly one — and the codec decides the
   header: `Authorization: Bearer` on the openai wire, `x-api-key` (with `anthropic-version`) on
   the anthropic wire. OAuth and Bearer tokens on the anthropic side are out of scope.

6. **Discovery is `GET /v1/models` with the anthropic headers, and no `/props`.** The Messages
   models list has no window field and Anthropic serves no `/props`, so the context window comes
   from the entry's `context-window:` pin ([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) decision 6 — the pin is never overridden)
   and `apogee probe` states the `/v1/models` outcome and the pinned window. The heartbeat
   Monitor carries the entry's wire so its own Client speaks the right one.

7. **One Client, one codec seam.** The wire is an unexported `wireCodec` inside the one
   `provider.Client`, selected by `provider.WithWire` at construction; every caller that dials —
   the engine's completion Client, the heartbeat Monitor, `apogee probe`, headless — passes the
   entry's wire through the same dial seam it already passes the endpoint and key through. There
   is no second client type, no per-vendor package and no engine-visible branch. The wire capture
   the Inspector reads stays codec-neutral (the bytes as sent and received), and the Inspector
   learns to read Anthropic SSE.

8. **The stub speaks Messages on `/v1/messages`.** `stubllm` gains a `/v1/messages` route beside
   `/v1/chat/completions`, judged by the same script and request log, so the anthropic wire is
   benchable and e2e-testable with the drivers `docs/design/test-drivers.md` already describes.
   The stub's Recorder stays chat-completions: it records a live OpenAI-wire server and nothing
   asks it to record a Messages one.

## Considered and rejected

- **A per-vendor provider package (`provider/anthropic`, `provider/openai`)** — the shape Crush and
  goose ship. Rejected: it duplicates the retry, fault, logprobs and capture machinery the one
  Client already carries, and it invites a vendor table where a protocol-family switch is all the
  facts support. A codec is the narrowest seam that closes the gap.
- **Detecting the wire from the endpoint URL** — `api.anthropic.com` is one host; Ollama and
  proxies speak Messages on any host. A heuristic would be wrong exactly where the key is needed
  most, and a wrong wire fails with a 404 rather than a refusal that names the fix.
- **Calling the key `dialect:`** (the bead's original spelling) — ADR 0060 already spent "dialect"
  on the effort spelling, and the two keys sit side by side on the same entry. One word per concept.
- **Requesting `thinking` and dropping the signed blocks on replay** — the API refuses the request;
  the alternative, storing the signature on the transcript, touches the session record and is its
  own decision.
- **Auto-mapping `effort-dialect:` onto the anthropic wire instead of refusing it** — a key that
  is silently ignored reads as configured while doing nothing, the failure ADR 0060's enum refusal
  exists to prevent.
- **A Recorder that also records Messages** — no live Messages server is part of the bench rig,
  and the stub's Messages route is scripted, not replayed.

## Consequences

- `ServerEntry` gains `Wire` (`yaml:"wire,omitempty"`); `ValidateServers` refuses an unknown wire and
  the anthropic-plus-dialect pair; the seeded template, `docs/manual/configuration.md` and
  `CONTEXT.md` (**Wire**, and **Upstream** widened past "the OpenAI HTTP surface") state the key.
- `internal/provider` grows the codec seam and the Messages encoder, decoder and SSE parser; the
  dial seams in `cmd/apogee` thread the wire; `stubllm` serves `/v1/messages`; the Inspector reads
  Anthropic SSE. Each is its own plan item under `docs/plans/2026-09-16 - 03`.
- Signed-thinking replay on the anthropic wire is filed as a follow-up bead when the codec lands.
- ADR 0036's entry shape and ADR 0060 decision 3 carry this ADR as the dated widening.
