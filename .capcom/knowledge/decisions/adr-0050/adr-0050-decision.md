---
title: ADR 0050 decision
---

**1 — Effort is the emit-side leaf of the Model profile's thinking axis.** A profile entry
spells `thinking: {effort: off|low|medium|high}` beside the parse-side `style/start/end`. One
axis, one dial: `off` is the bottom of it, so no second `enabled:` key exists and no
contradictory combination is spellable. The zero value (absent) means **send nothing** — the
wire anchor holds. The knob rides everything the profile already rides: three-layer resolution,
Rebind on every model switch, the watched config (ADR 0044,
ADR 0041).

**2 — One canonical wire mapping, owned by the provider Client.** `provider.Request` carries a
semantic `ThinkingEffort` field (the `DisableThinking` pattern: intent at the seam, expression
in the Client). The Client emits `{"enable_thinking": false}` for `off` and
`{"reasoning_effort": "<level>"}` otherwise. There is **no per-family table**: only the
Qwen3.8 behaviour is live-verified, templates that read neither kwarg ignore them, and ADR
0044's per-sighting rule extends to emission — a dialect knob grows only when a second
verified family actually diverges.
*(Amended 2026-08-25 by ADR 0060 §2: two further dialects have since been
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
*(Amended 2026-08-25 by ADR 0060 §4: the enum widens to the seven-name
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
(ADR 0045). The name is
`/effort`, not `/thinking` — the latter is reserved for the deferred thinking-display feature.
*(The reservation was spent 2026-09-02: `/thinking` now opens the plain-text thinking pane —
CONTEXT.md "Thinking pane", layout.md "The `/thinking` popup". `/effort` is unchanged.)*
*(Amended 2026-08-25 by ADR 0060 §5–§8: `/effort` is a popup picker of the
model's own reported levels — the level-word grammar is removed — hidden from the menu when
the model reports no dial, with the resolved effort shown as a footer segment. The override
still survives model switches, except into a model whose reported level set excludes it,
where it is cleared with a transcript note.)*

**6 — Engine stance.** Effort is configuration, not a Mechanism: it holds under `--bypass`
(the ADR 0046 precedent). The
session override is engine session state behind an engine door, so a bench Driver drives it
directly (ADR 0031).
And the **shipped shape table never carries an effort** — an out-of-the-box run stays
byte-identical on the wire; effort enters only through user config or the override.
