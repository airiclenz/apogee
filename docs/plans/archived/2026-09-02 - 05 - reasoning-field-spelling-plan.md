# The thinking channel's second wire spelling is never decoded

**Goal:** apogee decodes only `reasoning_content`, but Ollama and OpenRouter both spell the same
channel `reasoning` — so against either server the model's thinking streams into a field nothing
reads: `/thinking` shows `no thinking recorded yet`, `/inspect`'s readable view classifies nothing
as thinking, and `stubllm record` drops the channel. Ollama makes this a LOCAL-first defect, not a
gateway quirk. Teach the three decode sites `reasoning` as an alias, and prove the journey with a
stub that can emit either.

**Date:** 2026-09-02 · **Status:** unexecuted · **sized for:** ~200k-context host

**Sources:**
- Base commit `24e95154` — every line number below was read against it.
- Live wire evidence, all four probed 2026-09-02 against the owner's own servers:
  | Server | Streaming delta | Non-streamed message |
  | --- | --- | --- |
  | llama.cpp (`Gemma 4 26B`) | `reasoning_content` (×77); bare `reasoning` ×0 | `reasoning_content` |
  | LM Studio (`gemma-4-e2b-it`) | no channel emitted (this model did not reason) | `reasoning_content`, present but EMPTY |
  | Ollama (`gemma4:E2B`) | `reasoning` (×85); `reasoning_content` ×0 | `reasoning` |
  | OpenRouter (`deepseek/deepseek-v4-flash`, SiliconFlow) | `reasoning` + `reasoning_details` | (same family) |
  OpenRouter's terminal chunk sends `"reasoning":null`; Ollama sends no `reasoning_details` at all.
  LM Studio's `reasoning_content` key is ALWAYS present and empty when the model does not reason —
  the limit of that probe is that no LM Studio model on hand produced reasoning, so the key is
  evidence of the spelling, not of a populated payload. It is also why the ratified precedence tests
  the value for non-emptiness and never the key for presence: a key-presence test would let LM
  Studio's permanent empty key block the fallback outright.
  llama.cpp and LM Studio are the only two of the four that work today, and both stay on the winning
  branch of the precedence — this plan adds an alias and changes nothing for them, for vLLM, or for
  any other `reasoning_content` server.
- `CONTEXT.md` "Thinking channel" · ADR 0010 (wire types stay provider-local) · `docs/design/test-drivers.md`

**Ratified design calls** (owner, 2026-09-02):
- **Precedence:** `reasoning_content` wins per chunk; `reasoning` is read only where it is empty.
- **`reasoning_details`:** ignored entirely — a redundant echo; encrypted blocks carry nothing displayable.
- **Test depth:** stubllm gains a turn-script knob for the spelling, and an e2e test drives `/thinking`.
- **Gating:** unconditional — an alias of one channel, no config key, no model profile. No server
  detection: apogee reads whichever field arrives, so one binary serves llama.cpp, Ollama, vLLM,
  OpenRouter and a mixed roster without being told which is which.
- **Extensibility:** the precedence lives in ONE helper per decode site, so a future third spelling
  is a one-line addition there and nowhere else. Never open-code the fallback at a call site.

**Regression check (2026-09-02, `24e95154`):** the base was corrected from `27a0dc83` after the
re-check round found the line drift — every line number in this plan was read against `24e95154`
(`27a0dc83` is three commits earlier, where e.g. `internal/agent/loop.go:606/664/839` read 604/662/837).
- Item 1: recast — the precedence helper cannot hang on the anonymous delta/message structs, and a
  string-typed `reasoning` would make a non-string value drop the whole chunk; guard folded (named
  types plus `json.RawMessage`), and the comment-site list replaced by its grep rule.
- Item 2: recast — the same two corrections applied to `/inspect`'s mirrored delta; guard folded.
- Item 3: guard folded — stubllm deliberately keeps `Reasoning string`; the acceptance `Stub` half
  matched no test (widened to `-run 'TestE2E'`) and the doc edit is stated as a rule, not a list.
- Re-check round, item 1: guard folded — the stale `Reasoning string` in **What** replaced by
  `json.RawMessage` per the writer's decision; the extracted response type renamed
  `chatResponseMessage` (`chatMessage` is already the request-side message at
  `internal/provider/wirejson.go:69`, so reusing it is a redeclaration compile error); the three
  further comment-rule files added to **Files:**.
- Re-check round, item 2: guard folded — the same `json.RawMessage` correction on the mirrored
  delta, and the defect restated: a `reasoning`-spelled delta renders as nothing at all today
  (`extend` gets two empty strings), never as a `text` passage; the first test's bite corrected.
- Item 4: recast — it was ADDED after items 1-3 were checked, was reviewed on its own, and came
  back REGRESSION with all five findings accepted. Follow is armed and honoured only for
  `inspectReport`/`thinkingReport` (`/usage` is NOT static — `usage.go:184-207` appends a delegate
  row per run plus a session-total row — and keeps its clamp-only behaviour); the verbs ADD the flag
  and KEEP `top: len(rows)`; the growth tests open through the VERB and append more than a full
  window (as written they had no bite); `reportWheel` re-derives follow ONCE after its switch, not
  inside its two guarded branches; and the comment fix is a grep rule reaching `model.go`,
  `doc.go`, `usage.go` and `layout.md`. The `D:` line is accepted with it — `reportpane.go:178-182`
  credits the clamp with landing an opening `/inspect` on the newest record, which after the item is
  `follow`'s doing, so that comment is corrected, not left to contradict the code.
- Re-check round, item 4: GUARD, all three guards folded — the recast item was re-reviewed on its
  own and came back with NO regressions: the kind gate is complete and cannot half-gate `/usage`
  (`reportSpec`, `reportKey` and `reportWheel` all take `r`, the verbs and `usage.go:101` cover the
  rest, and gating either arming or honouring alone already leaves `/usage`'s clamp behaviour
  byte-identical), and both growth tests do bite. Folded: the verb sites keep their own names
  (`inspectorPane{…}` at `inspector.go:162`, `reportPane{…}` at `thinkingpane.go:201`) — the item
  YIELDS to `inspector.go:87-90` and `usage.go:39-41`, which record the per-pane alias as the
  intended spelling there; `reportWheel` re-derives follow from the `win` it already holds rather
  than recomposing the pane on every notch; and the comment rule's pattern is widened to
  `reportPane\|usagePane\|inspectorPane\|last full window\|clamp every report`, the narrow one
  having missed `model.go:181`/`:187` and `layout.md:1819` (a passage untrue of a following pane
  after a shrink) entirely. Two premises corrected with them: the transcript writes `detached` at
  twelve sites, not one funnel — the panes mirror the INVARIANT, not a writer — and the `/inspect`
  growth case must stay under the wire ring's 20-record cap while appending, or rotation zeroes the
  net growth and the test loses its bite (the `/thinking` case has room under its 64-record cap).
- Tree drift at that check: the working tree was at `f3dd29e0`, three commits past the pinned base
  `24e95154`; `reportpane.go`, `thinkingpane.go` and `inspector.go` are byte-identical there, but
  `inspector_test.go` has moved +7 lines. The base stays PINNED at `24e95154` — it is not re-pinned
  to a moving HEAD — so test-file line citations may have drifted and are to be re-derived at
  execute time.

**Standing requirements:**
- `skills: coding-standards`
- No AI attribution trailers; commit directly to `main`.

**Out of scope:** `reasoning_details` decoding · the request-side effort dialect
(`EffortDialectReasoning`, already shipped) · `~/.apogee/config.yaml` (fixed by hand, 2026-09-02) ·
any change to how thinking is retained, stripped or sent back Upstream.

## 1. Decode `reasoning` alongside `reasoning_content` in the provider — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the precedence helper is written ONCE, on an unexported `reasoningChannel`
(wirejson.go) carrying both wire fields and `thinking()`, which the two named types
(`sseDelta`, `chatResponseMessage`) EMBED — the item's "not a free function, and not duplicated
logic" admits no second copy of the body, and embedding promotes both the json fields and the
method to each named type.
NOTES (2026-09-02): the comment rule reached eleven sites; `internal/processing/parserfor.go:17`,
`internal/agent/loop.go:839`, `internal/domain/events.go:83`, `internal/tui/thinking.go:194` and
`internal/domain/hooks.go` were left untouched — each names the history-`Extra` KEY, not the wire
spelling. `internal/tui/inspector.go:289` and `internal/stubllm/` belong to items 2 and 3.

**What.** Recast at the regression check (2026-09-02). The fix.
`internal/provider/stream.go:387` (`sseChunk`'s delta) and
`internal/provider/wirejson.go:100` (`chatCompletionResponse`'s message) each gain
`Reasoning json.RawMessage \`json:"reasoning"\`` beside the existing `ReasoningContent`. The
extraction the guard prescribes carries it: the named unexported types `sseDelta` in stream.go and
`chatResponseMessage` in wirejson.go, each carrying an identical `thinking() string` — not a free
function, and not duplicated logic — returning the chunk's reasoning under the ratified precedence:
`ReasoningContent` when non-empty, else the unquoted `Reasoning`.
Both call sites use it: `stream.go:213`'s `textBytes` accounting must count the *chosen* field
(counting both would cap a duplicating proxy's stream at half the real limit), and `stream.go:228`
yields `DeltaThinking` off it. `wirejson.go:202`'s `out.Thinking` likewise.

A terminal chunk carries `"reasoning":null`, which as `json.RawMessage` decodes to the four bytes
`null` rather than to `""` — so it is `thinking()` that must return "" for it, and the pre-existing
`!= ""` guard at the yield site then suppresses the delta; a test pins that no thinking delta is
emitted. Comments that name the wire field as
the channel's only spelling are corrected: `stream.go:22` (`DeltaThinking`), `wire.go:166`
(`RawResponse`), `wirejson.go:93`, and `CONTEXT.md:473`'s "Thinking channel" definition.

**Regression guard.** Name the types before adding the field — a helper cannot hang on the structs
as they stand, because `sseChunk`'s `choices[].delta` and `chatCompletionResponse`'s
`choices[].message` are ANONYMOUS inline struct types. Extract each into a named unexported type in
its own file (`sseDelta` in stream.go, `chatResponseMessage` in wirejson.go — NOT `chatMessage`,
already declared as the request-side message at `internal/provider/wirejson.go:69`, so reusing that
name is a redeclaration compile error), leaving every field and json
tag byte-identical, then hang the precedence helper `thinking() string` on each named type. Decode
the new field as `Reasoning json.RawMessage` and NOT as a string: `stream.go:191` drops a whole
chunk on any json.Unmarshal error, so a server sending a non-string `reasoning` (an object, as
OpenRouter's own /models endpoint already does — see the existing `json.RawMessage` at
discovery.go:389) would silently lose that chunk's CONTENT as well as its reasoning. `thinking()`
returns `ReasoningContent` when non-empty; otherwise it unquotes `Reasoning` as a JSON string and
returns "" for `null`, for absent, and for any non-string shape. Both named types use the identical
helper body.

The comment fix is a rule, not the four-site list: correct every comment naming `reasoning_content`
as the WIRE spelling of the thinking channel — `grep -rn reasoning_content --include=*.go` — which
also reaches `internal/processing/thinking.go:11` and `internal/agent/loop.go:606` and `:664`, and
leave the history-Extra key prose (`internal/agent/loop.go:839`, `internal/domain/hooks.go`) untouched.

**Files:** `internal/provider/stream.go`, `internal/provider/wirejson.go`, `internal/provider/wire.go`,
`internal/processing/thinking.go`, `internal/agent/loop.go`, `internal/domain/events.go`,
`internal/domain/config.go`, `internal/probe/battery.go`,
`internal/provider/stream_test.go`, `internal/provider/client_test.go`, `CONTEXT.md`

**Tests.** In `stream_test.go`, modelled on `TestStream_RoundTrip` (line 55), a table over the
server matrix — `reasoning_content` only (llama.cpp, vLLM), `reasoning` only (Ollama, OpenRouter), and both
— asserting each yields the same joined `DeltaThinking` text, with the both-case yielding the
`reasoning_content` text once and only once. Plus: a chunk carrying an EMPTY `reasoning_content`
beside a populated `reasoning` yields the `reasoning` text (the LM Studio shape — presence must
never beat non-emptiness); a `"reasoning":null` chunk yields no thinking delta; a stream whose first chunk spells `reasoning_content` and whose later chunks spell
`reasoning` yields both, in order. Extend `TestStream_ThinkingCountsTowardTheCap` (line 828) over
the same matrix. A chunk whose `reasoning` is a JSON object (`{"effort":"high"}`) still yields its
`content` and no thinking delta — the chunk is decoded, never dropped. In `client_test.go`, mirror `TestRespond`'s `reasoning_content` assertion
(line 69) across the matrix for the non-streamed path, plus the same non-string `reasoning` case
on the whole reply.

**Acceptance.** `go build ./internal/provider/... && go test ./internal/provider/...`

**Regression guard.** This is an addition, not a replacement: `reasoning_content` remains the
winning spelling, so every server apogee works with today is untouched. Every existing
`reasoning_content` test must pass **unmodified** — an implementer who edits one to accommodate the
alias has changed behaviour and must stop. The precedence is per chunk, never per stream or per
connection: nothing may latch onto the first spelling it sees.

`fix(provider): decode the reasoning field as the thinking channel`

## 2. Teach /inspect's readable view the same spelling — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the mirror keeps the two channel fields directly on `sseDelta` (no embedded
struct) — the item prescribes "the same `thinking()` helper over a `json.RawMessage` field" on a
named `sseDelta`, and ADR 0010 keeps the provider's `reasoningChannel` provider-local, so shape and
precedence match without a second embedded type in package tui.

**What.** Recast at the regression check (2026-09-02). `internal/tui/inspector.go:285-297`
mirrors the provider's `sseChunk` (that type is unexported, so it is copied rather than imported — the comment at line 280 says so). The mirror
reads only `reasoning_content`, so an OpenRouter response's reasoning deltas render as nothing at
all: `wireResponsePassages` calls `extend` with two empty strings (`inspector.go:406-408`) and the
pane hides them — against its own contract (`inspector.go:305-308`) never to drop what a server
sent. Add `Reasoning json.RawMessage \`json:"reasoning"\`` to the mirrored delta, on a named
unexported `sseDelta` carrying the identical `thinking() string` helper item 1 prescribes,
and apply the same precedence item 1 ratifies, keeping the mirror's partial-by-design shape (still
no `reasoning_details`, still no `usage`). Extend the mirror's comment to say the two spellings are
one channel, so the next reader does not "simplify" one away.

Depends on item 1 only for the precedence wording; the code is independent.

**Regression guard.** Same two corrections as item 1, applied to the mirrored delta in
inspector.go: extract the anonymous `choices[].delta` into a named unexported `sseDelta` (package
tui) and give it the same `thinking() string` helper over a `json.RawMessage` field. The mirror must
match the provider's named shape and precedence exactly — that is what "stays a mirror" now means,
and the item's existing regression guard is to be read against the named type.

**Files:** `internal/tui/inspector.go`, `internal/tui/inspector_test.go`

**Tests.** In `inspector_test.go`, beside the existing response-passage tests (lines 610-700): a
response payload whose deltas spell `reasoning` yields no passage at all pre-item and exactly one
`thinking` passage post-item;
consecutive `reasoning` deltas concatenate into one passage exactly as `reasoning_content` deltas
do; a payload mixing the two spellings across chunks produces one thinking passage carrying both; a
chunk whose `reasoning` is a JSON object still renders its `content` as a text passage instead of
falling back to `prettyWireLine`.

**Acceptance.** `go build ./internal/tui/... && go test ./internal/tui/ -run 'Inspector|WireReadable|Readable'`

**Regression guard.** The mirror must stay a mirror: after this item its delta shape and precedence
match `internal/provider/stream.go`'s exactly. Do not import the provider type — it is unexported
and ADR 0010 keeps wire types provider-local.

`fix(tui): classify the reasoning field as thinking in /inspect`

## 3. stubllm emits and records either spelling; e2e drives /thinking — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the recorder writes `reasoning_field` ONLY for the bare spelling — `reasoning_content` leaves it unset, which is what an unset key already means, so recordings of llama.cpp-style servers stay byte-identical rather than gaining a line each. Pinned by TestRecorderLeavesTheDefaultSpellingUnwritten.
NOTES (2026-09-02): doc grep rule applied to test-drivers.md lines 32, 95 and 229. Line 65 (the example script) and line 183 (captures "refused as `reasoning` is", still true) were read and deliberately left: neither enumerates the turn keys nor the recorder's output.
NOTES (2026-09-02): added TestE2EThinkingFixturesDifferOnlyInTheSpelling beyond the item's Tests list — the two fixtures being identical but for one key is the claim both subtests rest on, and nothing else would catch that drift.
NOTES (2026-09-02): bite verified — with the provider's alias temporarily disabled, the `reasoning` subtest fails and the `reasoning_content` subtest passes; the provider file was restored unmodified (`git diff internal/provider/` empty).
NOTES (2026-09-02): ISSUES.md is dirty in the working tree from unrelated owner typo edits; left exactly as found and not in FILES.

**What.** The stub upstream only ever writes `reasoning_content` (`internal/stubllm/server.go:404`
streaming, `:447` whole) and only ever reads it back (`record.go:408` streaming, `:442` whole), so
no test can reproduce an OpenRouter stream and a real recording would silently drop the channel.

Add `ReasoningField string \`yaml:"reasoning_field,omitempty"\`` to `Turn` (`script.go`, beside
`Reasoning` at line 96), accepting exactly `reasoning_content` (the default, and what an empty value
means) or `reasoning`; any other value is a script-load error naming the key and the two accepted
spellings, in the style of the existing turn validation at `script.go:245-252`. `reasoning_field`
on a turn with no `Reasoning`, or on an `http`/`hang` turn, is refused the same way `reasoning`
already is there. Add `Reasoning string \`json:"reasoning,omitempty"\`` to `sseDelta`
(`wire.go:95`) and `wholeMessage` (`wire.go:131`); the emitters fill exactly one of the two, so an
unset knob produces byte-identical output to today. The recorder reads whichever field carried the
text and sets `ReasoningField` on the captured turn, so a recorded OpenRouter stream replays with
the spelling it was captured in.

Document the key in `docs/design/test-drivers.md` (the turn-key table at line ~88 and the
`reasoning` sentence at line 95).

Then the journey, driven over BOTH spellings so the pane is proven for llama.cpp-style servers and
OpenRouter alike: `cmd/apogee/testdata/stubllm/thinking.yaml` and
`cmd/apogee/testdata/stubllm/thinking-reasoning-field.yaml` script the same turn and the same
distinctive reasoning string, differing only in `reasoning_field`. A new
`cmd/apogee/e2e_thinking_test.go` — modelled on `cmd/apogee/e2e_stream_test.go` — runs one subtest
per fixture, driving a session, opening the pane with `/thinking`, and asserting the frame carries
that exact string. The `reasoning` subtest fails without item 1; the `reasoning_content` subtest
must pass before and after it, which is what pins that no existing server regressed.

Depends on item 1.

**Regression guard.** stubllm is the one place that keeps `Reasoning string`, deliberately. Its
`sseDelta`/`wholeMessage` (wire.go:95/131) are already NAMED types needing no extraction, and they
are ENCODERS first — they must marshal a plain JSON string, which json.RawMessage would not give
without hand-built bytes. Its recorder decodes only streams the stub itself captured, so the
non-string shape item 1 defends against is out of scope for a test double. Do not "unify" stubllm's
field with the provider's json.RawMessage: that would break the emitters at server.go:404 and :447.

The doc edit is a rule, not the two-site list: update every passage that enumerates the turn keys
or the recorder's output — `grep -n 'reasoning' docs/design/test-drivers.md` (lines 32, 64, 95, 176,
229) finds them all.

**Files:** `internal/stubllm/script.go`, `internal/stubllm/wire.go`, `internal/stubllm/server.go`,
`internal/stubllm/record.go`, `internal/stubllm/script_test.go`, `internal/stubllm/server_test.go`,
`internal/stubllm/record_test.go`, `cmd/apogee/testdata/stubllm/thinking.yaml`, `cmd/apogee/testdata/stubllm/thinking-reasoning-field.yaml`,
`cmd/apogee/e2e_thinking_test.go`, `docs/design/test-drivers.md`

**Tests.** `server_test.go`: a `reasoning_field: reasoning` turn writes `reasoning` and no
`reasoning_content`, streaming and whole; the default turn's bytes are unchanged. `script_test.go`:
an unknown value, and the key on a `Reasoning`-less / `http` / `hang` turn, each fail the load with
the key named. `record_test.go`: a captured `reasoning`-spelled stream round-trips to a turn whose
`Reasoning` holds the text and whose `ReasoningField` is `reasoning`. `e2e_thinking_test.go`: the
`/thinking` pane shows the fixture's reasoning text, and does not show `no thinking recorded yet`.

**Acceptance.** `go build ./... && go test ./internal/stubllm/... && go test ./cmd/apogee/ -run 'TestE2E'`
(the `Stub` half of the drafted `-run 'E2EThinking|Stub'` matched no test name; `TestE2E` is the
in-process set `docs/design/test-drivers.md:845` measures, and the run this item's guard demands.)

**Regression guard.** Every existing stubllm fixture and recording must replay byte-identically:
the new JSON fields are `omitempty` and the new script key defaults to today's spelling. Run the
full `./cmd/apogee/` e2e suite, not just the new test, before committing this item.

`test(stubllm): script and record either spelling of the thinking channel`

## 4. Report panes follow the tail, as the transcript does — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the kind gate is ONE statement — `reportKind.follows()`, an exhaustive switch
with a panicking default like the module's three other resolvers — read by `reportSpec`, `reportKey`
and `reportWheel`, so a fourth report must state its own answer rather than inherit /usage's.
NOTES (2026-09-02): deviation from the item's literal `reportWheel` formula. It prescribes
`follow = m.reportState(r).top >= max(0, win.total-shown)` after the switch; a FOLLOWING pane's
stored `top` is stale by design (the window is derived, not written), so a wheel-down over a pane
already at the tail — which fires neither guarded branch — would read a stale small `top` and drop
the follow. The follow is therefore re-derived from where the pane LANDED (`landed := win.start`,
overwritten by whichever branch fires), still ONCE after the switch and still off the `win` already
in hand. The two are identical in the case (d) the guard names — a detached pane whose rows dropped
onto the tail has `top >= last` and re-arms either way — and this form additionally keeps the
invariant total for a following pane that has grown.
NOTES (2026-09-02): the comment rule reached the grep's whole list —
`reportpane.go` (the doctrine bullets and the `reportSpec` clamp comment at :178-182, corrected to
credit the follow rather than the clamp for where the verbs open), `model.go:179-198`,
`doc.go:861`, `usage.go:39-41`, `inspector.go:87-89`/`:158`, `thinkingpane.go:197` and
`layout.md:1698`/`:1763`/`:1785`/`:1819`/`:1823`/`:1846`. Both verbs keep their own name
(`inspectorPane{…}`, `reportPane{…}`) and their existing `top`, adding only `follow: true`.
NOTES (2026-09-02): consequential edit — docs/manual/commands.md: made necessary by the follow, a
user-visible change to two shipped panes; the `/thinking` and `/inspect` rows said only that the
pane opens on the newest record.
NOTES (2026-09-02): bite verified — with the `reportSpec` follow branch temporarily disabled, both
verb-level growth cases fail (`window [29,37) of 39` for /inspect, `[12,20) of 22` for /thinking)
and pass with it restored; the file was restored byte-identically before the acceptance run.
NOTES (2026-09-02): ISSUES.md is dirty in the working tree from unrelated owner edits (as item 3
recorded); left exactly as found and not in FILES.

**What.** Recast at the regression check (2026-09-02). `/thinking` and `/inspect` open on their
newest row and then FREEZE: `reportPane.top`
(`reportpane.go:101`) is a fixed int, the verbs set it past the last row
(`thinkingpane.go:201`, `inspector.go:162`, both `top: len(rows)`), and `reportSpec`'s clamp
(`reportpane.go:183`) only pulls it down while it still exceeds `len(rows)-shown`. Once more rows
arrive the clamp stops moving it and the window stands still while reasoning streams past. Not a
regression — the panes never followed the tail; this is the behaviour the transcript has and they
lack. `/usage` shares the module and is NOT static — `usage.go:184-207` appends a delegate row per
run that reports a count, plus a session-total row — so follow is armed and honoured ONLY for
`inspectReport` and `thinkingReport`, and `/usage` keeps today's clamp-only behaviour exactly.

Mirror the transcript's mechanism exactly, because "the same behaviour as the main window" is the
ratified requirement. The transcript keeps `Model.detached` (`model.go:444`) under ONE total
invariant — `detached ⇔ off the bottom` — which its TWELVE writers each re-derive rather than
assume (from `!m.viewport.AtBottom()` at `model.go:1665`/`:2257` and `runview.go:237`, or by
re-arming on a send or a fresh view), and which `refreshViewport` (`model.go:2217`) honours by
ending each repaint at the true tail unless detached. The report panes' `follow` mirrors that
INVARIANT, not a single-writer shape.

Add `follow bool` to `reportPane`, stated positively (the panes have no "arrived detached" case the
transcript's negative spelling exists for). Both verbs ADD the flag and KEEP the existing top, each
under ITS OWN name — `inspectorPane{open: true, top: len(rows), follow: true}` at `inspector.go:162`
(the alias declared at `inspector.go:90` and documented at `:87-89`) and
`reportPane{open: true, top: len(rows), follow: true}` at
`thinkingpane.go:201`; replacing `top` would turn `TestInspectScopedOpensOnTheViewedRunsNewestRecord`
red, and that test is the open-on-newest pin this item's own guard relies on. `reportSpec`
derives `rowTop` as `max(0, len(c.rows)-shown)` while following and uses the clamped `top`
otherwise, with that derivation GATED BY REPORT KIND so only `inspectReport` and `thinkingReport`
reach it. BOTH scroll writers re-derive `follow` from where they landed
(`follow = newTop >= max(0, win.total-shown)`), and both gate arming by the same kind:
`reportKey` (`reportpane.go:271`), and `reportWheel` ONCE after its switch, from the resulting top
and the `win` it ALREADY holds (`reportpane.go:376`) — its branches write only `top`, so that `win`
is still current and a second `reportWindow` would recompose the whole pane (`inspectContent` plus
`renderPopupPlaced`) on every notch — NEVER inside its two guarded write branches (`reportpane.go:381-385`),
which do not fire on every notch, leaving a wheel-detached pane whose rows then rotated past their
cap sitting at `win.end == win.total` with nothing to re-arm it. So the invariant stays total in
both directions: scrolling up detaches, scrolling back to the bottom re-arms. `dismissReport`
(`reportpane.go:206`) already zeroes the struct, so each open starts armed.

Every comment or spec passage that names the `reportPane`'s fields or describes a report's scroll as
clamp-only is corrected — as a RULE, not a list:
`grep -rn 'reportPane\|usagePane\|inspectorPane\|last full window\|clamp every report' internal/tui layout.md`,
which reaches `model.go:184`/`:190`/`:198`
("a bool and an int", "Two plain values", "The same two plain values"), `doc.go:861`
("the reportPane value ({open, top})"), `usage.go:39-41`, and
`layout.md:1762-1763`/`:1819`/`:1823` — the last of those ("the scroll offset lands where the same
clamp every report applies puts it") being untrue of a following pane after a SHRINK, where the
clamp holds the old top and follow moves to the new tail. The narrower
`reportPane\|last full window` pattern matches NONE of `model.go:181`/`:187` or `layout.md:1819`,
which is why it is widened.
`reportpane.go:178-182` is named by that same rule: it credits the clamp with landing an opening
`/inspect` on the newest record, which after this item is `follow`'s doing — correct it rather than
leave it contradicting the code.

**Regression guard.** All five REGRESSION findings are accepted; the owner's ratified scope (follow
for `/thinking` and `/inspect` only, `/usage` untouched) is unchanged and every fix below serves it.
(a) The claim that "`/usage` is static" is FALSE and is deleted wherever it appears: `usage.go:184-207`
appends a delegate row per run plus a session-total row, and on a fitting pane the rule
`0 >= max(0, total-shown) = 0` would arm follow there. Arm and honour follow ONLY for `inspectReport`
and `thinkingReport` — gate it by report kind in `reportKey`, `reportWheel` and `reportSpec`'s
derived `rowTop` — so `/usage` keeps today's clamp-only behaviour exactly. (b) The verbs ADD the flag
and KEEP the existing top, each written under ITS OWN name:
`inspectorPane{open: true, top: len(rows), follow: true}` at `inspector.go:162` and
`reportPane{open: true, top: len(rows), follow: true}` at `thinkingpane.go:201`. The item yields
here to `inspector.go:87-90` (and `usage.go:39-41`), which record the per-pane alias as the intended
spelling at those sites — `inspectorPane` is a type alias, so `reportPane` would compile there and
leave silent doc drift inside the very file this item is de-drifting. Replacing `top` would turn
`TestInspectScopedOpensOnTheViewedRunsNewestRecord` red, and that test is the open-on-newest pin this
item's own guard relies on. (c) The growth tests as first written have NO BITE: today
`rowTop = min(top0, len(rows)-shown)` with `top0 = len(rows)` at open, so the pane already tracks the
tail until the list grows by a FULL WINDOW. The `thinkingpane_test.go` and `inspector_test.go` growth
cases therefore open through the VERB (not the helpers `thinkingPaneModel` / `inspectorPaneModel`,
which open at top 0 and would never follow), append MORE than `spec.maxRows` rows, and assert the
frame shows the new tail — and they must FAIL against the pre-item tree. (d) `reportWheel` writes
`top` only inside its two guarded branches (`reportpane.go:381-385`), so follow is not re-derived when
neither fires and the invariant is not total — a pane detached by wheel-up whose row list then rotates
past its cap sits at `win.end == win.total` and no wheel-down re-arms it. Re-derive follow ONCE after
the switch, never inside the write branches — and from the `win` already in hand
(`reportpane.go:376`), which the branches leave current because they write only `top`:
`follow = m.reportState(r).top >= max(0, win.total-shown)`. A second `reportWindow` there would
recompose up to 20 records × 100 lines on every notch the pointer makes over the pane.
(e) The Files line and the prose fix are incomplete: add `internal/tui/model.go`,
`internal/tui/doc.go`, `internal/tui/usage.go` and `layout.md`, and state the prose fix as a RULE, not
a list — correct every comment or spec passage that names the `reportPane`'s fields or describes a
report's scroll as clamp-only
(`grep -rn 'reportPane\|usagePane\|inspectorPane\|last full window\|clamp every report' internal/tui layout.md`;
the narrower `reportPane\|last full window` pattern misses `model.go:181`/`:187` and
`layout.md:1819` outright), which reaches `model.go:184`/`:190`/`:198`, `doc.go:861`, `usage.go:39-41` and
`layout.md:1762-1763`/`:1819`/`:1823`. The `D:` line is accepted with it: `reportpane.go:178-182` credits the
clamp with landing an opening `/inspect` on the newest record, which after this item is `follow`'s
doing — that comment is named by the same rule and must be corrected, not left to contradict the code.
(f) Two premises are corrected with them, neither of which any prescription above rests on. The
transcript writes `detached` at TWELVE sites (`commandrun.go:193`/`:307`, `interject.go:307`/`:471`,
`model.go:1665`/`:1764`/`:2225`/`:2257`, `runview.go:194`/`:230`/`:237`, `sessions.go:592`), not at
one funnel: what the panes mirror is the total invariant every writer re-derives, not a single-writer
shape. And the `/inspect` growth case must stay UNDER the wire ring's 20-RECORD cap
(`maxWireRecords`, `inspector.go:105`) while it appends — the ring holds at most 20 records of
roughly three rows each, so a case that appends past it rotates the oldest out, net growth falls to
zero and the test stops biting. The `/thinking` case has room under the board's 64-record cap
(`maxThinkingRecords`, `thinking.go:62`) and needs no such qualification; do not copy one case's
shape onto the other.

**Files:** `internal/tui/reportpane.go`, `internal/tui/thinkingpane.go`, `internal/tui/inspector.go`,
`internal/tui/model.go`, `internal/tui/doc.go`, `internal/tui/usage.go`, `layout.md`,
`internal/tui/reportpane_test.go`, `internal/tui/thinkingpane_test.go`, `internal/tui/inspector_test.go`

**Tests.** `reportpane_test.go`: a pane opened with `follow: true` whose row list then GROWS shows
the new tail, not the old window; one scrolled up by key holds its window across the same growth;
scrolling back down to the last full window re-arms follow and the pane resumes tracking; the wheel
does both of those too, including a wheel-down notch that fires neither guarded branch. Plus the kind
gate: one ↓ on a FITTING `/usage` pane leaves follow off, and an appended delegate row does not move
its window. `thinkingpane_test.go` and `inspector_test.go`: the growth cases open through the VERB
(`runThinkingCommand` / `runInspectCommand`), never through the helpers `thinkingPaneModel` /
`inspectorPaneModel`, which open at top 0 and would never follow, and append MORE than `spec.maxRows`
rows — thinking records appended to the board, wire records arriving at the inspector — before
asserting the frame shows the new tail without a keystroke. The `/inspect` case opens on a couple of
records and stays UNDER the wire ring's 20-RECORD cap (`maxWireRecords`, `inspector.go:105`) across
that growth: the ring holds at most 20 records of roughly three rows each, so a case that appends
past it rotates the oldest out, net growth falls to zero and the bite is gone. The `/thinking` case
needs no such qualification — the board caps at 64 records (`maxThinkingRecords`, `thinking.go:62`),
leaving room for the rows this case adds — so the two cases are sized differently on purpose and
neither shape is to be copied onto the other. Both cases must FAIL against the pre-item
tree; a few appended rows would not, because today's clamp already tracks the tail until the list
grows by a full window. `inspector_test.go` also: ctrl+r still toggles `raw` without disturbing
`follow`, and `TestInspectScopedOpensOnTheViewedRunsNewestRecord` passes unmodified.

**Acceptance.** `go build ./internal/tui/... && go test ./internal/tui/`

**Regression guard.** The clamp at `reportpane.go:183` stays for the NOT-following path — a detached
pane whose grant shrank or whose rows dropped (the board caps at `maxThinkingRecords`) still needs
it, and deleting it would paint one row over an empty pane. Opening behaviour must not change: both
verbs still land on the newest row, which the existing open-on-newest tests pin. `/usage` gains no
follow behaviour and its tests must pass unmodified.

`fix(tui): /thinking and /inspect follow the tail like the transcript`

---

**Suggested version bump:** patch (`VERSION` micro-bump) once all four items land — a user-visible
fix to a shipped pane. Not performed by this plan; the owner decides.
