# /effort detection, dialects, footer readout and picker — implementation plan

**Goal:** make the thinking-effort dial self-aware. Auto-detect whether the bound model
supports an effort dial (passively, from the discovery payloads already fetched); hide
`/effort` from the command menu when it does not; show the resolved effort as a footer
segment (`host ✦ model ✦ high ✦ ~/repo`) when it does; turn `/effort` into a popup picker
listing the model's own reported levels (no text parameter); and teach the provider Client
three wire dialects — llama.cpp's `chat_template_kwargs`, OpenRouter's `reasoning: {effort}`,
and OpenAI/Groq's top-level `reasoning_effort` — chosen from what detection saw, with a
per-server `effort-dialect:` config key for providers detection can't see, so effort actually
reaches the endpoint instead of being silently dropped.

**Date:** 2026-08-25
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources (precedence in this order when an item disagrees with them):**

1. `docs/adr/0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md` —
   the existing decision this feature amends. Item 1 writes the amending ADR 0060; from
   then on ADR 0060 is authoritative where the two differ, ADR 0050 elsewhere.
2. `docs/adr/0058-the-thinking-axis-resolves-as-two-sub-axes-style-and-effort.md` — effort
   is the emit-side half of the thinking axis, resolved independently of channel style.
3. `docs/adr/0031-…embeddable-engine.md` — the wire-silent anchor: a caller that asks for
   nothing puts byte-identical bytes on the wire. Every item preserves it.
4. `CONTEXT.md` "Thinking effort"; `docs/manual/configuration.md` (the effort section) and
   `docs/manual/commands.md` (the `/effort` row).
5. Pinned commit at write time: `2e2dc1eb`.

**Ratified design calls** (owner, grill session 2026-08-25):

- **Detection is passive, from the two discovery payloads already fetched — no probe.**
  llama.cpp: `GET /props` now returns `chat_template` (the Jinja text); a template
  mentioning `reasoning_effort` or `enable_thinking` means the dial is supported.
  OpenRouter/OpenAI-shaped: `GET /v1/models` returns a per-model `reasoning` object
  (`supported_efforts`, `default_effort`); its presence means supported. Neither ⇒
  unsupported. This is consistent with ADR 0050's explicit rejection of an
  `/apply-template` bind-time probe.
- **Wire dialect follows the detection source.** A `/props` template hit keeps today's
  live-verified `chat_template_kwargs` mapping (`enable_thinking:false` / `reasoning_effort`).
  A `/v1/models` `reasoning` object present ⇒ emit `reasoning: {effort}`. Neither ⇒ emit
  nothing (the wire anchor holds). This is ADR 0050 decision 2's per-sighting dialect
  growth, not a per-family table; nothing live-verified is replaced.
- **Vocabulary: the model's own reported set when known, else the canonical four.** The
  effort vocabulary widens from `off/low/medium/high` to the seven-name union
  `off/low/medium/high` plus `minimal/xhigh/max` (and `none`, mapped from `off` on the
  OpenRouter dialect). The picker lists exactly the model's `supported_efforts` (plus
  `auto`) when detection reports a set; a `/props` template hit, whose text states no
  vocabulary, lists the canonical four (plus `auto`).
- **Unsupported ⇒ hidden from the menu; the typed verb still answers.** The autocomplete
  dropdown omits `/effort` and the footer omits the segment when the dial is unsupported.
  Typing `/effort` by hand still runs and returns one note ("this model reports no
  thinking-effort dial"). No new greyed-out disabled-row machinery.
- **Footer segment content: override ▸ profile ▸ reported default ▸ `auto`.** Shows the
  level the next request will actually carry: the session override if set, else the
  profile's `thinking.effort:`, else the server's reported `default_effort` (OpenRouter),
  else the word `auto` (a `/props` hit, where the default is unknowable). The segment is
  present iff `/effort` is available.
- **`/effort` is a popup picker, never a text parameter.** Bare `/effort` opens a picker
  (the `pickerCycle`/`pickerScheduleMode` fixed-choice shape); the old `off|low|medium|high|auto`
  text grammar is removed. A picked level layers the session override; an `auto` row clears
  it; every accept ends on the existing resolution note.
- **Override clear-on-switch.** The session override survives model switches (ADR 0050 D5),
  but when a switch binds a model that *reports* a `supported_efforts` set the override is
  not in, the override is cleared with a transcript note. A model that reports no set (a
  `/props` hit) keeps the override as-is — the existing enriched turn-error stays the
  backstop.
- **A third wire dialect, plus a per-server config override for detection-blind providers**
  (owner, 2026-08-25). Passive detection recognises only llama.cpp and OpenRouter; OpenAI
  proper (o-series, gpt-5), Groq and similar use a *top-level* `reasoning_effort` field and
  advertise nothing sniffable on `/v1/models`, and self-hosted vLLM/SGLang/TGI honour
  `chat_template_kwargs` but expose no `/props`. So: (a) add a third dialect
  `EffortDialectOpenAI` mapping to the top-level `reasoning_effort` field (covers OpenAI and
  Groq); and (b) add a per-server config key `effort-dialect:` — `auto` (default: detect as
  above), `kwargs`, `reasoning`, `openai`, or `off` — that, when set to anything but `auto`,
  overrides detection: it forces the wire dialect AND makes the model count as supported
  (`off` forces *unsupported* — never send, an escape hatch for a server that errors on the
  kwarg). This is exactly ADR 0050 D2's per-sighting dialect growth; the config key is the
  fallback for providers with no passive tell, not a per-family table.

**Ratified design calls made by the plan-writer at write time** (owner, 2026-08-25 — from
codebase evidence, to keep items unambiguous):

- **The UI facts (supported / efforts / default) travel the Beat, and are read by the TUI
  from `heartbeatState`; only the wire DIALECT travels into the engine.** The menu gate,
  footer segment, picker rows and override-clear all live in `internal/tui` and read the
  detection folded into `m.hb`. The engine never learns the reported set — it does not need
  it. This mirrors how `TotalSlots` and the context window already ride the Beat and are
  consumed host-side (`internal/heartbeat/heartbeat.go:46`, `internal/tui/heartbeat.go`).
- **The wire dialect is a per-server fact carried on `RebindSpec` into the Agent, stored as
  a private Agent field, and put on `provider.Request` by `internal/agent/wire.go`.** It
  reaches the Agent through the exact channel `MaxOutputTokens`/`ResponseReserveFraction`
  already use — the composition-root rebind (`cmd/apogee/wire_verbs.go`), fed from the TUI
  `Rebind` call. The Client expresses it in `buildBody`; the intent stays semantic at the
  seam, exactly the `ThinkingEffort` pattern (`internal/agent/wire.go:44`,
  `internal/provider/client.go:485`). Dialect is per-server because the wire shape is a
  property of the endpoint, and the completion Client is rebuilt per server switch.
- **The title path is left on the existing `chat_template_kwargs` `EffortOff` mapping**
  (`internal/title/title.go:166`), i.e. no dialect is threaded into the out-of-band naming
  call. On an OpenRouter thinking model the kwarg is ignored and a title may reason to the
  cap; that pre-existing edge is recorded as a DEFER, not fixed here (out of scope below).

**Out of scope (explicit):**

- Windows: nothing platform-specific here; detection and dialect are OS-independent.
- The title/naming call's dialect (see the write-time call above) — DEFER.
- Native (non-OpenAI-compatible) provider APIs — Anthropic's own Messages API, Google's — are
  out of scope entirely: apogee speaks the OpenAI-compatible wire only. Those providers are
  reachable through OpenRouter (the `reasoning` dialect) instead.
- Auto-detecting the OpenAI/Groq top-level `reasoning_effort` dialect from `/v1/models` — no
  passive tell exists, so that class is reached only through the `effort-dialect:` config key,
  never by detection.
- A `thinking-effort` *profile* probe or any live model call for detection — rejected by
  ADR 0050 and re-affirmed above.
- Persisting the override or writing effort to config — unchanged; the override stays
  session-only (ADR 0050 D5).
- Any change to how the reasoning *channel* (style/parse) resolves (ADR 0058) — untouched.

**Standing requirements:** `skills: coding-standards`.

---

## 1. ADR 0060 + CONTEXT + manual: ratify detection, dialects, vocabulary and the picker — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the item's Files list named four documents; a fifth,
`docs/adr/0050-…`, gained three dated one-line amendment pointers (under decisions 2, 4
and 5) because the plan header declares ADR 0060 amending and superseding those decisions
and the repo's ADR convention records that on the amended record (as ADRs 0008/0057 carry
for ADR 0059). Additive only — no decision text was altered.
NOTES (2026-08-25): docs-only item, so no `make check`; acceptance greps and `go build
./...` both clean.

**What:** Write `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`
(status accepted, dated 2026-08-25) recording, as an amendment to ADR 0050, the seven
ratified design calls in this plan's header: passive detection from `/props` `chat_template`
and `/v1/models` `reasoning`; the three wire dialects (`chat_template_kwargs` /
`reasoning: {effort}` / top-level `reasoning_effort`) — two chosen from the detection source,
the third reached only via the config key; the per-server `effort-dialect:` override for
detection-blind providers; the seven-name vocabulary union with the model's reported set
preferred; menu-hide-when-unsupported with the typed verb still answering; the footer segment
(override ▸ profile ▸ reported default ▸ `auto`); `/effort` as a picker with the text grammar
removed; and override clear-on-switch. Cross-reference ADR 0050 (decisions 2, 4, 5) and ADR
0058. Add a "Considered and rejected" line for the bind-time probe (already rejected in 0050),
for a per-request UI-facts channel into the engine (rejected: the host reads them from the
Beat), and for auto-detecting the top-level `reasoning_effort` dialect (rejected: no passive
tell — the config key covers it).

Then update the docs to match the ratified end state (the code items below implement it):
- `CONTEXT.md` "Thinking effort" section — note the seven-name vocabulary, that the dial is
  now detected passively and surfaced in the footer, that the command is a picker, and add a
  cross-reference to ADR 0060 beside the ADR 0050 one. Keep the _Avoid_ list.
- `docs/manual/configuration.md` — the effort paragraph: the widened vocabulary, the three
  wire dialects (llama.cpp `chat_template_kwargs`, OpenRouter `reasoning`, OpenAI/Groq
  top-level `reasoning_effort`), the new per-server `effort-dialect:` key
  (`auto`/`kwargs`/`reasoning`/`openai`/`off`) and when to reach for it (a provider effort
  detection can't see), and that `/effort` opens a picker of the model's reported levels
  rather than taking a level word. Document `effort-dialect:` in the `servers:` entry
  reference too.
- `docs/manual/commands.md` — the `/effort` row: reword to "opens a picker of the levels
  this model supports; hidden when the model reports no dial", and note the footer readout.

**Files:** `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`,
`CONTEXT.md`, `docs/manual/configuration.md`, `docs/manual/commands.md`.

**Tests:** none (docs only).

**Acceptance:** `test -f docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`;
`grep -q "ADR 0060" CONTEXT.md`; `grep -qi "picker" docs/manual/commands.md`;
`grep -qi "reasoning" docs/manual/configuration.md`. Docs-only commit — no `make check`.

**Commit:** `docs(adr): ratify effort detection, per-server dialects and the picker (ADR 0060)`

---

## 2. Widen the effort vocabulary in the domain and config layers — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the item's text says `Valid()` accepts "all seven plus `""`", but it also
adds four constants beside the existing four and dictates an error string naming eight words;
`Valid()` therefore accepts all EIGHT named levels (the seven-name union plus `none`) plus `""`,
which is the only reading consistent with both, with ADR 0060 decision 4 and with the manual.
NOTES (2026-08-25): two files beyond the item's Files list were updated because this change
falsified what they state and no later plan item owns them — `apogee.go` (+`example_test.go`,
whose "one reference each" list pins the façade's re-exports) mirrors the domain constants for
the embeddable engine, and `internal/config/defaults/config.yaml`'s `effort` key reference said
"off | low | medium | high … a value outside those four is a startup error".
NOTES (2026-08-25): the required "effort: bogus fails with the widened message" coverage was
added by widening the existing `hihg` case in `TestApplyConfigBadModelProfileAxisErrors` rather
than adding a second near-identical case; the "loads" half is a new `wide-effort` profile with
`effort: xhigh` in `TestApplyConfigModelProfileEffort`.

**What:** Extend `domain.ThinkingEffort` (`internal/domain/config.go:448`) with four new
constants — `EffortNone = "none"`, `EffortMinimal = "minimal"`, `EffortXHigh = "xhigh"`,
`EffortMax = "max"` — beside the existing `off/low/medium/high`. Widen `Valid()`
(`internal/domain/config.go:465`) to accept all seven plus `""` (still the unset/wire-anchor
zero). Update the doc comment on the type to state the seven-name union and that `off` is the
apogee-canonical spelling of "no reasoning" (the OpenRouter dialect renders it as `none` —
item 4). In `internal/config/config.go`, update `validateThinkingAxes`'s error string
(`internal/config/config.go:1967`) to name the widened vocabulary ("want off, low, medium,
high, minimal, xhigh, max, or none, or leave the key out…"). Do NOT change the wire mapping
here (item 4) and do NOT touch `internal/agent/wire.go`'s `toProviderEffort` here (item 4
widens the provider side and its bridge together).

Restated standards for this item: this is a mechanical enum widening — keep the existing
comment style (the type's doc explains why `""` is not a level); the `switch` in `Valid()`
stays exhaustive over the named constants, no `default:true`.

**Files:** `internal/domain/config.go`, `internal/domain/config_test.go`,
`internal/config/config.go`, `internal/config/config_test.go`.

**Tests:** extend `TestThinkingEffortValid` (`internal/domain/config_test.go:70`) to cover
the four new levels as valid and a junk value as invalid. Add a config-load case asserting
`thinking: {effort: xhigh}` loads and `thinking: {effort: bogus}` fails with the widened
message.

**Acceptance:** `go test ./internal/domain/ ./internal/config/`.

**Commit:** `feat(effort): widen the thinking-effort vocabulary to seven levels`

---

## 3. Detect effort support during provider discovery — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the item says the `reasoning` object is read when "the ACTIVE model carries one"; implemented as the advertised entry that DESCRIBES the active model — the exact id, else the base slug before the first ':' — mirroring how `resolveHint` already sources the context window, so an unlisted OpenRouter routing variant (`vendor/model:exacto`) inherits its base model's answer instead of reading as a model with no dial. Covered by its own test case.
NOTES (2026-08-25): the per-entry `reasoning` field is held as `json.RawMessage` and decoded by a `decodeReasoning` helper rather than as a typed pointer. A typed `*modelReasoning` made a server that writes a non-object there (e.g. `"reasoning":"high"`) fail the whole `/v1/models` decode, turning discovery into an error — the opposite of the item's "any parse miss yields the zero value, never an error". Regression caught by `TestDiscover_MalformedEffortPayloadsStayBestEffort`, which now also pins an explicit `"reasoning":null`.
NOTES (2026-08-25): `internal/provider/discovery.go` is now 411 lines, just past the coding-standards ~400-line guidance. Not split — the item's Files list scopes the change to this file and splitting it would move existing content between files.

**What:** Teach `provider.Discover` to report, per the ratified detection rule, whether the
active model supports an effort dial and (when the server says) which levels and its default.
Add an `EffortSupport` struct to `internal/provider/discovery.go`:

```go
type EffortSupport struct {
    Supported bool     // the dial is usable on this model
    Dialect   EffortDialect // which wire shape reaches it (see item 4's type)
    Efforts   []string // the reported level set, nil when the source states none
    Default   string   // the server-reported default level, "" when none
}
```

Populate it from the two probes already fetched:
- In `discoverProps` (`internal/provider/discovery.go:139`), read the new `chat_template`
  string field on `propsResponse` (`GET /props` returns it). A template containing
  `reasoning_effort` or `enable_thinking` ⇒ `Supported=true`, `Dialect=EffortDialectKwargs`,
  `Efforts=nil` (the template text names no vocabulary), `Default=""`.
- In `discoverModels`/`toModelInfo` (`internal/provider/discovery.go:219`), read a per-model
  `reasoning` object on the `modelsResponse` entries (`supported_efforts []string`,
  `default_effort string`). When the ACTIVE model carries one ⇒ `Supported=true`,
  `Dialect=EffortDialectReasoning`, `Efforts=<supported_efforts>`, `Default=<default_effort>`.
- The `reasoning`-object source wins over the template source when both somehow appear (a
  server advertising the structured field is speaking the more specific dialect). Neither ⇒
  zero `EffortSupport{}` (`Supported=false`).

Add `EffortSupport EffortSupport` to `ModelInfo` and set it in `Discover`
(`internal/provider/discovery.go:90`), after the props/models merge. Keep the whole thing
best-effort exactly like the window/slots probe: any parse miss yields the zero value, never
an error.

Depends on item 4 for the `EffortDialect` type — declare it in item 4; this item imports it.
(To keep the two provider items buildable independently, item 4 lands first: see its
DEPENDS note.)

Restated standards: this is a deep-ish addition to one module (discovery) behind the existing
`Discover` seam — no new exported function, the detection is a private helper feeding the
struct. Keep the "one observation, best-effort" doc idiom the window/slots fields already use.

**Files:** `internal/provider/discovery.go`, `internal/provider/discovery_test.go`.

**Depends on item 4** (for the `EffortDialect` type).

**Tests:** table cases in `discovery_test.go`: a `/props` payload whose `chat_template`
mentions `reasoning_effort` ⇒ supported+kwargs+no-set; a `/v1/models` payload with a
`reasoning` object on the active model ⇒ supported+reasoning+set+default; a payload with
neither ⇒ unsupported; both present ⇒ reasoning dialect wins.

**Acceptance:** `go test ./internal/provider/ -run 'Discover|Effort'`.

**Commit:** `feat(effort): detect effort support and dialect during discovery`

---

## 4. Teach the provider Client the three wire dialects — ✅ DONE (2026-08-25)

NOTES (2026-08-25): item 2's text said item 4 "widens the provider side and its bridge together",
but item 6 explicitly owns `internal/agent/wire.go`'s `toProviderEffort` widening and item 4's own
Files list names provider files only — the bridge is therefore left to item 6, where its test is
also specified. It still compiles and stays total in the meantime (an unmapped domain level yields
`""`, which emits nothing).
NOTES (2026-08-25): the mapping lives in a package-level `applyEffort` helper beside `buildBody`
rather than as a fourth inline switch inside it; `buildBody` was already at the size limit and the
three-dialect doc comment belongs on the mapping, not on the body projection.
NOTES (2026-08-25): the item text says the reasoning/openai arms take "any other non-empty level".
Implemented as "any other NAMED level" (the eight-word vocabulary) so the item's own "unrecognised
non-empty value emits nothing / Client stays total" invariant holds identically on all three
dialects; `TestBuildBody_UnknownEffortEmitsNothing` now asserts that across the four dialect
spellings.

**What:** Add the dialect type and the two new mappings. In `internal/provider/wire.go`:
widen the `Effort` vocabulary to the seven-name union (add `EffortNone="none"`,
`EffortMinimal="minimal"`, `EffortXHigh="xhigh"`, `EffortMax="max"` beside the existing
constants) with `""` still the absence anchor; add an `EffortDialect` type with FOUR values —
`EffortDialectNone` (zero), `EffortDialectKwargs`, `EffortDialectReasoning`,
`EffortDialectOpenAI`; add an `EffortDialect EffortDialect` field to `provider.Request`
documented as the semantic per-server seam (nil/zero ⇒ the existing `chat_template_kwargs`
behaviour, preserving every current caller byte-for-byte).

In `internal/provider/wirejson.go`, add to `chatRequest`, beside `ChatTemplateKwargs`, both
(each `omitempty`, omitted when nil/zero so the anchor holds):
- `Reasoning *reasoningField` — `type reasoningField struct { Effort string `json:"effort,omitempty"`; Enabled *bool `json:"enabled,omitempty"` }` (OpenRouter);
- `ReasoningEffort *string `json:"reasoning_effort,omitempty"`` (OpenAI/Groq top-level).

In `internal/provider/client.go` `buildBody` (`internal/provider/client.go:485`), branch on
`req.EffortDialect`:
- `EffortDialectReasoning`: `EffortOff`/`EffortNone` ⇒ `reasoning: {enabled: false}`; any
  other non-empty level ⇒ `reasoning: {effort: "<level>"}`; `""` ⇒ nothing.
- `EffortDialectOpenAI`: any non-empty level ⇒ top-level `reasoning_effort: "<level>"`, with
  `EffortOff`/`EffortNone` mapped to `"minimal"` (OpenAI reasoning models cannot disable
  reasoning; `minimal` is the documented floor); `""` ⇒ nothing. Other levels pass through
  verbatim (`minimal/low/medium/high` are OpenAI's set; `xhigh/max` pass through and the
  server rejects an unknown one, which stays the enriched turn error).
- `EffortDialectKwargs` **and the zero `EffortDialectNone`**: today's exact behaviour —
  `EffortOff` ⇒ `enable_thinking:false`, `low/medium/high` ⇒ `reasoning_effort` (as a
  `chat_template_kwargs` map entry, NOT the top-level field). Extend the kwargs level arm to
  pass `minimal/xhigh/max/none` through as the kwargs `reasoning_effort` too (the template
  forwards whatever it is given; an unsupported one stays the enriched turn error).
Keep the "unrecognised non-empty value emits nothing / Client stays total" invariant.

Restated standards: one canonical mapping per dialect, in the Client, no per-family table
(ADR 0050 D2 — three sighted dialects, each its own arm). The `EffortDialect` zero value MUST
reproduce today's wire exactly — assert it. Note the two distinct `reasoning_effort` spellings
(a top-level field for OpenAI vs a `chat_template_kwargs` entry for llama.cpp) so a reader does
not conflate them.

**Files:** `internal/provider/wire.go`, `internal/provider/wirejson.go`,
`internal/provider/client.go`, `internal/provider/client_test.go`.

**Tests:** extend the existing `TestBuildBody_*` effort tests
(`internal/provider/client_test.go:406`): kwargs dialect (and zero dialect) reproduce today's
bytes for off/low/medium/high; reasoning dialect emits `reasoning.effort` for a level and
`reasoning.enabled=false` for off; openai dialect emits top-level `reasoning_effort` for a
level and `"minimal"` for off, and no `chat_template_kwargs`; a widened level (`xhigh`)
round-trips on the three dialects; `EffortDialect` zero + `ThinkingEffort` "" emits none of the
three keys (byte-identical anchor).

**Acceptance:** `go test ./internal/provider/`.

**Commit:** `feat(effort): add the OpenRouter and OpenAI wire dialects to the Client`

---

## 5. Carry effort detection on the heartbeat Beat — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the beat test is a new table-driven `TestBeatCarriesEffortSupport` (mirroring `TestBeatCarriesTotalSlots`) rather than extra asserts inside `TestBeatCarriesDiscovery` — same file, same stub server, keeps one logical subject per test.

**What:** Add `EffortSupport provider.EffortSupport` to `heartbeat.Beat`
(`internal/heartbeat/heartbeat.go:46`) and populate it in `Monitor.Beat`
(`internal/heartbeat/heartbeat.go:122`) from `info.EffortSupport`, beside the existing
`ContextWindow`/`TotalSlots` copies. Document it like `TotalSlots`: a property of the
active binding, re-observed every interval, that only the host acts on. No behaviour changes
when the field is zero.

Restated standards: one more field on an existing observation value; do not introduce a
second probe or a second call — it rides the Beat that already lands every interval.

**Files:** `internal/heartbeat/heartbeat.go`, `internal/heartbeat/heartbeat_test.go`.

**Depends on item 3** (for `provider.EffortSupport`).

**Tests:** extend the Monitor beat test to assert the `EffortSupport` from a stub discovery
is copied onto the Beat.

**Acceptance:** `go test ./internal/heartbeat/`.

**Commit:** `feat(effort): carry detected effort support on the heartbeat beat`

---

## 6. Plumb the wire dialect from the beat into the engine request

**What:** Deliver the per-server dialect from the beat down to `provider.Request`, through
the existing rebind channel.

- `internal/agent/rebind.go`: add `EffortDialect provider.Effort`-style field to
  `RebindSpec` (use the `provider.EffortDialect` type) documented like `MaxOutputTokens` —
  a per-server fact, carried because the pin has no engine setter of its own. In `Rebind`
  (`internal/agent/rebind.go:160`), after the binding commits, store it on a new private
  Agent field `a.effortDialect` (add the field in `internal/agent/agent.go` beside the
  other session state).
- `internal/agent/wire.go`: set `EffortDialect: a.effortDialect` on the built
  `provider.Request` (`internal/agent/wire.go:44`), and widen `toProviderEffort`
  (`internal/agent/wire.go:69`) to map all seven domain levels (add
  none/minimal/xhigh/max), staying total (`""` for anything else).
- TUI seam: extend `tui.RebindHost.Rebind` (`internal/tui/tui.go:277`) to
  `Rebind(model string, contextWindow int, effortDialect provider.EffortDialect)`; carry
  the dialect on `rebindIntent` (`internal/tui/heartbeat.go:71`), set it in `observeBinding`
  from `beat.EffortSupport.Dialect`, and pass it through `applyRebind`
  (`internal/tui/heartbeat.go:303`) and the `picker.go:746` call site.
- Composition root: `cmd/apogee/wire_server.go:199` `serverHost.Rebind` and
  `cmd/apogee/wire_verbs.go` `rebind` accept and forward the dialect onto the `RebindSpec`.

The dialect is the ONLY effort fact that crosses into the engine; the reported level set and
default stay host-side (item 7). Keep every existing caller compiling — a zero
`EffortDialect` reproduces today's wire (item 4 guaranteed this).

Restated standards: this is the established server-fact channel (`MaxOutputTokens`'s path);
do not invent a parallel setter. The dialect is a value carried on the spec, not new engine
control flow. Deviations land as dated NOTES lines under this item.

**Files:** `internal/agent/rebind.go`, `internal/agent/wire.go`, `internal/agent/agent.go`,
`internal/tui/tui.go`, `internal/tui/heartbeat.go`, `internal/tui/picker.go`,
`cmd/apogee/wire_server.go`, `cmd/apogee/wire_verbs.go`.

**Depends on items 4 and 5.**

**Tests:** an agent test asserting a `RebindSpec` with `EffortDialectReasoning` makes the
next request carry `EffortDialect: EffortDialectReasoning` (extend the existing effort/rebind
tests, e.g. near `internal/agent/agent_test.go:88`). A `toProviderEffort` case for each new
level.

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/tui/ ./cmd/apogee/`.

**Commit:** `feat(effort): plumb the per-server effort dialect into the request`

---

## 7. Fold detected effort support into the TUI heartbeat state

**What:** Store the detected support host-side so the menu, footer, picker and clear-on-switch
can read it. Add an `effort provider.EffortSupport` field to `heartbeatState`
(`internal/tui/heartbeat.go:24`) and set it in `foldBeat`
(`internal/tui/heartbeat.go:217`) from `beat.EffortSupport`, beside the `m.hb.models` line
(`internal/tui/heartbeat.go:233`). Add a small read accessor `func (m Model) effortSupport()
provider.EffortSupport` (returning `m.hb.effort`) so later items read one seam rather than
the field directly. No visible behaviour yet — this item only makes the fact available.

Restated standards: `heartbeatState` is value-copied with the Model (ADR 0011) — the new
field is a plain struct/slice, never a builder or self-pointer type; `EffortSupport.Efforts`
is a slice, which is fine to hold by value.

**Files:** `internal/tui/heartbeat.go`, `internal/tui/heartbeat_test.go`.

**Depends on item 5.**

**Tests:** extend a `foldBeat` test to assert a beat's `EffortSupport` lands in `m.hb.effort`
and is returned by `effortSupport()`.

**Acceptance:** `go test ./internal/tui/ -run 'Beat|Effort|Heartbeat'`.

**Commit:** `feat(effort): fold detected effort support into heartbeat state`

---

## 8. Add the effort segment to the footer

**What:** Insert an effort word into the footer run, between the upstream (model) segment and
the workdir, so the line reads `host ✦ model ✦ <effort> ✦ ~/repo`. In
`internal/tui/model.go` `footerContent` (`internal/tui/model.go:2578`), build the segment
from a new pure helper and splice it into the `segments`/`info` join only when the dial is
supported (`m.effortSupport().Supported`). The word is resolved override ▸ profile ▸ reported
default ▸ `auto`:
- override and profile from `m.eng.ThinkingEffort()` (returns override, profile);
- reported default from `m.effortSupport().Default`;
- `auto` when none of the above says anything (a `/props` hit with no default).

Put the pure resolver in `internal/tui/effort.go` (e.g. `func footerEffortLabel(override,
profile domain.ThinkingEffort, reportedDefault string, supported bool) (string, bool)`
returning the word and whether to show it) so it is table-testable without a Model, matching
`effortResolutionNote`'s existing pure style there. The segment is dropped whole (like the
mode marker) when the footer is too narrow — reuse the existing `nonEmpty` join so an empty
word disappears with its separator.

Restated standards: keep the footer's "reads outward-in" ordering intact — effort sits with
the upstream facts (it is a property of how the model answers), before the local workdir. One
new pure helper; no new state.

**Files:** `internal/tui/model.go`, `internal/tui/effort.go`, `internal/tui/effort_test.go`.

**Depends on item 7.**

**Tests:** `effort_test.go` table over `footerEffortLabel`: override wins; profile when no
override; reported default when neither; `auto` when supported-but-nothing; `("",false)`
when unsupported. One `footerContent` assertion that a supported binding shows the word
between model and workdir and an unsupported one omits it.

**Acceptance:** `go test ./internal/tui/ -run 'Footer|Effort'`.

**Commit:** `feat(effort): show the resolved effort as a footer segment`

---

## 9. Hide /effort from the command menu when the dial is unsupported

**What:** Drop the `/effort` row from the autocomplete dropdown when the bound model reports
no dial, while leaving the typed verb fully routable. In
`internal/tui/autocomplete.go` `commandSuggestions` (`internal/tui/autocomplete.go:421`),
skip the `effort` spec when a new predicate says it is hidden this moment. Thread the
predicate from the Model (which holds `m.effortSupport()`): add a `hidden bool` decision at
the call site rather than a new table column, since availability is a property of the moment
(the `busy`/`idleOnlyTag` precedent right there). A clean way: mark the spec with a
`hideWhen` capability in `commandSpec` (`internal/tui/command.go`) — a small enum/bool like
`gatedByEffort` — and have `commandSuggestions` take the current support and skip gated rows
when unsupported. Do NOT touch `parseInput`/`matchCommand` — a hand-typed `/effort` must
still parse and route so item 10's "typed verb still answers" holds.

Restated standards: the gate is menu-presentation only; the registry and parser stay
complete. One property on the spec + one check at the suggestion seam — no second table.

**Files:** `internal/tui/autocomplete.go`, `internal/tui/command.go`,
`internal/tui/autocomplete_test.go` (or `command_test.go`).

**Depends on item 7.**

**Tests:** a `commandSuggestions` case: with unsupported effort, `/eff` yields no `/effort`
row; with supported effort it does; a hand-typed `/effort` still classifies as the command
regardless (parser unchanged).

**Acceptance:** `go test ./internal/tui/ -run 'Suggest|Command|Menu'`.

**Commit:** `feat(effort): hide /effort from the menu when the model has no dial`

---

## 10. Replace the /effort text grammar with a popup picker

**What:** Turn `/effort` into a fixed-choice popup, removing the level word grammar.
- `internal/tui/picker.go`: add `pickerEffort` to the `pickerKind` enum
  (`internal/tui/picker.go:74`); give it rows = the model's reported `Efforts` (from
  `m.effortSupport()`) when non-empty, else a **dialect-aware** canonical fallback — the
  OpenAI dialect (`EffortDialectOpenAI`) lists `minimal/low/medium/high` (OpenAI has no
  "off"), every other supported dialect lists `off/low/medium/high` — with an `auto` row
  appended (clears the override); wire its title, its rows, and its accept in
  the three `switch` sites (`internal/tui/picker.go:681`, `:836`, `:869`) and a
  `pickerHintFor` arm (`internal/tui/picker.go:135`) reading "⏎ choose" like `pickerCycle`.
  The accept calls `m.eng.SetEffortOverride(level)` (or clears on `auto`) and ends on the
  existing `effortResolutionNote` (reuse `effort.go`).
- `internal/tui/command.go`: change the `effort` `commandSpec` (`internal/tui/command.go:223`)
  to open the picker rather than parse args — drop `takesArgs`/`parseArgs`, keep
  `whileRunning`; make it open-at-accept like `/schedule` opens `pickerCycle`
  (`internal/tui/schedule.go:119`). Remove `effortArgs`, `effortAction`, `effortUsage`,
  `parseEffort` and their doc block (`internal/tui/command.go:555`–`596`).
- `internal/tui/commandrun.go`: the `case "effort"` (`internal/tui/commandrun.go:388`) opens
  `pickerEffort` when the dial is supported; when unsupported (the hand-typed path) it adds
  the single note "this model reports no thinking-effort dial" and does nothing else.
- `internal/tui/effort.go`: `runEffort` becomes the picker-accept handler (takes the chosen
  level / auto), keeping `effortResolutionNote` and its pure helpers.

Restated standards: model the picker exactly on `pickerCycle`/`pickerScheduleMode` (a
question that moves session state at accept, not a switch) — same value-copied inline state,
no callback field (ADR 0011). Delete the text grammar fully; a half-removed parser is worse
than either end state.

**Files:** `internal/tui/picker.go`, `internal/tui/command.go`,
`internal/tui/commandrun.go`, `internal/tui/effort.go`, `internal/tui/picker_test.go`,
`internal/tui/command_test.go`.

**Depends on item 7.**

**Tests:** replace `TestParseEffortVocabulary` (`internal/tui/command_test.go:100`) — the
grammar is gone — with a picker test: `pickerEffort` rows equal the reported set + `auto`
when a set is reported, and the canonical four + `auto` otherwise; accepting a level asserts
`SetEffortOverride` was called with it and the resolution note is added; accepting `auto`
clears it. Assert `/effort` on an unsupported model adds the no-dial note and opens no picker.

**Acceptance:** `go test ./internal/tui/ -run 'Effort|Picker'`.

**Commit:** `feat(effort): make /effort a popup picker of the model's levels`

---

## 11. Clear the effort override on a switch to a model that excludes it

**What:** When a rebind binds a model whose reported `supported_efforts` set does not contain
the live session override, clear the override and add a transcript note. In
`internal/tui/heartbeat.go` `applyRebind` (`internal/tui/heartbeat.go:303`) — the one seam
every model switch funnels through, whether beat-observed or user-picked — after the binding
is applied, read the override via `m.eng.ThinkingEffort()` and the newly bound model's set
via the beat's `EffortSupport.Efforts`: when the override is non-empty AND the set is
non-empty AND the override is not in it, call `m.eng.SetEffortOverride("")` and append a note
like `effort override "<level>" is not offered by <model> — cleared; back to auto`. A model
that reports no set (empty `Efforts`, e.g. a `/props` hit) leaves the override untouched —
the enriched turn error stays the backstop. Guard against noise: emit the note only when a
clear actually happened.

Restated standards: this is a host-side policy reading two facts the host already holds (the
override from the engine, the set from the beat) — the engine never learns the set. Keep the
note wording a plain fact, no verdict, matching the transcript's other rebind notes.

**Files:** `internal/tui/heartbeat.go`, `internal/tui/heartbeat_test.go`.

**Depends on items 7 and 10.**

**Tests:** a switch where the override is outside the new model's reported set clears it and
adds the note; a switch to a model reporting no set keeps the override and adds no note; a
switch where the override is in the set keeps it silently.

**Acceptance:** `go test ./internal/tui/ -run 'Rebind|Effort|Heartbeat'`.

**Commit:** `feat(effort): clear the effort override when a switched-to model excludes it`

---

## 12. Per-server `effort-dialect:` config override for detection-blind providers

**What:** Let a `servers:` entry force the effort dialect when passive detection cannot see it
(OpenAI, Groq, vLLM/SGLang/TGI). The forced dialect overrides detection for BOTH the wire and
the UI, flowing through the same Beat→`EffortSupport` path so no downstream item changes.

- `internal/config/config.go`: add `EffortDialect string `yaml:"effort-dialect,omitempty"`` to
  `ServerEntry` (`internal/config/config.go:1259`). Validate it (in `ValidateServers` or a
  sibling) against the set `{"", "auto", "kwargs", "reasoning", "openai", "off"}` — a bad
  value is a startup error naming the entry and the key, the `validateThinkingAxes` posture.
  `""` and `auto` both mean "detect".
- `internal/provider/client.go`: add a `WithEffortDialect(EffortDialect)` option
  (`internal/provider/client.go:111` neighbourhood) storing a forced dialect on the `Client`
  (write-once at construction, no lock needed — unlike `model` it never changes after build).
- `internal/provider/discovery.go`: in `Discover` (`internal/provider/discovery.go:90`), after
  the detection synthesis from item 3, apply the forced dialect when one is set: `off` ⇒
  `EffortSupport{Supported:false, Dialect:EffortDialectNone}` (never send — the escape hatch);
  `kwargs`/`reasoning`/`openai` ⇒ `EffortSupport{Supported:true, Dialect:<mapped>, Efforts:nil
  or the detected set if the same dialect was also detected, Default: detected default when the
  dialects match else ""}`. `auto`/"" ⇒ leave the detected value untouched. A map from the
  config string to `EffortDialect` lives here (one small total helper).
- `cmd/apogee`: where the per-server `provider.Client`/`heartbeat.Monitor` are built for a
  session (`cmd/apogee/upstream.go` `Bind`, and the `NewMonitor` call), pass
  `WithEffortDialect` resolved from the bound `ServerEntry.EffortDialect`. `NewMonitor`
  (`internal/heartbeat/heartbeat.go:100`) gains a variadic `provider.Option` pass-through (or
  an explicit dialect parameter) so the Monitor's discovery client carries the forced dialect —
  this is the client whose `Discover` feeds the Beat, so the forced dialect reaches
  `EffortSupport` and rides to the TUI and (via item 6) to the completion request.

Restated standards: the config key is the sanctioned fallback for providers with no passive
tell (ADR 0050 D2), not a per-family table — it names a *dialect*, not a model family. The
forced value flows through the existing `EffortSupport`/Beat channel; do not add a second path
into the engine. Keep detection (item 3) as the default — the override only fires when the key
is set to a non-`auto` value.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`,
`internal/provider/client.go`, `internal/provider/discovery.go`,
`internal/provider/discovery_test.go`, `internal/heartbeat/heartbeat.go`,
`cmd/apogee/upstream.go`.

**Depends on items 3, 4 and 5.**

**Tests:** config: `effort-dialect: openai` loads, `effort-dialect: bogus` fails with a
message naming the entry and key. provider: `Discover` with a forced `openai` dialect over a
server that detected nothing yields `Supported:true, Dialect:EffortDialectOpenAI`; forced
`off` over a server that DID detect a dial yields `Supported:false`; `auto` leaves detection
untouched.

**Acceptance:** `go build ./... && go test ./internal/config/ ./internal/provider/ ./internal/heartbeat/`.

**Commit:** `feat(effort): add the per-server effort-dialect config override`

---

**Suggested version bump (not performed):** this ships a user-facing feature (footer readout,
picker, cloud-endpoint effort). A `VERSION` micro-bump is warranted at closeout — the owner
decides whether and when. No item changes `VERSION` or `CHANGELOG` release headings.

**Deferred (record at closeout):** the out-of-band title/naming call keeps the
`chat_template_kwargs` `EffortOff` mapping and does not adopt the OpenRouter `reasoning`
dialect, so on an OpenRouter thinking model a generated title may reason to the token cap
(`title.ErrTruncated`). Pre-existing edge, left out of this plan's scope.
