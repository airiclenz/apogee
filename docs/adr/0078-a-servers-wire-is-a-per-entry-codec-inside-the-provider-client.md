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
   *(Superseded by the Amendment (2026-10-02) below: thinking is requested whenever an effort
   resolves.)*

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
  *(Delivered — see the Amendment (2026-10-02) below.)*
- ADR 0036's entry shape and ADR 0060 decision 3 carry this ADR as the dated widening.

## Amendment (2026-10-02) — thinking is requested whenever an effort resolves

Supersedes decision 4 ("v1 never requests `thinking`") and the Consequences line filing
signed-thinking replay as a follow-up bead (apogee-4kl). The session record now carries the
signed `thinking` and `redacted_thinking` blocks and replays them verbatim, only to the anthropic
wire and only to the model that produced them, so the reason decision 4 gave is gone.

- **The thinking mode follows the resolved effort.** When the request's thinking effort resolves to
  a level the Messages API has (`low`, `medium`, `high`, `xhigh`, `max`), the body requests
  `thinking: {"type": "adaptive"}` beside `output_config.effort`. When it resolves to `off`,
  `none` or `minimal`, or to nothing at all, the body requests `thinking: {"type": "disabled"}`
  explicitly, exactly as before, so a request with no effort is byte-identical to what v1 sent.
  (*Superseded by the [Amendment (2026-10-02) — the no-effort shape is per model](#amendment-2026-10-02--the-no-effort-shape-is-per-model)
  below: the no-effort half now depends on the model, and only models outside its table keep
  `disabled`.*)
- **Thinking blocks go back in the order received.** A reply can put a thinking block after its
  text or between its `tool_use` blocks (interleaved thinking, and the progress update that sits
  just before each tool call). The API takes the latest assistant turn back only with its thinking
  blocks in their original sequence, and on the models that check preserved thinking a reordered
  block is an edit that invalidates every later one (a 400). So each carried block keeps its place
  — whether text and how many tool calls preceded it — and the encoder puts it back there rather
  than ahead of the message's other blocks. A block that led its reply is carried bare, as before.
  The reply's text blocks keep their original places too: the message's content is every text
  block joined, so a reply whose text sat in more than one block or after a tool call also carries
  its whole block layout (each text block's length, and where its thinking blocks and tool calls
  sat), and the encoder splits the content back into those text blocks. A layout that no longer
  fits the message — content edited after the reply, a count that differs — is ignored, and the
  blocks go back at their places as above.
  (*Narrowed by [ADR 0092](0092-replayed-signed-thinking-is-guarded-by-a-prefix-digest.md): on
  the preserved-thinking models (Fable 5.1, Mythos 5.1, Opus 5.5, Sonnet 5.5) a reply's blocks go
  back only while the prefix before that reply — `system`, `tools` and every earlier message —
  digests as it did when the reply was produced; otherwise the codec drops them client-side. A
  resumed session replays them only where the resumed prefix still matches.*)
- **The compaction summary never requests thinking on this wire.** The summariser's request is
  forced to the off rung on an anthropic server, as it already was on the kwargs and reasoning
  dialects, so a thinking pass can never spend the summary's output cap.
  (*Superseded for Opus 5.5, Fable 5.x and Mythos 5.x by the
  [Amendment (2026-10-02) — the no-effort shape is per model](#amendment-2026-10-02--the-no-effort-shape-is-per-model)
  below: on those models the off rung requests adaptive thinking at effort `low`, and on Sonnet
  5.5 it requests `between_tools`.*)
- **Thinking wins over sampling.** While the body requests adaptive thinking, it leaves out the
  request's `temperature`, `top_p` and `top_k`, because the API restricts them while thinking is on.
  With thinking disabled they are sent as before. This drop covers only the body the codec builds.
  A `request-extra:` block is merged after the codec and is never refused, so a `temperature` or
  `top_k` written there still reaches the wire and still conflicts with thinking at a resolved
  effort. Leave sampling knobs out of an anthropic entry's `request-extra:`.

## Amendment (2026-10-02) — the no-effort shape is per model

Supersedes the first bullet of the amendment above ("The thinking mode follows the resolved
effort") for requests whose effort resolves to `off`, `none`, `minimal` or nothing, and its third
bullet ("The compaction summary never requests thinking on this wire") for the models below. A
named effort from `low` up is unchanged: `thinking: {"type": "adaptive"}` beside
`output_config.effort`.

`thinking: {"type": "disabled"}` is not a shape every current model accepts. Opus 5.5, Fable 5.x
and Mythos 5.x answer it with a 400 and have no off switch at all; Sonnet 5.5 answers it with a
400 but accepts `between_tools`. Because compaction summaries and the session-naming call both
force the off rung, every compaction on those models failed (bead
`apogee-anthropic-thinking-disabled-400`).

- **The no-effort shape comes from a prefix table on the model id.** The codec matches the
  request's model id, ignoring case, against a short table of id prefixes, and the longest match
  wins:

  | Model id starts with | Body for an effort below `low` |
  |---|---|
  | `claude-opus-5-5`, `claude-fable-5`, `claude-mythos-5` | no `thinking` key, and `output_config.effort: "low"` |
  | `claude-sonnet-5-5` | `thinking: {"type": "between_tools"}`, and no effort |
  | anything else | `thinking: {"type": "disabled"}`, and no effort, byte-identical to before |

  On the first row "no effort" means the least thinking the model allows, not none.
- **A new model needs a table row.** The table is a denylist in `internal/provider`, not a
  capability apogee discovers: the Messages API advertises no tell. A future model that also
  refuses `disabled` keeps getting `disabled`, and failing with a 400, until it has a row. An
  allowlist would have broken every model apogee does not know, including proxies that rename
  models.
- **Only `disabled` keeps the profile's sampling.** The `between_tools` shape and the
  no-key-plus-`low` shape both let the model think, so the body leaves out `temperature`, `top_p`
  and `top_k` exactly as it does for adaptive thinking.
- **The effort hint follows the requested effort, not the body.** The no-key-plus-`low` shape
  writes `output_config` for a request that named no effort. A fault on such a request is still
  reported as one that carried no effort, so it never gains the hint that blames an effort level.
- **Compaction and naming get the same mapping.** Both force the off rung, and the codec maps it
  per model like any other request. On Opus 5.5, Fable 5.x and Mythos 5.x the summariser therefore
  runs with adaptive thinking at effort `low`. The capped-summary fault says the summariser
  "asked for as little reasoning as this server allows" rather than "asked for no reasoning".
