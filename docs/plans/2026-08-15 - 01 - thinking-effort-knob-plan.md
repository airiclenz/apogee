# Plan: profile-level thinking-effort knob

**Goal:** a per-model-profile `thinking.effort` dial (`off|low|medium|high`) forwarded to the
server's chat template as `chat_template_kwargs`, a `/effort` session override layered above it,
and the retirement of `Request.DisableThinking` — with the wire anchor intact: absent means send
nothing, and an out-of-the-box request stays byte-identical.

**Date:** 2026-08-15
**Status:** not started
**Sized for:** ~200k-context host

**Authoritative sources:**
- `docs/adr/0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md` — the
  ratified design; if an item's text disagrees with it, ADR 0050 wins.
- `docs/adr/0044-model-profiles-are-per-model-and-mostly-shipped.md` — profile resolution rules.
- `docs/handoffs/2026-08-15 - 00 - reasoning-effort-knob.md` — live `/apply-template` evidence
  (Qwen3.8 kwarg behaviour, the HTTP-500 failure shape).
- `CONTEXT.md` "Thinking effort" — the glossary term (already written).

**Ratified design calls** (owner, via AskUserQuestion, 2026-08-15 grill):
1. Single axis `thinking: {effort: off|low|medium|high}` inside the profile's thinking block;
   zero value = send nothing.
2. One canonical wire mapping owned by the provider Client — `off` → `{"enable_thinking": false}`,
   else `{"reasoning_effort": "<level>"}` (high emits `"high"`, verified accepted). No per-family
   table until a second family is live-verified.
3. `Request.DisableThinking` is DELETED; semantic `ThinkingEffort` replaces it; the title namer
   sets `off` (byte-identical output).
4. Validation = load-time enum + enriched turn error; no `/apply-template` probe.
5. `/effort` (NOT `/thinking` — reserved for the deferred display feature): session override,
   resolution `override ▸ profile ▸ nothing`, survives model switches, never persisted, primary
   loop only.
6. Engine stance: configuration not Mechanism (holds under `--bypass`); override settable via an
   engine door; the shipped table never carries an effort.

**Standing requirements:** skills: coding-standards.

**In-flight caution:** plan `2026-08-15 - 00 - stall-guard-plan.md` may still be executing and
touches `internal/tui/`. Only item 5 here shares that tree — its implementer works against
whatever has landed on `main` at dispatch time.

**Out of scope:** a gpt-oss shipped effort entry (kwarg unverified — per-sighting rule); a lower
default effort for sub-agents; visible rendering of thinking tokens; `logit_bias` on `</think>`;
llama.cpp `--reasoning-budget`; persisting `/effort` to config; any settings-TUI row for
`model-profiles:`.

## 1. Domain effort type and config-load validation — ✅ DONE (2026-08-15)

NOTES (2026-08-15): `internal/config/defaults_test.go` edited beyond the item's Files list — one
string added to the existing `TestEmbeddedDefaultConfigDocumentsModelProfiles` want-list so the
template's new `effort:` example is guarded like the `style:` one beside it.
NOTES (2026-08-15): `ThinkingEffort.Valid()` accepts the zero value as well as the four levels
(absence is a legitimate configuration, not a defect); the loader therefore needs no separate
empty-string check.
NOTES (2026-08-15): validation runs in `LoadFileConfig` via a new `validateModelProfiles` helper
rather than inside `fileConfig.layer()`, which has no error return; patterns are walked in sorted
order so a file with two bad entries reports the same one every run.

**What:** Add the effort leaf to the domain profile and the YAML schema, with load-time enum
validation.
- `internal/domain/config.go`: new `type ThinkingEffort string` with constants `EffortOff`
  ("off"), `EffortLow` ("low"), `EffortMedium` ("medium"), `EffortHigh` ("high"); zero `""`
  means unset/send-nothing. `ThinkingProfile` gains `Effort ThinkingEffort` with a doc comment
  stating the wire anchor (absent ⇒ nothing emitted) and citing ADR 0050. Add a
  `ThinkingEffort.Valid()` (or equivalent) helper the config loader uses.
- `internal/config/config.go`: the `model-profiles:` thinking block (schema around the existing
  `style/start/end` parsing, see lines ~933–940 and the profile translation ~1611) gains
  `effort:`; an out-of-vocabulary value is a LOAD ERROR whose message names the offending
  pattern key and lists the four valid values (match the loader's existing enum-error style).
- `internal/config/defaults/config.yaml`: extend the commented `model-profiles:` example
  (~line 698) with an `effort:` line and a one-line explanation (absent = model's own default).
- The shipped table (`internal/profiles/shipped.go`) is NOT touched — no shipped entry carries
  an effort (design call 6).

**Files:** `internal/domain/config.go`, `internal/domain/config_test.go`,
`internal/config/config.go`, `internal/config/config_test.go`,
`internal/config/defaults/config.yaml`

**Tests:** parse a profile entry with `effort: low` into the domain type; absent key yields zero
value; `effort: hihg` fails load with the enum message; existing profile round-trip tests stay
green.

**Acceptance:** `go build ./... && go test ./internal/domain/... ./internal/config/...`

**Commit:** `feat(config): model-profile thinking gains a validated effort leaf`

## 2. Provider seam: ThinkingEffort replaces DisableThinking — ✅ DONE (2026-08-15)

NOTES (2026-08-15): `cmd/apogee/title.go` edited beyond the item's Files list — the naming call's
"rejected outright, re-send without the kwarg" fallback read and cleared `req.DisableThinking`, so
deleting the field breaks the build there; both sites now test/clear `req.ThinkingEffort` (`!= ""`
rather than the removed bool), preserving the behaviour exactly.
NOTES (2026-08-15): the "title-namer test asserts the `enable_thinking:false` bytes" requirement is
met across the two packages rather than in one test — `internal/title` asserts the Request carries
`provider.EffortOff` (it builds a Request and never touches HTTP), and the provider wire-bytes test
for `EffortOff` asserts exactly `{"enable_thinking": false}` reaches the body.

**What:** One semantic field, one canonical mapping (design calls 2–3).
- `internal/provider/wire.go`: DELETE `DisableThinking`. Add a provider-local
  `type Effort string` (constants mirroring off/low/medium/high; the provider package keeps its
  no-domain-import stance — the agent maps at the boundary, like `toProviderSampling`) and
  `ThinkingEffort Effort` on `Request`, doc comment: `""` ⇒ nothing on the wire (the
  byte-identical anchor), `off` ⇒ no chain-of-thought, levels ⇒ template effort dial.
- `internal/provider/client.go`: replace the `req.DisableThinking` block (~line 410) with the
  mapping — `off` → `{"enable_thinking": false}`; `low|medium|high` →
  `{"reasoning_effort": "<level>"}` verbatim. Unknown non-empty values emit nothing (the config
  enum already rejects them; the Client stays total).
- `internal/provider/wirejson.go`: update the `ChatTemplateKwargs` comment (no longer "only to
  switch thinking off").
- `internal/title/title.go` (~line 166): set `ThinkingEffort: provider.EffortOff` instead of
  `DisableThinking: true`.

**Files:** `internal/provider/wire.go`, `internal/provider/client.go`,
`internal/provider/wirejson.go`, `internal/provider/client_test.go`, `internal/title/title.go`,
`internal/title/title_test.go`

**Tests:** wire-bytes tests: `off` produces exactly the bytes `DisableThinking` produced before
(adapt the existing DisableThinking wire test); each level produces its `reasoning_effort`
kwarg; empty produces no `chat_template_kwargs` key at all; the title-namer test asserts the
`enable_thinking:false` bytes still go out.

**Acceptance:** `go build ./... && go test ./internal/provider/... ./internal/title/...`

**Commit:** `feat(provider): semantic ThinkingEffort replaces DisableThinking on the request seam`

## 3. Engine plumbing: profile effort on the wire, override door — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the item asks for the child-isolation assertion in "the existing child-construction test", but this package has no single such test — `newChildAgent` is driven from `routedspawn_test.go`, `delegationtarget_test.go` and `contextfiles_test.go`, each pinning its own concern; the assertion is therefore a dedicated test in `internal/agent/agent_test.go`, a file the item's Files list already names and which did not exist before this item.
NOTES (2026-08-15): the override needed no code in `newChildAgent` — it lives on the Agent rather than on Config, so a child never sees it by construction; the new test is what makes a future move of it onto Config fail loudly.

Depends on item 1. Depends on item 2.

**What:** The loop's requests carry the resolved effort; a session override layers above the
profile (design calls 5–6).
- `internal/agent/agent.go`: new unexported field (e.g. `effortOverride domain.ThinkingEffort`)
  plus the engine door `SetEffortOverride(domain.ThinkingEffort)` — zero value clears; a getter
  for the TUI's bare `/effort` display (return override and the profile's effort so the caller
  can show the layering). Doc comment: session intent, never persisted, holds under Bypass
  (configuration, not a Mechanism — ADR 0046 precedent), Driver-drivable (ADR 0031).
- `internal/agent/wire.go` (`buildRequest`, ~line 40): resolve
  `override ▸ a.cfg.Profile.Thinking.Effort ▸ ""` and map the domain value to the provider
  `Effort` at this boundary (same style as `toProviderSampling`).
- Child isolation: the override is parent-Agent state — `newChildAgent` must NOT copy it into a
  delegated child (design call 5: primary loop only; the child's own profile resolution stands).
  Add the assertion to the existing child-construction test.
- Rebind/SetProfile need no code change (the profile — effort included — already rides them);
  add one regression test proving a model switch re-resolves effort from the new profile while
  the override stays put.

**Files:** `internal/agent/agent.go`, `internal/agent/wire.go`, `internal/agent/agent_test.go`,
`internal/agent/wire_test.go`

**Tests:** request carries the profile effort when set and nothing when unset; override beats
profile; clearing the override falls back to the profile; override survives a rebind; a child
agent does not inherit the override; Bypass mode leaves the emitted effort unchanged.

**Acceptance:** `go build ./... && go test ./internal/agent/...`

**Commit:** `feat(agent): requests carry resolved thinking effort with a session override door`

## 4. Enriched turn error when kwargs are on the wire

Depends on item 2.

**What:** A template that rejects a value raises in Jinja and the server answers HTTP 500
mid-turn (design call 4). In `internal/provider/client.go`'s non-2xx error path, when the failed
request carried `chat_template_kwargs`, append a hint to the returned error naming the likely
culprit — binding wording:
`(this request carried chat_template_kwargs — an unsupported thinking effort for this model's template? check model-profiles thinking.effort or the /effort override)`.
The hint is provider-side and mentions both doors; no behaviour change on requests without
kwargs.

**Files:** `internal/provider/client.go`, `internal/provider/client_test.go`

**Tests:** a stubbed 500 on a request with kwargs yields an error containing the hint; the same
500 without kwargs yields today's error unchanged.

**Acceptance:** `go test ./internal/provider/...`

**Commit:** `feat(provider): non-2xx errors hint at thinking effort when kwargs were sent`

## 5. /effort command in the TUI

Depends on item 3.

**What:** The session command (design call 5).
- `internal/tui/command.go`: table entry
  `{name: "effort", summary: "set how hard the model thinks — off, low, medium, high, or auto", takesArgs: true, whileRunning: true}`
  (whileRunning: the override applies from the next request, so setting it mid-run is safe and
  useful). Arg parsing accepts exactly `off|low|medium|high|auto`; anything else is a usage
  error echoing the vocabulary.
- Command run wiring (`internal/tui/commandrun.go` / `internal/tui/tui.go`, matching how other
  engine-door commands dispatch): a level calls `SetEffortOverride(level)`; `auto` calls it with
  the zero value; bare `/effort` prints the current resolution using the item-3 getter —
  binding format: `thinking effort: <effective> (session override: <level or —>; profile:
  <level or —>)`.
- Autocomplete/help surfaces follow automatically from the table entry; touch nothing else.

**Files:** `internal/tui/command.go`, `internal/tui/command_test.go`,
`internal/tui/commandrun.go`, `internal/tui/tui.go`

**Tests:** table-driven parse tests for the five arguments + a bad one; a run test asserting the
engine door is called with the mapped value and that bare `/effort` renders the binding format.

**Acceptance:** `go build ./... && go test ./internal/tui/...`

**Commit:** `feat(tui): /effort sets the session thinking-effort override`

## 6. README documentation

Depends on item 5.

**What:** `README.md`: add the `/effort` row to the slash-command table (~line 258, whileRunning
column ✅) and a short paragraph in the model-profiles section documenting `thinking.effort:`
(vocabulary, absent = the model's own default, the `/effort` override, the Qwen3.8 xhigh-default
motivation in one sentence). Keep to the README's existing voice; CONTEXT.md and ADR 0050 are
already written and are NOT touched by this plan.

**Files:** `README.md`

**Tests:** none (prose).

**Acceptance:** `grep -n "/effort" README.md` shows the table row and the profile paragraph.

**Commit:** `docs(readme): document thinking.effort and the /effort override`

## Suggested version bump

A user-visible feature (config key + command + wire behaviour): suggest a micro bump per house
policy once the plan lands. Not performed by this plan — owner's call.
