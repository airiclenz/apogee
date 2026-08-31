---
Status: accepted
---

# Effort is detected passively, dialected per server, and picked from the model's own levels

## Context

[ADR 0050](0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md) put the
thinking-effort dial on the Model profile with **one** canonical wire mapping — llama.cpp's
`chat_template_kwargs` (`enable_thinking: false` / `reasoning_effort`) — and a `/effort`
command taking a level word. That shape was correct for the one family whose behaviour was
live-verified (Qwen3.8, 2026-08-15) and deliberately grew no dialect table until a second
family was actually sighted. Two things have since made the single mapping insufficient and
the command's silence unhelpful:

- **The wire is not one wire.** OpenRouter takes a `reasoning: {effort}` object; OpenAI proper
  (o-series, gpt-5) and Groq take a **top-level** `reasoning_effort` field; self-hosted
  vLLM/SGLang/TGI honour `chat_template_kwargs` but expose no `/props`. Against all three,
  apogee's kwargs map is ignored: the human dials `high`, the footer says nothing, and the
  request goes out at the model's own default. Effort was silently dropped.
- **Nothing told the human whether the dial existed at all.** `/effort` was offered on every
  model, answered identically on a model with no dial, and left no readout of what the next
  request would actually carry.

Meanwhile the vocabulary widened underfoot: `minimal` (OpenAI), `xhigh` (Qwen3.8's template
default), `max`, and OpenRouter's `none` all exist in the wild, and ADR 0050's four-value
load-time enum rejects them.

The information needed to close both gaps is **already fetched**: `provider.Discover` reads
`GET /props` and `GET /v1/models` on every heartbeat. llama.cpp's `/props` now returns the
`chat_template` Jinja text; OpenAI-shaped servers that expose a dial return a per-model
`reasoning` object on `/v1/models`. No new request is needed — which matters, because ADR 0050
explicitly rejected an `/apply-template` bind-time probe.

A grill on 2026-08-25 settled the shape; this record is that ratification. It **amends
ADR 0050** (decisions 2, 4 and 5) and is authoritative where the two differ; ADR 0050 stands
elsewhere. The effort sub-axis and its resolution are untouched
([ADR 0058](0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md)).

## Decision

**1 — Detection is passive, from the two discovery payloads already fetched.** No probe, no
extra request, no live model call. A `/props` `chat_template` whose text mentions
`reasoning_effort` or `enable_thinking` means the dial is supported. A `/v1/models` entry
carrying a `reasoning` object (`supported_efforts`, `default_effort`) on the **active** model
means the dial is supported. Neither ⇒ unsupported. Detection is best-effort exactly like the
context-window and slot-count observations beside it: a parse miss yields the zero value, never
an error, and never fails a beat.

**2 — The wire dialect follows the detection source, and there are three of them.** This is
ADR 0050 decision 2's *per-sighting* dialect growth — three sighted dialects, each its own arm
in the Client's `buildBody`, still no per-family table:

- **`kwargs`** (a `/props` template hit, and the zero value) — today's live-verified mapping,
  byte-for-byte: `off` ⇒ `chat_template_kwargs: {enable_thinking: false}`, any other level ⇒
  `chat_template_kwargs: {reasoning_effort: "<level>"}`.
- **`reasoning`** (a `/v1/models` `reasoning` object) — `off`/`none` ⇒ `reasoning: {enabled:
  false}`, any other level ⇒ `reasoning: {effort: "<level>"}` (OpenRouter).
- **`openai`** (never detected — reached only through the config key of decision 3) — the
  **top-level** `reasoning_effort` field, with `off`/`none` mapped to `minimal`, because an
  OpenAI reasoning model cannot be told not to reason.

The two `reasoning_effort` spellings are different wires and must not be conflated: one is an
entry inside `chat_template_kwargs`, the other a top-level request field. The dialect is a
property of the **endpoint**, not the model family, so it is a per-server fact, resolved where
the completion Client is built and rebuilt on every server switch. Nothing live-verified is
replaced.

**The wire anchor holds unconditionally** ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
an absent effort emits none of the three keys, and the zero dialect reproduces today's bytes
exactly. Detection changes what apogee *may* say, never what it says unasked.

**3 — A per-server `effort-dialect:` key covers the providers detection cannot see.** OpenAI,
Groq and self-hosted vLLM/SGLang/TGI advertise no passive tell. A `servers:` entry may spell
`effort-dialect:` — `auto` (the default: detect as above), `kwargs`, `reasoning`, `openai`, or
`off`. Any value but `auto` **overrides detection**: it forces the wire dialect *and* makes the
model count as supported, so the picker and the footer segment appear. `off` forces
**unsupported** — never send anything — the escape hatch for a server that errors on the kwarg.
An unknown value is a startup error naming the entry and the key. This is the sanctioned
fallback ADR 0050 decision 2 anticipated, and it names a *dialect*, not a family.

**4 — The vocabulary is the model's own reported set when known, else the canonical four.**
The load-time enum of ADR 0050 decision 4 widens from `off | low | medium | high` to the
seven-name union `off | low | medium | high | minimal | xhigh | max`, plus `none` on the
`reasoning` dialect (where `off` maps to it). `""` remains the absence anchor. Where a level a
template rejects still reaches the wire, the enriched turn error naming `thinking.effort`
stays the backstop — the enum was never the only guard.

**5 — Unsupported means hidden from the menu; the typed verb still answers.** When the bound
model reports no dial, the autocomplete dropdown omits the `/effort` row and the footer omits
the effort segment. The command registry and the parser stay complete: a hand-typed `/effort`
still routes, and answers with one note — *this model reports no thinking-effort dial*. No
greyed-out disabled-row machinery is introduced; absence is the whole signal.

**6 — The footer carries the resolved effort, and it is present exactly when `/effort` is.**
The footer run reads `host ✦ model ✦ <effort> ✦ ~/repo` — the effort sitting with the upstream
facts it belongs to, before the local workdir. The word shown is what the **next request will
actually carry**: session override ▸ profile `thinking.effort:` ▸ the server-reported
`default_effort` ▸ the word `auto` (a `/props` hit, where the default is unknowable). The word
sits in the footer's LEFT run with the upstream facts it belongs to, so a window too narrow to hold
both ends truncates that run with an ellipsis exactly as its neighbouring segments do; only the mode
marker on the right drops WHOLE, because a clipped mode word would name a blast radius the session
is not in. *(Amended 2026-08-28 — this decision previously said the segment dropped whole at a
narrow footer, like the other optional segments. It never did: `footerContent`
(`internal/tui/model.go`) has always ellipsized the left run and dropped only the marker. Leaving
with its separator stays what an UNNAMED segment does — decision 5's absence, not narrowness. No
footer code changed.)*

**7 — `/effort` is a popup picker; the text grammar is removed.** Bare `/effort` opens a
fixed-choice popup ([ADR 0053](0053-popup-surfaces-embed-one-list-surface.md), the
`pickerCycle`/`pickerScheduleMode` shape), listing exactly the model's reported
`supported_efforts` when detection reports a set, and otherwise the canonical four — dialect-
aware, so the `openai` dialect lists `minimal/low/medium/high` and every other lists
`off/low/medium/high` — with an `auto` row appended. Picking a level layers the session
override; `auto` clears it; every accept ends on the existing resolution note. The
`off|low|medium|high|auto` word grammar of ADR 0050 decision 5 is deleted, not deprecated: a
half-removed parser is worse than either end state.

**8 — The override survives model switches, except into a set that excludes it.** ADR 0050
decision 5's rule stands — the override is human session intent, never persisted, primary loop
only. One narrow exception: when a switch binds a model that **reports** a `supported_efforts`
set the live override is not in, the override is cleared and a transcript note says so, because
that override could only produce a turn failure. A model that reports no set keeps the override
untouched; the enriched turn error remains its backstop.

**9 — The UI facts travel the Beat; only the dialect enters the engine.** Supported / reported
levels / reported default ride the heartbeat Beat and are read host-side, the way the slot
count and the context window already are. The engine never learns the reported set — the menu
gate, the footer segment, the picker rows and the override-clear are all host policy. The one
fact that must reach the engine is the wire **dialect**, and it travels the channel the output
cap already uses: the composition-root rebind spec, into a private Agent field, onto
`provider.Request`. Intent stays semantic at the seam, expression stays in the Client.
*Amended 2026-08-31:* the dialect now reaches the engine through a SECOND channel alongside the
rebind spec — the **Delegation target**
([ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md)), which carries
the Sub-agent server's own dialect to a routed child, because decision 3 makes the dialect a
property of the server and a routed child is on another one (a target naming none leaves the
child on the parent's). Both channels are composition-root ▸ engine, and the dialect is still
the ONLY effort fact that crosses: the reported set, the reported default and `Mandatory` stay
host-side under this decision's own rule.

## Considered and rejected

- **A bind-time `/apply-template` probe** to learn the dial — already rejected by ADR 0050
  (llama.cpp-only, a request per bind, no fallback story) and re-rejected here: the two
  payloads discovery already fetches answer the question for free.
- **Auto-detecting the top-level `reasoning_effort` dialect** — OpenAI and Groq advertise no
  passive tell on `/v1/models`, so any detection would be a provider-name or URL heuristic.
  The `effort-dialect:` key covers that class explicitly instead of guessing.
- **A per-request UI-facts channel into the engine** (reported levels, default) — the host
  already receives them on the Beat; a second path would duplicate state and give the engine a
  fact it has no use for.
- **A greyed-out disabled `/effort` row in the menu** — new machinery for one command, where
  omission already reads correctly and the typed verb still explains itself.
- **Keeping the `/effort <level>` text grammar beside the picker** — two spellings of one
  question, and the word grammar cannot express a model's reported vocabulary.
- **A per-family dialect table** — ADR 0050 decision 2's standing refusal. Dialects grow per
  *sighting*, and the endpoint, not the family, is what determines the wire shape.
- **Clearing the override on every model switch** — rejected in ADR 0050 decision 5 and still
  rejected; decision 8's exception fires only on a reported set that provably excludes it.
- **Threading the dialect into the out-of-band title/naming call** — deferred, not denied: the
  namer keeps its `chat_template_kwargs` `off` mapping, so on an OpenRouter thinking model a
  generated title may reason to the cap. Pre-existing edge, recorded rather than fixed here.
  *Resolved 2026-08-25* — the namer now states the dialect the beat observed (`title.Prompt`
  takes it; the composition root reads it off the same live latch the rebind path writes), so
  `off` reaches every dialect in its own shape and the fallback re-send is skipped where the
  dialect put nothing on the wire.

## Consequences

- ADR 0050 decisions 2, 4 and 5 carry dated pointers here. Decision 2's "one canonical wire
  mapping" reads as one mapping *per dialect*; decision 4's enum widens; decision 5's command
  becomes a picker and gains the narrow clear-on-switch exception.
- `provider.Discover` reports an `EffortSupport` observation (supported, dialect, levels,
  default) on `ModelInfo`; it rides the Beat to the TUI and the dialect rides the rebind spec
  to the engine — and, since 2026-08-31, the Delegation target to a routed child (ADR 0045).
- `servers:` entries gain `effort-dialect:`; `CONTEXT.md`'s Thinking-effort entry and the
  configuration and commands manual pages state the widened vocabulary, the three dialects, the
  footer readout and the picker.
- The out-of-band naming call carries the observed dialect, read from the composition root's
  live latch, never from the engine (ADR 0022 addendum — it is not a Turn).
