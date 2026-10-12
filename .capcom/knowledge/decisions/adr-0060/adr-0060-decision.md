---
title: ADR 0060 decision
---

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

**The wire anchor holds unconditionally** (ADR 0031):
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
sits in the footer's LEFT run with the upstream facts it belongs to, and that is also where a narrow
window spends first: the row is composed TO the width in priority order — the session's spend given
up first where a call was priced (amendment 2026-10-03 below), then the effort word, then the
workdir, then the host — while the model, the `✦ offline` marker and the mode marker
are what the row never gives up, the marker dropping only where it cannot seat whole, because a
clipped mode word would name a blast radius the session is not in.

*(Amended 2026-08-28 — this decision previously said the segment dropped whole at a narrow footer,
like the other optional segments. It never did: `footerContent` (`internal/tui/model.go`) has always
ellipsized the left run and dropped only the marker. Leaving with its separator stays what an
UNNAMED segment does — decision 5's absence, not narrowness. No footer code changed.)*

*(Amended 2026-09-02, superseding the amendment above — the footer now fits by PRIORITY
(`internal/tui/footerfit.go`; `layout.md`, "The footer's upstream slot"). The effort word is
priority 3 and is the FIRST thing a narrow row gives up, whole and with its separator, before the
workdir (2) and the host (1); the mode marker is priority 0 and survives every rung above the fit's
floor. So narrowness now does what the 2026-08-28 note said it did not, and for the effort segment
alone the two exits — nothing named it, or the row could not afford it — look the same on screen.
What the word SAYS, and that it is present exactly when `/effort` is, are untouched.)*

*(Amended 2026-10-03, superseding "the effort word is priority 3 and is the FIRST thing a narrow
row gives up" in the amendment above — ADR 0093 adds a spend segment, the session's **Spend
(money)** after the workdir, present only once a call was priced. It is priority 4 and is now the
first thing a narrow row gives up, whole and with its separator; the effort word stays priority 3
and is the first of the outward-in facts to go, before the workdir (2) and the host (1). In a
session that priced no call there is no spend segment, and the effort word is still the first
thing the row gives up.)*

**7 — `/effort` is a popup picker; the text grammar is removed.** Bare `/effort` opens a
fixed-choice popup (ADR 0053, the
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
(ADR 0045), which carries
the Sub-agent server's own dialect to a routed child, because decision 3 makes the dialect a
property of the server and a routed child is on another one (a target naming none leaves the
child on the parent's). Both channels are composition-root ▸ engine, and the dialect is still
the ONLY effort fact that crosses: the reported set, the reported default and `Mandatory` stay
host-side under this decision's own rule.
