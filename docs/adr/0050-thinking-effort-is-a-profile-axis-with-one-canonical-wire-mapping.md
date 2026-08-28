---
Status: accepted
---

# Thinking effort is a profile axis with one canonical wire mapping

## Context

On 2026-08-14 a slow turn on Qwen3.8-27B sat in bare thinking for ~20 minutes. Live
`/apply-template` evidence (2026-08-15, no inference needed) showed why: the family's chat
template reads a `reasoning_effort` kwarg with levels low/medium/xhigh — and **defaults to
xhigh when the kwarg is omitted**, so every ordinary apogee turn runs at maximum effort today.
The same template also honours `enable_thinking: false` (pre-closes the block), older Qwen3
hybrids honour only `enable_thinking`, and an unsupported value raises in Jinja — the server
answers HTTP 500.

Apogee already has the wire plumbing: `ChatTemplateKwargs` on the chat request, emitted only
when `Request.DisableThinking` is set, whose sole setter is the title namer. The stated anchor
in those comments binds this work: **a caller that asks for nothing changes nothing on the
wire.**

A grill on 2026-08-15 settled the shape; this record is that ratification.

## Decision

**1 — Effort is the emit-side leaf of the Model profile's thinking axis.** A profile entry
spells `thinking: {effort: off|low|medium|high}` beside the parse-side `style/start/end`. One
axis, one dial: `off` is the bottom of it, so no second `enabled:` key exists and no
contradictory combination is spellable. The zero value (absent) means **send nothing** — the
wire anchor holds. The knob rides everything the profile already rides: three-layer resolution,
Rebind on every model switch, the watched config ([ADR 0044](0044-model-profiles-are-per-model-and-mostly-shipped.md),
[ADR 0041](0041-the-config-file-is-watched.md)).

**2 — One canonical wire mapping, owned by the provider Client.** `provider.Request` carries a
semantic `ThinkingEffort` field (the `DisableThinking` pattern: intent at the seam, expression
in the Client). The Client emits `{"enable_thinking": false}` for `off` and
`{"reasoning_effort": "<level>"}` otherwise. There is **no per-family table**: only the
Qwen3.8 behaviour is live-verified, templates that read neither kwarg ignore them, and ADR
0044's per-sighting rule extends to emission — a dialect knob grows only when a second
verified family actually diverges.
*(Amended 2026-08-25 by [ADR 0060](0060-effort-is-detected-passively-dialected-per-server-and-picked.md) §2: two further dialects have since been
sighted — OpenRouter's `reasoning: {effort}` and OpenAI/Groq's top-level `reasoning_effort` —
so the mapping here is the llama.cpp dialect, one of three. Still no per-family table: the
dialect is a property of the endpoint, chosen by passive detection or the per-server
`effort-dialect:` key.)*

**3 — `DisableThinking` is deleted.** The title namer sets `ThinkingEffort = off`, producing
byte-identical output to today. One field, one mapping, no precedence rules — whoever builds a
request states its effort.

**4 — Validation is a load-time enum plus an enriched turn error.** Config load rejects
anything outside the four values. A template that still rejects a valid value stays a turn
failure, but when the failed request carried `chat_template_kwargs` the error hint names
`thinking.effort` as the likely culprit. No `/apply-template` probe — llama.cpp-specific
machinery for an error the enum mostly prevents.
*(Amended 2026-08-25 by [ADR 0060](0060-effort-is-detected-passively-dialected-per-server-and-picked.md) §4: the enum widens to the seven-name
union `off|low|medium|high|minimal|xhigh|max`, plus `none` on the OpenRouter dialect. The
probe stays rejected — detection is passive, from the discovery payloads already fetched.)*
*(Amended 2026-08-28: the hint is gated on the request BODY carrying an effort in ANY dialect —
`chat_template_kwargs`, `reasoning` or `reasoning_effort` — not on the kwargs field alone, and it
is worded to name the intent (`thinking.effort` / the `/effort` override) rather than one
dialect's field. The `off` dialect emits none of the three and so stays unhinted.)*

**5 — `/effort` is a session override layered above the profile.** Resolution is session
override ▸ profile effort ▸ nothing. `/effort low` sets it, bare `/effort` shows the current
resolution, `/effort auto` clears it. It is the human's session intent, not a model fact: it
**survives model switches**, is **never written to config**, and applies to the primary loop
only — a routed sub-agent resolves its own model's profile
([ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md)). The name is
`/effort`, not `/thinking` — the latter is reserved for the deferred thinking-display feature.
*(Amended 2026-08-25 by [ADR 0060](0060-effort-is-detected-passively-dialected-per-server-and-picked.md) §5–§8: `/effort` is a popup picker of the
model's own reported levels — the level-word grammar is removed — hidden from the menu when
the model reports no dial, with the resolved effort shown as a footer segment. The override
still survives model switches, except into a model whose reported level set excludes it,
where it is cleared with a transcript note.)*

**6 — Engine stance.** Effort is configuration, not a Mechanism: it holds under `--bypass`
(the [ADR 0046](0046-the-engine-bounds-every-reply-with-an-output-cap.md) precedent). The
session override is engine session state behind an engine door, so a bench Driver drives it
directly ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)).
And the **shipped shape table never carries an effort** — an out-of-the-box run stays
byte-identical on the wire; effort enters only through user config or the override.

## Considered and rejected

- **Two keys (`enabled:` + `effort:`)**: mirrors the two kwargs but makes `enabled: false,
  effort: high` spellable, which then needs a rule.
- **A top-level `thinking-effort:` profile axis**: the channel and its dial are one concept in
  two directions; the profile already drives both directions at the seams.
- **A per-family dialect field or table now**: config surface for families whose kwarg
  behaviour nobody has verified; decision 2's per-sighting rule instead.
- **Raw `chat-template-kwargs:` maps in the profile**: maximum generality, but exposes wire
  detail in config and bypasses validation; revisit only if a verified family cannot be
  expressed semantically.
- **Probing `/apply-template` at bind time**: llama.cpp-only, a request per bind, and needs a
  fallback story for servers without the endpoint.
- **`/thinking` as the command name**: collides with the deferred display feature's natural
  name.
- **Override cleared on model switch**: treats a human intent as a model fact and costs
  re-typing it after every switch; a knobless model ignores the kwarg anyway.
- **`logit_bias` on `</think>`** (graduated per-token hack) and **llama.cpp
  `--reasoning-budget`** (a launch flag, 0/-1 only — llama-launcher territory): denied.

## Deferred (dated 2026-08-15, not denied)

- **A gpt-oss shipped effort entry** — its `reasoning_effort` kwarg (and the harmony
  `Reasoning:` system-prompt line) is unverified live; per-sighting rule applies.
- **A lower default effort for sub-agents** — the child already resolves its own profile;
  a distinct default is extra policy for later.
- **Visible rendering of thinking tokens** — a separate future plan (owner scope call,
  2026-08-15); `/thinking` stays reserved for it.
