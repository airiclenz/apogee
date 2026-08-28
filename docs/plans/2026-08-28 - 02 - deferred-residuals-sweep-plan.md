# Deferred residuals sweep — implementation plan

**Goal:** close every entry in the nine "… residuals — deferred out of the YYYY-MM-DD run"
sections of `ISSUES.md` (the "Open defects" block, lines 35–389 at write time): 45 findings
left behind by the eight archived plans of 2026-08-25 … 2026-08-28. Each is a verified
defect, a missing test, or a stale doc with `file:line` evidence; none is a regression (those
never reach ISSUES.md — plan `2026-08-28 - 01`). The plan groups them into 34 items by
package, each sized to one sub-agent, and strikes the nine sections from `ISSUES.md` as
item 34; item 35 closes one residual of the `2026-08-28 - 01` run that never reached
`ISSUES.md`. The CHANGELOG entries land at the closeout from the sidecars, per the skill.

**Date:** 2026-08-28
**Status:** unexecuted
**sized for:** ~200k-context host

**Authoritative sources:**
- `ISSUES.md` — the nine residual sections (each item quotes its entry's title). Where an
  entry's claim turned out wrong on re-verification, the item says so and the corrected
  fact is the binding one (item 14, item 34).
- `AGENTS.md` (Bypass floor, ADR 0031 north star, ISSUES/CHANGELOG convention);
  `docs/design/test-drivers.md` (stubllm scripts, tuitest drivers, e2e support helpers);
  `docs/design/confinement-execution-contract.md`; ADR 0012 (2026-07-26 amendment), ADR
  0031, ADR 0039, ADR 0041, ADR 0049, ADR 0056, ADR 0060, ADR 0061.
- Line numbers below were re-verified on 2026-08-28 against `main` at `91be2725`; the
  symbol name is the anchor, the number a hint.

**Ratified design calls (owner unless noted, all 2026-08-28):**
- **Scope:** the nine run-residual sections only. `Improvements / Ideas` and the whole
  `Parked / deferred work` section stay out — each needs its own grill.
- **C-03 effort dialect seed:** carried on `domain.Config`, applied at engine construction;
  `Rebind` keeps overriding it. (Plan author: a mirrored `domain.EffortDialect` string type,
  converted in `internal/agent/wire.go` beside `toProviderEffort` — `domain` must not import
  `provider`, matching the existing `domain.ThinkingEffort` ↔ `provider.Effort` mirror.)
- **ADR 0060 D6:** amended to the ellipsis truncation the left run of footer segments has
  always had; no footer code change.
- **`rmFlag`:** a bare `-` is restored as a flag token, so `rm - -rf /etc` matches again.
  `rm rf /etc` (harmless) stays unmatched and the test pins that — a dashless flag word
  would fire on `rm build /etc`-shaped lines and trades the precision the rule defends.
- **Transcript wire:** `wireEntry` gains a cached-prompt-tokens member; the census test is
  updated with this plan as the recorded decision. Old blobs decode to 0.
- **MCP `WaitDelay`:** the stdio `Cmd` gets a cancellable context (never the connect ctx)
  that `Client.Close` cancels after the SDK's clean close returns, so `cmd.Cancel` and
  `WaitDelay` bound the drain.
- **Settings `auto` row at narrow width:** below the threshold the cell renders
  `(current)` first, then the sentence (which ellipsizes); the wide layout (sentence, then
  marker) is unchanged.
- **Child fold bridge:** a child's mid-Exchange fold appends the same `overflowBridge` the
  emergency fold uses; `assertRequestTemplateLegal` gains the trailing-role check.
- **`filehint` ID-less fallback:** when no call in the opening turn carries an ID, results
  are parsed only if EVERY call in that turn is a listing tool.
- Plan-author calls (mechanical, following existing precedent) are stated inline in each
  item's **What** as binding text.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Per-item Acceptance is targeted; `make check` runs once, at the closeout.
- No item changes `VERSION`, a CHANGELOG release heading, or a tag.
- Each item's sidecar CHANGELOG entry names the ISSUES.md entry it closes (its bold title)
  so the closed trail is greppable.

**Out of scope:**
- Everything under `ISSUES.md` "Improvements / Ideas" and "Parked / deferred work"
  (including the Audit-residue C-13, C-04, C-20/F-08, C-15 entries and the four irreducible
  test-driver claims).
- The five hand-rolled `LookPath` + fence pairs (architecture pass).
- Teaching `internal/stubllm` a `/props` route (item 30 pins the cap instead).
- Splitting `security.Rule.Hint` into human/model halves (item 19 uses neutral wording).
- Per-model effort support from a `/props`-only server (item 3 states the fallback).

---

## 1. The turn-error hint fires on every effort dialect — ✅ DONE (2026-08-28)

NOTES (2026-08-28): edited `docs/adr/0050-…md` beyond the item's Files list — decision 4 of that ADR states the hint's gate as "the failed request carried `chat_template_kwargs`", which this item's change makes an under-description; a dated amendment parenthetical (the file's own house pattern) records the widened gate and the reworded hint.

**What.** ISSUES: *The enriched turn-error hint reaches only the kwargs dialect.* The four
gates `len(wire.ChatTemplateKwargs) > 0` (`internal/provider/client.go:336`, `:347`;
`internal/provider/stream.go:85`, `:88`) become one predicate on the wire body,
`chatRequest.carriesEffort()` in `internal/provider/wirejson.go`, true when any of the
three fields `applyEffort` (`client.go:610-638`) sets is non-nil: `ChatTemplateKwargs`,
`Reasoning`, `ReasoningEffort`. The `off` dialect sets none and stays silent (binding —
gate on the body, not on `req.ThinkingEffort`, so
`TestBuildBody_OffDialectEmitsNothingOnEveryLevel` keeps its meaning). Rename the bool
parameter threaded through `statusError`, `inBandError`, `statusDelta`, `inBandErrorDelta`
and `parseSSE` to say "effort" not "kwargs". Reword `thinkingEffortHint` (`client.go:486`)
so it names the intent — that the request asked for a thinking effort under
`thinking.effort:` / the `/effort` override — and no dialect's field.

**Files:** `internal/provider/client.go`, `internal/provider/stream.go`,
`internal/provider/wirejson.go`, `internal/provider/client_test.go`,
`internal/provider/stream_test.go`

**Tests.** Extend `TestRespond_ThinkingEffortHint` and `TestStream_ThinkingEffortHint` into
tables over the three dialects (kwargs, reasoning, openai): a 4xx status and an in-band
error each carry the hint on all three; the `off` dialect carries none. Assert the hint text
no longer contains `chat_template_kwargs`.

**Acceptance.** `go build ./... && go test ./internal/provider/`

**Commit.** `fix(provider): the effort turn-error hint fires on the reasoning and openai dialects too`

## 2. The engine's effort dialect has a construction seed (C-03) — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the seed lands in `newAgent` (`internal/agent/construct.go`) rather than literally in `agent.New` — `New`, `Resume` and the white-box test path all funnel through it, and the item's own test ("drives the request through the fake provider") constructs by that path; seeding only in `New` would leave `Resume` and every fake-provider Driver unseeded.

NOTES (2026-08-28): the item names `rebindSpecFor` as what puts the dialect on the Firing's Spec; it does not — the interactive path sets `spec.EffortDialect` at the caller (`cmd/apogee/wire_verbs.go:52`) and `firingConfig` never did. `firingConfig` therefore resolves the value itself (forced entry key, else one beat) and states it on the Config; the Spec is unchanged.

NOTES (2026-08-28): the beat "already taken at headless.go:109" returns only `TotalSlots` and is skipped behind a `parallel-agents:` pin, so widening it would rename `discoverSlots` and move six call sites. The dialect took its own package seam, `discoverDialect`, beside it, plus an optional `firingInputs.dialect` — a session passes its own `observedDialect()` (`cmd/apogee/schedule.go`) so a Firing spends no round trip re-asking, the same design call `width` follows.

NOTES (2026-08-28): the item's headless test ("a stubllm whose `/v1/models` reports a reasoning dialect … read the stub's request log") is not implementable — `internal/stubllm` serves `{id, object}` only on `/v1/models` and its request log records no effort field, and teaching it either is out of this plan's scope. Substituted: `TestHeadlessSendsTheServersEffortDialect` drives the real CLI over the runner seam and asserts the composed `run.Spec.Config.EffortDialect` across forced/observed/silent servers, with `internal/agent`'s `TestNewSeedsTheEffortDialectFromTheConfig` carrying the same value onto the wire body the fake provider was handed.

NOTES (2026-08-28): edited `internal/config/options.go`, `internal/config/config.go` and `cmd/apogee/wire_server.go` beyond the item's Files list — `startupEntry` dropped the entry's `effort-dialect:`, so the seed's stated first rank ("the entry's forced `effort-dialect:`") was dead on every Driver, the interactive session's Monitor included. Fixed with the established `Startup*` flattening pattern.

**What.** ISSUES: *The Agent's effort dialect has no construction seed* (code audit C-03,
ADR 0031 Driver parity). Add `EffortDialect` to `domain.Config` (`internal/domain/config.go:17`)
as a new `domain.EffortDialect` string type mirroring `provider.EffortDialect`'s vocabulary
(`internal/provider/wire.go:128-150`) with a `Valid()` like `ThinkingEffort`'s; convert it
in `internal/agent/wire.go` beside `toProviderEffort` (`:75`). `agent.New`
(`internal/agent/agent.go:369-378`) seeds `a.effortDialect` from `cfg.EffortDialect`;
`Rebind` (`internal/agent/rebind.go:261`) keeps overwriting it. Producers of the new value
(enumerated — this is a changed representation): `firingConfig`
(`cmd/apogee/wire_firing.go:178`) copies the same dialect `rebindSpecFor` (`:134`) puts on
the Spec; `runOnce`'s Spec (`cmd/apogee/headless.go:92`, `:380`) seeds it from the entry's
forced `effort-dialect:` else the startup beat's `EffortSupport.Dialect` (the beat already
taken at `headless.go:109`); `daemonfire.go:225` goes through `runOnce` and needs no change
beyond what `runOnce` does. Consumers: `internal/agent/wire.go:48` (unchanged read),
`run.Once` (`internal/run/run.go:230`, `agent.New(cfg)` — no change). Update the field doc
at `agent.go:160-170` so it no longer says the zero value is the only pre-rebind state.

**Files:** `internal/domain/config.go`, `internal/agent/agent.go`, `internal/agent/wire.go`,
`cmd/apogee/wire_firing.go`, `cmd/apogee/headless.go`, `internal/agent/agent_test.go`,
`cmd/apogee/wire_firing_test.go`, `cmd/apogee/headless_test.go`

**Tests.** `internal/agent`: a config carrying `EffortDialect: reasoning` puts `reasoning`
on the first request with no `Rebind` (drives the request through the fake provider and
reads the wire body); `TestRebindCarriesTheEffortDialectOntoTheRequest` keeps proving
`Rebind` overrides. `cmd/apogee`: `TestFiringConfigSetsEveryUnattendedField` asserts
`cfg.EffortDialect` equals the Spec's; a headless run against a stubllm whose `/v1/models`
reports a reasoning dialect sends that dialect on its first request (read the stub's request
log — the announced surface is the wire).

**Acceptance.** `go build ./... && go test ./internal/domain/ ./internal/agent/ ./internal/run/ && go test ./cmd/apogee/ -run 'Firing|Headless|Effort'`

**Commit.** `fix(agent): a Driver that never rebinds still sends the server's effort dialect`

## 3. A `/model` pick judges the override against the picked model; the cleared note names the real fallback — ✅ DONE (2026-08-28)

NOTES (2026-08-28): edited `internal/tui/actuation_test.go` beyond the item's Files list — adding `provider.EffortSupport` (which holds a slice) to `rebindIntent` makes the struct non-comparable, so the two `*m.hb.pendingRebind != want` assertions (`actuation_test.go:548`, `heartbeat_test.go:936`) had to become `reflect.DeepEqual`. Mechanical, no assertion weakened.

NOTES (2026-08-28): `Discover` now applies the server entry's forced `effort-dialect:` (`forceEffortDialect`) to every advertised entry, not only to the active model. Beyond the item's literal text, which names only the `modelsResponse.effortSupport` path: without it a server that forces a dialect different from the one it advertises would hand the pick a vocabulary the active model's own resolution had already dropped, and the two answers about one server would disagree (ADR 0060 decision 3's "one channel"). It can only narrow the clear, never widen it. The /props chat-template tell is deliberately NOT spread across entries — it describes the one model the server has loaded.

NOTES (2026-08-28): `bindPickedModel` now takes the whole `heartbeat.ModelSummary` instead of `(id string, window int)`. Both call sites already held the entry, and the window it binds and the vocabulary it judges against are both entry facts — splitting them into parameters would invite a caller to pass one without the other.

**What.** ISSUES: *A `/model` pick judges the override against the PREVIOUS model's level
set* and *The cleared-override note says "back to auto" even when a profile level sits
underneath.* (1) Widen `provider.DiscoveredModel` (`internal/provider/discovery.go:19-23`)
and `heartbeat.ModelSummary` (`internal/heartbeat/heartbeat.go:34-41`) with a per-entry
`EffortSupport`, computed by the same `modelsResponse.effortSupport` path (`discovery.go:375`,
`:384`) that today serves only the active id. `bindPickedModel` (`internal/tui/picker.go:741`)
passes the picked model's support into `applyRebind` (a field on `rebindIntent`,
`internal/tui/heartbeat.go:~86-105`), and the clear at `heartbeat.go:384` judges against
that instead of `m.effortSupport()`. Fallback (binding): a picked model whose support is
empty (a `/props`-only server, or a `/v1/models` entry without reasoning metadata) keeps the
override standing and the next beat judges it — today's behaviour, now stated in the
`rebindIntent` field doc. (2) `effortClearedNote` (`heartbeat.go:418`) takes the profile
level and the support's `Default` and words its tail with `footerEffortLabel("", profile,
default, true)` (`internal/tui/effort.go:209`) — the note and the footer share one ladder and
cannot disagree.

**Files:** `internal/provider/discovery.go`, `internal/heartbeat/heartbeat.go`,
`internal/tui/heartbeat.go`, `internal/tui/picker.go`,
`internal/provider/discovery_test.go`, `internal/tui/heartbeat_test.go`,
`internal/tui/picker_test.go`

**Tests.** Provider: a `/v1/models` payload with two entries of different reasoning
vocabularies yields two different `EffortSupport`s. TUI: a pick into a model whose support
excludes the live override clears it in the same `Update` (no beat); a pick into a model with
empty support leaves it; `TestSwitchClearsAnExcludedEffortOverride` (beat path) still
passes. Note: with `thinking.effort: medium` in the profile the note ends `back to medium`;
with no profile level `back to auto` — assert the exact strings the footer paints.

**Acceptance.** `go build ./... && go test ./internal/provider/ ./internal/heartbeat/ && go test ./internal/tui/ -run 'Effort|Switch|Pick|Rebind|Beat'`

**Commit.** `fix(tui): a model pick judges the effort override against the picked model and the cleared note names the fallback`

## 4. `pickerKindCases` covers the effort picker — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the arm asserts with `reflect.DeepEqual` (already imported, and what every other arm in `pickerKindCases` uses) rather than `slices.Equal`; same assertion, no new import.

**What.** ISSUES: *`pickerKindCases()` no longer covers every overlay kind.* Add a
`pickerEffort` arm to `pickerKindCases` (`internal/tui/picker_test.go:1714`): `open` builds
a `*fakeEngine` and calls `openEffortPicker(t, eng, support)` (`command_test.go:109`) with an
explicitly reported vocabulary `Efforts: {"low","medium","high"}` (binding — not the
canonical-four fallback), `filter: "med"`, `want: "medium"`, and an assert closing over
`eng` that `eng.effortsSet()` equals `[]domain.ThinkingEffort{domain.EffortMedium}` — the
driven seam, as every other arm asserts.

**Files:** `internal/tui/picker_test.go`

**Tests.** The new arm runs under `TestPickerFilteredViewAgreesOnRowsCountAndAccept`; the
doc comment at `:1711` is true again.

**Acceptance.** `go test ./internal/tui/ -run 'TestPickerFilteredViewAgreesOnRowsCountAndAccept'`

**Commit.** `test(tui): the filtered-accept census covers the effort picker`

## 5. A child's mid-Exchange fold bridges back to a user turn — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `reanchorAfterShrink` is dropped from `autoCompact` rather than kept on an else branch — it is a documented no-op outside an Exchange, so the `inExchange` branch the item replaces was the only one where it did anything; keeping the call would have left a line that provably cannot fire.

NOTES (2026-08-28): edited `docs/adr/0018-…md` beyond the item's Files list — its 2026-08-26 amendment states that a child's estimate-driven fold re-anchors "through the sibling of §3's `anchorAtBridge` (`reanchorAfterShrink`)", which this item's change makes false, and §4 scopes the bridge to the emergency fold alone. Both carry dated amendment markers (the file's own house pattern) naming the widened rule.

NOTES (2026-08-28): the item says "all seven call sites must still hold" for `assertRequestTemplateLegal`; the tree has five (`overflowrecovery_test.go:178`, `:378`, `predictiveguard_test.go:129`, `:212`, `subagent_test.go:1299`). All five hold, and the whole `internal/agent` package is green.

**What.** ISSUES: *A child's post-fold request ends on the assistant summary and no test pins
it.* In `autoCompact` (`internal/agent/compact.go:105`), when the fold ran inside an
Exchange (`a.turns.inExchange`, the branch only a child with `midExchangeCompaction` reaches),
append `domain.Message{Role: domain.RoleUser, Content: overflowBridge}` and call
`a.turns.anchorAtBridge()` — exactly what `emergencyFold` does at `compact.go:320-321` —
in place of `reanchorAfterShrink` on that branch. Reuse `overflowBridge` verbatim (binding;
no second prompt asset). The depth-0 Exchange-boundary fold is untouched: the real user
message follows it. Extend `assertRequestTemplateLegal`
(`internal/agent/overflowrecovery_test.go:87`) to also require the last non-system message
to be `RoleUser` or `RoleTool`; all seven call sites must still hold.

**Files:** `internal/agent/compact.go`, `internal/agent/overflowrecovery_test.go`,
`internal/agent/subagent_test.go`, `internal/agent/autocompact_guard_test.go`

**Tests.** `TestSubAgent_ChildFoldsMidDelegationAndFinishes` fails against the pre-item
tree once the helper checks the trailing role (bite check), and passes after: the post-fold
request ends `assistant-summary | user(bridge)`.
`TestAutoCompactSkipsMidExchangeThenFoldsAtNextOpening` proves the main loop gained no
stray bridge.

**Acceptance.** `go test ./internal/agent/ -run 'Compact|SubAgent|Fold|Overflow'`

**Commit.** `fix(agent): a child's mid-exchange fold appends the user bridge so no request ends on the summary`

## 6. A delegate's empty capped reply reports the reasoning spend — ✅ DONE (2026-08-28)

**What.** ISSUES: *A child's EMPTY capped reply lost the reasoning-spend number.* In
`replyFault` (`internal/agent/loop.go:524`) move the visible-text check ahead of the
delegate rule: a reply with no visible text and no calls goes to `emptyReplyFault` (`:561`)
at every depth, so a depth > 0 `finish: length` empty reply uses `cappedReplyErrFmt`
(`:470`) with its "after roughly N tokens of reasoning" tail; a delegate's capped reply WITH
text keeps `cappedDelegateReplyErrFmt` (`:480`). Binding: no third format string.

**Files:** `internal/agent/loop.go`, `internal/agent/emptyreply_test.go`

**Tests.** A new table row in `emptyreply_test.go` — depth 1, `finish: length`, empty
text, reasoning usage present — asserts the fault text contains `tokens of reasoning`.
`TestCappedReplyWithTextFaultsOnlyOnADelegate` unchanged and green.

**Acceptance.** `go test ./internal/agent/ -run 'Reply|Capped|Empty'`

**Commit.** `fix(agent): a delegate's empty capped reply names the reasoning it burned`

## 7. The loop drops malformed native tool calls the way the probe does — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the shared predicate is `processing.WellFormedToolCall(name, id string) bool` — two strings rather than a call type. Its callers hold different shapes (`provider.ToolCall` in the probe, `domain.ToolCall` in the loop) and `processing` must not import `provider` (ADR 0010, restated in the package's own `NativeToolCall` doc), so a slice-filtering signature could not have served both. Each caller keeps its own one-line filter loop over the shared rule.

NOTES (2026-08-28): the loop's filter lives in a new `(*Agent).dispatchableCalls` helper beside `assembleResponse` rather than inline at `loop.go:581` — the ErrorEvent needs `turn` and `a.cfg.Events`, and inlining the emit would have put a five-line side effect in the middle of the parse seam. `assembleResponse`'s doc comment names it.

NOTES (2026-08-28): `dispatchableCalls` returns the caller's own slice unchanged when nothing is dropped, so the no-malformed-call path (every real reply) is byte-identical to the pre-item pass-through, including its nil-ness.

**What.** ISSUES: *`loop.go` dispatches native tool calls unfiltered — the probe is now
stricter than the loop.* At `assembleResponse` (`internal/agent/loop.go:581`, before
`calls := nativeCalls` at `:584`) filter `nativeCalls` with the probe's predicate — keep
only calls with a non-empty name AND a non-empty ID — sharing the rule with
`wellFormedToolCalls` (`internal/probe/battery.go:157`) by moving the predicate to
`internal/processing` (one function, both callers; `processing.ParseNativeToolCalls`'s
atomic-or-error contract at `toolcall.go:36-39` is NOT changed). When at least one call is
dropped, emit ONE `ErrorEvent` from source `"processing"` (mirroring `loop.go:418`) naming
how many entries lacked a name or id. Control flow after the filter is the existing one: some
survive → dispatch those; none → the text parser at `:585`, then `replyFault`. Never
synthesise an ID for a native call (binding — C-18's unusable-echo hazard).

**Files:** `internal/agent/loop.go`, `internal/processing/toolcall.go`,
`internal/probe/battery.go`, `internal/agent/loop_test.go`,
`internal/processing/toolcall_test.go`

**Tests.** Loop: a reply whose `tool_calls` carries one well-formed call and one with an
empty `id` dispatches exactly the first and emits one processing `ErrorEvent`; a reply whose
only call lacks an id falls through to the text parser and, with empty text, faults. Probe:
`TestBatteryMalformedToolCallsAreNotEvidence` still green over the shared predicate.

**Acceptance.** `go build ./... && go test ./internal/processing/ ./internal/probe/ && go test ./internal/agent/ -run 'Native|Assemble|ToolCall'`

**Commit.** `fix(agent): id-less native tool calls are dropped with a signal instead of dispatched`

## 8. The fan-out path refuses colliding argument keys like the serial path — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's second test ("the fan-out `ToolCallEvent` carries `ResolvedPath`") is driven at the `prepareDelegation` seam rather than through a whole run — the fan-out group holds `sub_agent` calls only, `sub_agent` writes nothing inspectable, so an end-to-end fan-out can only ever observe `""`, which is also what the unfixed code produced. Handing the seam a workspace-scoped writer whose target travels a symlink is the one way to see the field populated; the bite check confirms a revert drops it back to `""`.

**What.** ISSUES: *The fan-out path resolves without the colliding-key check.* Extract the
refusal at `internal/agent/dispatch.go:389-391` into
`collidingArgumentKeysResult(call) (domain.ToolResult, bool)` beside
`collidingArgumentKeysMessage` (`:422`); call it in `resolveAndExecute` where it was, and in
`prepareDelegation` (`:237`) AFTER `runPreToolExecHooks` (`:240` — hooks may rewrite
arguments) and after the `lookupTool` miss (`:249-253`), returning the same unaudited
`fanOutSlot{call, result}` with `run: false` shape the unknown-tool slot uses (binding: no
`executeRefuse`, no audit record — the serial path records none). Also carry
`ResolvedPath: a.resolvedPath(call)` on `prepareDelegation`'s `ToolCallEvent` (`:238`) as
`dispatchSerially` does at `:137` — the second divergence between the two paths.

**Files:** `internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`

**Tests.** Mirror `TestDispatch_CollidingArgumentKeysAreRefusedBeforeResolution` (`:1281`)
over a fanned-out group: same refusal wording, approver never consulted, nothing ran, no
`ApprovalEvent`; and the group's other member still runs. A second test asserts the
fan-out `ToolCallEvent` carries `ResolvedPath`.

**Acceptance.** `go test ./internal/agent/ -run 'Dispatch|Colliding|FanOut|Delegat'`

**Commit.** `fix(agent): a fanned-out call with colliding argument keys is refused like a serial one`

## 9. `filehint` fires on ID-less listing results — ✅ DONE (2026-08-28)

NOTES (2026-08-28): amended the file's `init` doc rule (2) beyond the item's literal "update the function doc" — that rule states the ID gate as the whole story ("Only a tool result whose ToolCallID answers a call to a listing tool is parsed"), which this item's change makes false; it is reworded to name both matchings while keeping the invariant it defends (only a listing tool's result is ever parsed).

**What.** ISSUES: *`filehint`'s ID→tool gate needs a non-empty native tool-call ID.* In
`fileHintDetectOpportunity` (`internal/mechanisms/filehint.go:138`), after the map build
(`:164-169`): when the opening assistant turn carries at least one call and EVERY call's
`.Tool` is in `fileHintListingResultTools` (`:73`), accept every in-batch `RoleTool` message
regardless of `ToolCallID` (the existing `break` at `:175-177` still bounds the batch).
Otherwise the ID gate stands unchanged, so a turn mixing a listing tool with any other tool
never parses an unmapped result (C-08 stays closed). Update the function doc to state both
rules.

**Files:** `internal/mechanisms/filehint.go`, `internal/mechanisms/filehint_test.go`

**Tests.** Beside `TestFileHintParsesOnlyListingResults` (`:262`): an all-listing turn whose
results carry empty IDs fires the hint; a mixed turn with empty IDs fires nothing.

**Acceptance.** `go test ./internal/mechanisms/ -run 'FileHint'`

**Commit.** `fix(mechanisms): filehint parses id-less results when the whole turn is listing tools`

## 10. The transcript wire carries a delegation's cached-token share — ✅ DONE (2026-08-28)

NOTES (2026-08-28): edited `internal/tui/transcript_test.go` beyond the item's Files list — the shared fold helper `childUsage` builds a `domain.UsageEvent` from a `usageTotals` and mapped only four of its five members, so no test could drive a cache share through the real `applyUsage` path. One line maps `CumulativeCachedPromptTokens`; every existing caller passes a share of zero, so nothing else moves.

NOTES (2026-08-28): the new usage-pane test is named `TestUsageRestoredDelegateKeepsItsCachedShare` so the item's own Acceptance filter (`-run 'Transcript|Codec|Usage'`) selects it — a name without one of those three words would have been silently skipped by the acceptance command.

NOTES (2026-08-28): the item's third test ("a pre-feature blob decodes with 0") landed as its own subtest of `TestTranscriptCodecRoundTripsASubAgentsTotals` carrying the OTHER four members and no share, rather than relying on the existing all-zero legacy subtest — a blob with zero totals cannot tell a decoded zero from an empty accounting.

**What.** ISSUES: *A resumed delegate row loses its cached share.* Add
`UsageCachedPromptTokens int \`json:"usageCachedPromptTokens,omitempty"\`` to `wireEntry`
(`internal/tui/transcriptcodec.go:95-98`, between `UsagePromptTokens` and
`UsageCompletionTokens`), map it in `toWireEntry` (`:409-412`) and `fromWireEntry`
(`:524-529`, into `usageTotals.CachedPromptTokens`). Additive under the codec's stated rule
(omitempty; old blobs decode to 0) — no `transcriptVersion` bump. Update the census
`wantEntry` (`transcriptcodec_test.go:1204-1209`) in the same position, citing this plan in
the test's decision comment.

**Files:** `internal/tui/transcriptcodec.go`, `internal/tui/transcriptcodec_test.go`,
`internal/tui/usage_test.go`

**Tests.** Round trip: a delegation head with `CachedPromptTokens: 300` survives
`toWireEntry`/`fromWireEntry`; a restored transcript renders the delegate's usage row with a
non-empty cached cell (`usageSubAgentRows`, `internal/tui/usage.go:212`); a pre-feature blob
(no member) decodes with 0.

**Acceptance.** `go test ./internal/tui/ -run 'Transcript|Codec|Usage'`

**Commit.** `fix(tui): a restored delegation keeps its cached-token share on the usage row`

## 11. A delegation's verdict comes from its outcome, never from its report text — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's second half ("also set `failed` on the head's `branchSummary` wherever the presenter enriches a delegation whose tool result is an error result") needed no code — the premise that a refusal is a quoted promotion with `.failed` false does not hold: `runSubAgent` returns every refusal through `errorToolResult` (`IsError: true`, `internal/agent/subagent.go:100-116`), and `enrichWithResult` short-circuits such a result into `absorbFailure`, which words it with `namedSummary` and so already sets `failed`. The invariant is pinned instead by the new "a delegation refused before it ran is red and wears no done mark" subtest, which passes with no change under `internal/tui/toolview.go`.

NOTES (2026-08-28): the golden at `subagentblock_test.go:1180` KEEPS its ✓, against the item's "the ✓ goes". Its fixture `refusedDelegation` (`:1099`) reports through `subAgentReport`, which builds a NON-error `domain.ToolResult`, so under this item's own binding rule ("the verdict is the result's error status") that row is a delegation that succeeded and quoted a one-line report opening `error: …` — exactly the case the item requires to keep its ✓. Making the fixture a real error result would take the promotion away (`absorbFailure` sets no stat, so `promotable` is false), which is what `unframedSubAgentView`'s body layout and `guardRefuses` both work on — and what item 12's whole approach is written against. Left alone; the promised behaviour is asserted on a faithful error-result refusal instead.

NOTES (2026-08-28): edited `internal/tui/toolleader.go` beyond the item's Files list — `failedSummary`'s doc named `subAgentSummary` as its "one other caller" and explained why it read the head's text there, which this item's change makes false. Reworded to name `namedSummary` as the only caller and to record what the removed reading got wrong.

**What.** ISSUES: *A red slot and a ✓ can land on one delegation row.* Replace
`summary.failed = failedSummary(head.tool.Summary.Text)` (`internal/tui/subagentblock.go:595`)
with `summary.failed = head.tool.Summary.failed` — the field `namedSummary` sets
(`internal/tui/toolview.go:142-143`) and `subAgentFinished` (`subagentblock.go:402`) already
reads. Because a refused/never-ran delegation is a *quoted* promotion whose `.failed` is
false (`toolview.go:152-154`), also set `failed` on the head's `branchSummary` wherever the
presenter enriches a delegation whose tool result is an error result (the refusal path), so
a refusal is red AND wears no ✓, while a succeeded delegation whose quoted report opens
`error: …` is neither red nor un-✓'d. Binding: the verdict is the result's error status;
`failedSummary` is no longer consulted for delegations. Update the golden at
`subagentblock_test.go:1177` (`… ✓ ⋯ <refusedResult>`) — the ✓ goes.

**Files:** `internal/tui/subagentblock.go`, `internal/tui/toolview.go`,
`internal/tui/subagentblock_test.go`

**Tests.** `TestFailedDelegationPaintsItsSlotRed` still red, no ✓; new: a succeeded
delegation with summary text `error: none found` is not red and wears ✓; a refused
delegation is red and wears no ✓.

**Acceptance.** `go test ./internal/tui/ -run 'Delegation|SubAgent|Refus'`

**Commit.** `fix(tui): a delegation row's colour and tick both follow the outcome, not the report text`

## 12. A never-ran delegation row always wears ▶ and keeps its target — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's half (2) needed no code. Re-verified at 80/110/120 columns: when the guard refuses the promotion the demoted slot holds only the typed phrase and `leaderRow`'s spend order leaves the target its `promoteMinTargetCells`, so the ISSUES claim that a long refusal cuts the target away entirely below ~120 columns does not reproduce at any width. It is pinned instead — by the width table's ≥15-cells assertion and by a new never-ran-delegation arm in `TestPromoteGuardHoldsFifteenCellsOfTarget` (`toolleader_test.go`, the file the item lists).

NOTES (2026-08-28): edited two files beyond the item's Files list, both goldens the fix necessarily moves and neither belonging to another item. `internal/tui/transcript_test.go` — `TestSubAgentStreamStaysInsideItsCollapsedRun`'s collapsed live delegation now wears ▶ (opening it shows the prompt and the railed stream, verified); its assertion about the delegate's words not leaking is untouched. `cmd/apogee/testdata/frames/t15-cancelled-delegation.txt` — the restored interrupted delegation's row gains the same ▶, re-recorded with `go test ./cmd/apogee -update`.

NOTES (2026-08-28): the fix is deliberately the SINGLE block's rule only (`blockHidesWhenCollapsed`, the predicate the item names). A grouped delegation's row answers its own predicate in `renderSubAgentGroup`/`renderGroupMember` and is unchanged, which keeps the queued member inert as `scheduledSubAgentView` documents — see the DEFER line for the gap that leaves.

**What.** ISSUES: *A never-ran delegation whose refusal stays promoted wears no ▶* and *At
80 columns a long refusal clips the target off a never-ran delegation's collapsed row.*
Depends on item 11. (1) `blockHidesWhenCollapsed` (`internal/tui/blockstate.go:156-168`)
answers true for a delegation that never ran and carries a non-empty task — the prompt body
`unframedSubAgentView` (`subagentblock.go:249`) lays out exists at every width, so the
indicator must not depend on the promote guard. (2) When the guard refuses the promotion
(`guardRefuses`, `internal/tui/toolleader.go:88`) the demoted view's slot holds only the
short typed phrase, so the leader row's spend order (`toolleader.go:107-110`) leaves the
target its `promoteMinTargetCells` (15); at widths where the guard admits the refusal, the
refusal stays in the slot (today's wide reading) and ▶ is added. Binding: no unconditional
demote — the wide row keeps saying why it never ran.

**Files:** `internal/tui/blockstate.go`, `internal/tui/subagentblock.go`,
`internal/tui/subagentblock_test.go`, `internal/tui/toolleader_test.go`

**Tests.** A width table {80, 110, 120} over `refusedDelegation` (`subagentblock_test.go:1099`):
the collapsed row shows ≥15 cells of the target and ▶ at every width; expanding shows the
prompt body and the refusal; `TestUnframedSubAgentShowsThePromptWhenExpanded` goldens
updated only where the ▶ appears.

**Acceptance.** `go test ./internal/tui/ -run 'SubAgent|Unframed|Leader|Promote'`

**Commit.** `fix(tui): a never-ran delegation row is expandable at every width and keeps its target`

## 13. The settings `auto` row keeps its `(current)` marker at 80 columns — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `settingsModeEditModel` gained the engine-overlaid rows and the mode-moving apply of its sibling `settingsModeModel` (the file's own house wiring) beyond the item's literal "parameterise by width" — the marker and the sentence share ONE cell only when `auto` is the HELD rung, and the helper's fixed `ask-before` row could never hold it, so no test at any width could reach the composition this item changes.

NOTES (2026-08-28): the item says `TestSettingsEnumAutoRowCarriesTheBlastRadiusCell` holds unchanged at 160 "(sentence, then marker)"; it holds unchanged, but it never carried a marker on the `auto` row (its `(current)` sits on the boot rung). The wide reading is pinned instead by the `wide` arm of the new `TestSettingsEnumCurrentMarkerSurvivesANarrowColumn`, which takes the auto rung first and then asserts the composed cell exactly.

NOTES (2026-08-28): "narrow widths" is resolved by MEASUREMENT rather than by a fixed column count — `settingsEnumCellWidth` and `settingsNoteWidth` compute what the sub-list's cell and the key list's note column actually have at this terminal width (the same `popupInnerWidth`/`popupColumnWidths` arithmetic the painter then spends), so the threshold cannot drift as the key list gains rows or the vocabulary gains a longer word.

**What.** ISSUES: *The auto blast-radius row truncates at 80 columns.* In
`renderSettingsEnum` (`internal/tui/settings.go:1775-1789`): when the composed cell
(`sentence + " (current)"`) would exceed the sub-list's row width (the width
`renderSettingsSubList` at `:1747` hands `renderList`), render `"(current) " + sentence`
so the marker survives the `truncateToWidth` at `internal/tui/popup.go:804`; otherwise the
landed order stands. The post-⏎ note (`internal/tui/settingsapply.go:206`, cell 4 of
`settingRowCells` `:1416`) is shortened at narrow widths to the sentence's first clause —
`autoBlastRadiusLine` (`internal/tui/confine.go:89`) gains a `short` form ending before the
first comma — so it, too, ends without an ellipsis. Document the threshold beside
`settingsEnumValueCell` (`:1799`).

**Files:** `internal/tui/settings.go`, `internal/tui/settingsapply.go`,
`internal/tui/confine.go`, `internal/tui/settings_test.go`

**Tests.** Parameterise `settingsModeEditModel` (`settings_test.go:1195-1201`) by width: at
160 `TestSettingsEnumAutoRowCarriesTheBlastRadiusCell` holds unchanged (sentence, then
marker); at 80 the `auto` row line starts with `(current)` when auto is held, contains no
`(curren…` fragment, and the post-⏎ note carries no `…`.

**Acceptance.** `go test ./internal/tui/ -run 'Settings.*(Enum|Mode|Auto|BlastRadius)'`

**Commit.** `fix(tui): the auto blast-radius row keeps its (current) marker on an 80-column terminal`

## 14. `settingsPersistedValue` answers `mode` from the live engine — ✅ DONE (2026-08-28)

NOTES (2026-08-28): added a reciprocal sentence to `settingsCurrentValue`'s doc comment (same file, three lines) beyond the item's literal "with a doc comment stating that `mode` has no persisted-value reading" — the two methods now call each other for `mode`, and the note names the branch that answers without consulting the journal as what keeps the deferral terminating.

**What.** ISSUES: *`settingsPersistedValue` still answers `mode` from the journal.*
Re-verification corrects the entry: its three callers (`internal/tui/settings.go:579`,
`:783`, `:1132`) never reach it for `mode` today (`settingsCurrentValue` short-circuits at
`:1129-1131`; the reset path reads `row.Default` and `settingEditOf` directly). Close it as
defence-in-depth: `settingsPersistedValue` (`:1000`) returns `settingsCurrentValue`'s live
answer for `settingKeyMode` before consulting the journal, with a doc comment stating that
`mode` has no persisted-value reading — the engine is the only authority (ADR 0037).

**Files:** `internal/tui/settings.go`, `internal/tui/settings_test.go`

**Tests.** With a journaled `mode` edit and a different live engine mode,
`settingsPersistedValue(modeRow)` returns the live mode.

**Acceptance.** `go test ./internal/tui/ -run 'Settings.*(Persisted|Current|Mode)'`

**Commit.** `fix(tui): the settings pane never answers mode from its own journal`

## 15. An in-TUI `/sessions` restore starts with an empty spent-skills set — ✅ DONE (2026-08-28)

**What.** ISSUES: *An in-TUI `/sessions` restore inherits the outgoing session's spent
skills.* Add `m.spentSkills = nil` beside `m.liveStats.reset()`
(`internal/tui/sessions.go:573`) — unconditional, on every restore (binding: the `/clear`
rationale at `internal/tui/commandrun.go:146-149` applies — the set falls with the
conversation it advised). Amend the `spentSkills` field doc (`internal/tui/model.go:321-329`)
to name both clearing sites.

**Files:** `internal/tui/sessions.go`, `internal/tui/model.go`,
`internal/tui/suggestband_test.go`

**Tests.** Mirror the `/clear` assertion (`suggestband_test.go:441-443`) over the in-TUI
restore message: a spent skill before the restore is suggested again after it.

**Acceptance.** `go test ./internal/tui/ -run 'Suggest|Spent|Restore'`

**Commit.** `fix(tui): a session restore clears the spent-skills set`

## 16. `renderFileGroup`'s context row is proven escaped — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the two table cases now carry their own file content and their own `t.TempDir()` (the shared `"needle\n"` seed could not serve both); each row is also asserted to END in its expected `:<n>[:-]<text>` suffix, so the header/row alignment is pinned, not just the escaping.

**What.** ISSUES: *`renderFileGroup`'s context-row form is unexercised.* Extend
`TestGrep_Execute_NewlineInAFilenameCannotForgeARow` (`internal/tools/grep_test.go:646`):
its `"context rows"` case seeds `"before\nneedle\nafter\n"` and asserts four lines —
header, `<path>:1-before`, `<path>:2:needle`, `<path>:3-after` — each carrying
`forgingRowSpelling`, so the `%s:%d-%s` branch at `grep.go:393` is the one under test.
Keep the `"plain rows"` case as it is.

**Files:** `internal/tools/grep_test.go`

**Tests.** The extended case; it must fail if `:393` is changed to use `group[0].path`
unescaped (bite check by the verifier).

**Acceptance.** `go test ./internal/tools/ -run 'TestGrep_Execute_NewlineInAFilenameCannotForgeARow'`

**Commit.** `test(tools): grep's context rows carry the escaped filename`

## 17. The bare-name hook test observes argv[0] — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the test skips (rather than fails) when `exec.LookPath("sh")` finds no shell, matching the existing fixture-unavailable skips in this file; the Windows skip is untouched.

**What.** ISSUES: *The bare-name hook-resolution test never observes argv[0].* In
`TestRunHookSubprocessResolvesABareProgramNameToAnAbsolutePath`
(`internal/tools/exec_common_test.go:490`) run `sh -c 'printf %s "$0"'` and assert the
output is an absolute path equal to what `exec.LookPath("sh")` returns (the `$0` canary;
the Windows skip at `:491` stays). Against the pre-fix code the output is `sh`.

**Files:** `internal/tools/exec_common_test.go`

**Tests.** The amended test.

**Acceptance.** `go test ./internal/tools/ -run 'TestRunHookSubprocess'`

**Commit.** `test(tools): the hook subprocess test proves argv[0] became the resolved program`

## 18. `rmFlag` matches a bare `-` again — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's three cases were added verbatim (`/etc` targets) to both home-anchored tables per its text, each with a comment saying why the spelling is pinned there; `rm -- -rf /etc` was verified to be non-matching before and after the change, so "unchanged from today" means it stays untriggered.

**What.** ISSUES: *The `rmFlag` token loses two spellings the old patterns caught.* Change
`rmFlag` (`internal/security/rules.go:54`) to `(?:--?[a-z][a-z-]*|-)`; the trailing `\s+`
in every use keeps `--` from being consumed dash by dash, and the comment at `:52-53` is
updated to say a bare `-` IS a flag token while `--` still is not. Per the ratified call,
`rm rf /etc` stays unmatched.

**Files:** `internal/security/rules.go`, `internal/security/rules_test.go`

**Tests.** In both home-anchored tables (`rules_test.go:315`, `:362`): `rm - -rf /etc` →
`rm-rf-root-home-system`; `rm rf /etc` → no rule; `rm -- -rf /etc` unchanged from today.

**Acceptance.** `go test ./internal/security/ -run 'DangerousRules'`

**Commit.** `fix(security): rm -rf rules match the getopt-permuted "rm - -rf" spelling again`

## 19. The `~/.apogee` rule's hint says "needs approval", not "is refused" — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's Files list named only `internal/security/rules.go` and `rules_test.go`, but the hint is pinned verbatim by the e2e approval suite; leaving it would have broken a green test. Also updated `cmd/apogee/e2e_approval_test.go`'s `forcedFix` constant and regenerated the `t10-forced-pane` golden (`go test ./cmd/apogee -update`) — the shorter hint wraps to four rows instead of five, so the transcript above the pane gains one visible line.

NOTES (2026-08-28): `assertFixWrapsAsOneBlock` compared the hint's last pane row for EQUALITY with the sentence's last word, which only held because the old wording happened to wrap "argument)" onto a row of its own. Relaxed to `strings.HasSuffix`, which is the check's stated claim (the sentence is not clipped); the wrap point itself is the golden's business.

**What.** ISSUES: *The `~/.apogee` rule's Hint still opens with a refusal.* Reword the first
clause of the `write-apogee-control-plane` Hint (`internal/security/rules.go:186-188`) to
neutral Tier-2 wording that reads correctly for both audiences (the approval prompt's remedy
line and the deny result's tail, `internal/agent/resolution.go:490-502`): "a terminal
command naming ~/.apogee needs approval, even for a read; …" — the sanctioned-tools half
stays verbatim.

**Files:** `internal/security/rules.go`, `internal/security/rules_test.go`

**Tests.** `TestDefaultDangerousRules_ApogeeControlPlaneReadHintsTheSanctionedRoute`
(`:260`) asserts the Hint does not contain `is refused` and does contain `needs approval`
and `copy_file`.

**Acceptance.** `go test ./internal/security/ -run 'ControlPlane'`

**Commit.** `fix(security): the ~/.apogee rule's hint states the approval it now asks for`

## 20. `refuseAbsurdObjectCount` ignores `/Size` inside streams — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the xref-`/Size` test builds the dictionary immediately before its own
`stream … endstream` (the modern xref-stream shape) rather than a bare dictionary, which pins the
exclusion's boundary — a dictionary introducing a stream stays scanned — more tightly than a
`/Size` with no stream anywhere near it.

NOTES (2026-08-28): a `stream` keyword with no `endstream` after it opens no skipped span, so a
truncated stream cannot hide the trailer from the guard; recorded in `withoutStreamBodies`' doc
comment.

**What.** ISSUES: *`refuseAbsurdObjectCount` scans raw bytes.* Keep the whole-file scan
(`internal/doctext/pdf.go:378`, regex `:372`) but skip every `stream … endstream` span
before matching, in one bounded pass (binding — option (ii): trailer and xref-stream
dictionaries stay covered, compressed content cannot trip the guard). Document the
exclusion in the comment at `:367-371`.

**Files:** `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`

**Tests.** Using `hostilePDF`/`contentStream` (`pdf_test.go:216`, `:238`): a valid PDF whose
content stream contains the bytes `/Size 4000000000` extracts normally; the existing
`TestExtractPDF_RefusesAnAbsurdXrefSize` (`:384`) still refuses; a `/Size` in an
`/Type /XRef` object dictionary outside any stream still refuses.

**Acceptance.** `go test ./internal/doctext/ -run 'PDF'`

**Commit.** `fix(doctext): the absurd-object-count guard no longer reads /Size out of a stream body`

## 21. `WaitDelay` bounds the MCP stdio child's drain — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `buildTransport`/`buildStdioTransport` gained a fifth return value (the
`context.CancelFunc`), so `internal/mcp/transport_test.go` — not in the item's Files list — was
updated at its seven call sites (one extra `_`); mechanical, no behaviour asserted there changed.

NOTES (2026-08-28): added `stdioTerminateDuration`, a package var defaulting to zero (the SDK's own
5s default, so production is unchanged) that sets `CommandTransport.TerminateDuration`. Without that
seam the wedged-server test would spend ~10s in the SDK's stdin-close → SIGTERM → SIGKILL ladder;
with it the test runs in 0.2s. Same var-as-test-seam precedent as `platform.ProcessWaitDelay` and
this package's `proxyForRequest`.

NOTES (2026-08-28): the item's prescribed end-to-end test does not discriminate the fix — the SDK's
ladder ends in a `SIGKILL` of the leader, which bounds a wedged server with or without the cancel
(verified by neutering `s.cancel()` and re-running: still green). It is kept as the behavioural pin
(bounded `Close`, no goroutine left in `cmd.Wait`) and joined by
`TestBuildStdioTransport_CancelArmsTheCmdsTeardown`, which starts the wedged fixture directly and
proves the returned cancel is what makes `cmd.Wait` return — that one fails (7s timeout) when
`buildStdioTransport` is reverted to `context.Background`.

NOTES (2026-08-28): `docs/design/mcp-client.md` gained one clause on the stdio bullet (the
session-scoped cancellable context and the bounded drain) — not in the item's Files list, but the
doc states that bullet's teardown contract.

**What.** ISSUES: *`WaitDelay` wired by `NewProcessTeardown` is inert on the MCP `Cmd`.*
`buildStdioTransport` (`internal/mcp/transport.go:149`) builds the `Cmd` on
`ctx, cancel := context.WithCancel(context.Background())` and returns `cancel`; `liveSession`
(`internal/mcp/client.go:49-53`) keeps it; `Client.Close` (`:184`) calls it after
`s.session.Close()` returns and before `reapProcess` (`:208`), so `cmd.Cancel` (the process-
group kill) fires and `WaitDelay` (`internal/platform/teardown.go:91`) bounds the rest.
Update the comment at `transport.go:126-129` (the context is still never the connect ctx)
and `teardown.go:85-91` (the drain bound now applies to MCP). Binding: cancel-at-Close,
never a `Connect`-scoped owner.

**Files:** `internal/mcp/transport.go`, `internal/mcp/client.go`,
`internal/platform/teardown.go`, `internal/mcp/mcp_test.go`

**Tests.** Beside `TestClose_ReapsTheStdioServersDescendants` (`mcp_test.go:278`): a stub
stdio server that ignores stdin-close and SIGTERM and keeps stdout open returns from
`Client.Close` within `ProcessWaitDelay` plus the SDK's `terminateDuration` (set
`ProcessWaitDelay` low for the test) with no goroutine left in `cmd.Wait`.

**Acceptance.** `go build ./... && go test ./internal/mcp/ ./internal/platform/`

**Commit.** `fix(mcp): the stdio server's post-close drain is bounded by WaitDelay`

## 22. Every `ResidualNotice` print is driven in both directions — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's first half was already landed — `wire_boot.go` has routed through `newConfiner()` since `b32e25b0` (plan `2026-08-28 - 01`, its own recorded NOTES), so this item moved the seam and built the coverage rather than doing the routing again; `wire_boot.go`'s comment about where the seam lives was updated to match the move.

NOTES (2026-08-28): the item says the seam moves "beside the other process-wide test seams" in `wire.go`, but `wire.go` held none — its only package-level var was the `tui.Engine` compile-time assertion. The seam landed under a new `The process-wide seams` section header there, and `wire.go`'s package doc gained the matching phrase.

NOTES (2026-08-28): `TestRunRootConfinementStartupNotices` was rewritten rather than extended. Its expectations were derived from `platform.NewConfiner().Capabilities()`, so on any one host only one cell was ever exercised; making the residual cell deterministic meant dictating the caps through the seam, which makes all five cells deterministic. The now-unused `internal/platform` import was dropped from `wire_test.go` as a consequence.

NOTES (2026-08-28): each of the three prints was deleted in turn and the matching test confirmed to fail (TUI, headless, daemon), then restored.

**What.** ISSUES: *Two of the three `ResidualNotice` prints still have no test.* Route
`cmd/apogee/wire_boot.go:102` through the existing `var newConfiner = platform.NewConfiner`
seam (`cmd/apogee/headless.go:116`) so all three prints (`headless.go:321`,
`wire_boot.go:290`, `daemon.go:230`) share one injectable confiner; the seam moves to
`cmd/apogee/wire.go` beside the other process-wide test seams. No production behaviour
changes.

**Files:** `cmd/apogee/wire_boot.go`, `cmd/apogee/wire.go`, `cmd/apogee/headless.go`,
`cmd/apogee/headless_test.go`, `cmd/apogee/wire_test.go`, `cmd/apogee/daemon_test.go`

**Tests.** With `fakeConfiner{caps: {FSWrite: true, Residuals: {"truncate(2)"}}}`
(`headless_test.go:47`): headless prints the notice on stderr with stdout untouched; the
TUI boot notice (`TestRunRootConfinementStartupNotices`, `wire_test.go:817`) carries it;
the daemon prints it. With a residual-free fake, all three print nothing.

**Acceptance.** `go test ./cmd/apogee/ -run 'Residual|Notice|Confinement'`

**Commit.** `test(cmd): the landlock residual notice is driven on every surface without an old kernel`

## 23. The settings editor's production fence root is pinned — ✅ DONE (2026-08-28)

NOTES (2026-08-28): mutation-checked — seeding `""` at the `newExternalEdit` call in `wire_live.go` fails all three assertions, the behavioural one included (the spec hands back an argv).

**What.** ISSUES: *No test pins the settings editor's production fence root.* In
`cmd/apogee/wire_live_test.go`, after `urlGuardWiring(t, config.Options{})` and
`w.wireSession(ctx)`: assert `w.externalEdits.workspace == w.roots.workspace` and that it is
absolute; then the behavioural half — plant an executable under `w.roots.workspace`, point
`w.externalEdits.look` at it and assert `w.externalEdits.spec("mode")` wraps
`security.ErrExecFromWritablePath` (mirrors `settingsedit_test.go:655`), proving the seeded
root is load-bearing.

**Files:** `cmd/apogee/wire_live_test.go`

**Tests.** The new test; seeding `""` at `wire_live.go:280` must fail it.

**Acceptance.** `go test ./cmd/apogee/ -run 'ExternalEdit|Fence'`

**Commit.** `test(cmd): the settings editor's fence root is the wired workspace`

## 24. The firing mount refuses an escaping skill root — ✅ DONE (2026-08-28)

NOTES (2026-08-28): mutation-checked — reverting the `ExtraReadRoots` mount in `wire_firing.go` to `skillProvider.SourceDirs` fails the test. The entry pins `parallel-agents:` and `effort-dialect:` so the composition settles with no discovery round trip.

**What.** ISSUES: *The Firing mount site has no test that would catch a revert to
`SourceDirs`.* In `cmd/apogee/wire_firing_test.go`, plant `outside := t.TempDir()` and
`os.Symlink(outside, filepath.Join(roots.workspace, ".apogee"))`, build the provider on
those `Sources`, call `firingConfig`, and assert the escaping path IS in
`provider.SourceDirs()` and IS NOT in `cfg.ExtraReadRoots()` — the second half is what a
revert of `wire_firing.go:229` to `SourceDirs` breaks. Skip on Windows like the sibling
symlink tests.

**Files:** `cmd/apogee/wire_firing_test.go`

**Tests.** The new test beside `TestFiringConfigSetsEveryUnattendedField` (`:41`).

**Acceptance.** `go test ./cmd/apogee/ -run 'Firing'`

**Commit.** `test(cmd): the firing mount drops an escaping skill root`

## 25. The fail-closed proxy paths are tested — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the "could not be pinned" cases stage `http://proxy.invalid:3128` through each package's injected resolver seam (`WithResolver`) rather than letting the name reach real DNS — the item's own `.invalid` spelling, made hermetic the way every other resolver-dependent test in both files already is.

NOTES (2026-08-28): the item attaches the credential assertion ("NOT the password of a `http://user:pw@…` value") to the resolver-error path only; both paths carry it, because a resolved proxy URL keeps the password in its userinfo and only `Hostname()` standing between it and the message. That is what makes the four tools cases a 2×2 rather than two cases plus two assertions. Mutation-checked in both packages: interpolating the resolver's error, pinning `proxyURL.String()`, dropping the proxy from the pinned set, and failing open on either path each fail a case.

NOTES (2026-08-28): `TestVetEndpoint_TheEgressProxyComesFromTheEnvironment` can only observe its own `t.Setenv` while no earlier non-parallel test in `internal/mcp` has resolved a proxy through the real `http.ProxyFromEnvironment` (net/http caches the environment behind a `sync.Once`). It holds today — every other proxy test swaps the `proxyForRequest` seam — and the constraint plus its failure mode are stated in the test's doc comment so a future serial test that breaks it reads as the cause rather than as a proxy bug.

**What.** ISSUES: *The fail-closed proxy paths carry no committed test.* Tools: pass a
`proxy` func into `newHTTPClient` (`internal/tools/network.go:338`) returning an error →
`errors.Is(err, security.ErrURLBlocked)` and the message contains `not a usable URL` and
NOT the password of a `http://user:pw@…` value; a proxy at `http://proxy.invalid:3128` →
`could not be pinned`. MCP: swap `proxyForRequest` (`internal/mcp/transport.go:193`,
non-parallel) the same two ways over `vetEndpoint` (`:201`); plus one `t.Setenv("HTTPS_PROXY",
"http://proxy.invalid:3128")` test on the MCP path proving the env is the surface.

**Files:** `internal/tools/network_test.go`, `internal/mcp/transport_test.go`

**Tests.** Four tools cases, three MCP cases, beside `TestWebFetch_ProxiedDialPinsTheProxy`
(`network_test.go:887`) and `TestGuardedClient_ProxiedEndpointPinsBothHosts`
(`transport_test.go:261`).

**Acceptance.** `go test ./internal/tools/ -run 'Proxy' && go test ./internal/mcp/ -run 'Proxy|Endpoint'`

**Commit.** `test(egress): an unusable or unpinnable proxy refuses the call on both funnels`

## 26. The JS/TS regex rule admits `>`, `+`, `case`, and refreshes its state on close — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's "`a / b / c` still not a regex" is asserted as a second line of the
new `javascript division follows a closed regex literal` row rather than by editing an existing
row; `TestCheckSyntaxReportsEachBrokenShape` is untouched, as the item requires.

**What.** ISSUES: *The JS/TS regex rule still misreports an arrow body and three other
predecessors* and *A closed regex literal leaves the preceding-rune state stale.* Add `'>'`
and `'+'` to `regexOpeners` (`internal/mechanisms/syntaxengine.go:145-157`) and `case` beside
`return` as an opener keyword (`:161` becomes a set); on the closing `/` at `:226` set
`lastRune = '/'` and close the identifier window so the next `/` reads as division. Bypass
floor: the `syntax` Mechanism must not retry correct code.

**Files:** `internal/mechanisms/syntaxengine.go`, `internal/mechanisms/syntaxengine_test.go`

**Tests.** Rows in `TestCheckSyntaxAcceptsValidCode` (`:15`), one per opener — `=> /'"/`,
`a + /'"/`, `case /'"/:`, and every map rune not yet covered (`,` `{` `:` `&` `|` `?` `;`),
each literal holding both quote characters — plus `foo(/a/ / 2)` accepted and `a / b / c`
still not a regex (`TestCheckSyntaxReportsEachBrokenShape` `:299` stays).

**Acceptance.** `go test ./internal/mechanisms/ -run 'Syntax'`

**Commit.** `fix(mechanisms): the JS regex rule reads an arrow body, + and case as openers and resets after a literal`

## 27. The lenient frontmatter scan keeps a `triggers:` sequence as items — ✅ DONE (2026-08-28)

NOTES (2026-08-28): a `- phrase` continuation line is recorded as an item AND still folded into
the open key's value exactly as before, so only `triggers:` — the sole reader of the items —
changes; leniency for every other key is untouched.

**What.** ISSUES: *The lenient frontmatter scan comma-splits a `triggers:` YAML sequence into
one phrase.* In `scanFrontmatterFields` (`internal/skills/parse.go:239-270`), a continuation
line beginning `- ` under an open key accumulates an item instead of being folded into the
value (the precedent is `stripBlockScalar` at `:272`); the scanner returns items per key
and `splitTriggers` (`:124`) accepts an already-split list. Binding: fix in the scanner,
never by stripping a leading `- ` inside `splitTriggers`.

**Files:** `internal/skills/parse.go`, `internal/skills/parse_test.go`

**Tests.** A `TestParseSkillTriggers` (`:339`) case with an unbalanced quote elsewhere AND a
block-sequence `triggers:` yields the two normalised phrases; the existing lenient CSV case
(`:376-377`) unchanged.

**Acceptance.** `go test ./internal/skills/ -run 'Trigger'`

**Commit.** `fix(skills): a lenient frontmatter scan keeps a triggers sequence item by item`

## 28. The recorder's replay comparison covers the reasoning channel — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `same` compares `thinking` beside `text` in a guard of its own — folding it
into the existing single condition would have pushed that line past 110 columns; the comparison
is still exact and unconditional.

**What.** ISSUES: *The recorder's replay comparison never looks at the reasoning channel.*
In `internal/stubllm/record_test.go`: `replayed` (`:226`) gains `thinking string`; `observe`
(`:251`) gains a `provider.DeltaThinking` arm accumulating `delta.Thinking`; `same` (`:234`)
compares it exactly, as it does `text`.

**Files:** `internal/stubllm/record_test.go`

**Tests.** `TestRecorderReplaysWhatItRecorded` now fails if the recorder drops
`Reasoning` (verifier bite check: blank `turn.Reasoning` at `record.go:425` locally).

**Acceptance.** `go test ./internal/stubllm/ -run 'Recorder'`

**Commit.** `test(stubllm): the replay comparison covers reasoning`

## 29. `CheckLeaks` attributes only goroutines born during its own test — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item's Files list named `leak.go` and `leak_test.go`; the change falsified
three in-repo docs that assert the package-global scan, so `doc.go`'s file-map line,
`driver.go`'s `joinReadLoop` rationale (`:328`) and `docs/design/test-drivers.md` (`:330`, `:420`)
were corrected with it. Item 33's test-drivers.md edit is a different paragraph (`:667-674`).

**What.** ISSUES: *`CheckLeaks` still scans goroutines package-globally.* In
`internal/tuitest/leak.go`: `leakedGoroutines` (`:76`) returns `map[goroutineID]stack`,
keyed on the `goroutine <id> [...]` header of each block; `CheckLeaks` (`:54`) snapshots the
map at entry and its cleanup reports only ids absent from the snapshot. `harnessFrames`,
`timerFrames`, `checkerFrame` and the `leakGrace` poll stay. Document the id-reuse caveat
in the package doc.

**Files:** `internal/tuitest/leak.go`, `internal/tuitest/leak_test.go`

**Tests.** A goroutine leaked BEFORE `CheckLeaks` is called (matching a marker) is not
reported; one started after it is. The in-process driver tests still pass under
`-count=2 -parallel 4`.

**Acceptance.** `go test ./internal/tuitest/ -count=2 -parallel 4`

**Commit.** `fix(tuitest): CheckLeaks reports only the goroutines its own test started`

## 30. e2e: a `/server` switch keeps the session fanning out — ✅ DONE (2026-08-28)

NOTES (2026-08-28): the item names `awaitPane`; no such helper existed, so the new file adds it
(a `WaitFor` over the frame) beside `paneNeverShows` — the negative form the control run needs —
and `frameHas`.

NOTES (2026-08-28): the children BLOCK PERMANENTLY (`hang: 1h`) rather than on a merely slow turn,
and both runs end with `drv.Kill()` (the T-03 shape) instead of a clean quit. A child that can never
answer is what makes the control's "the second delegate is not there" a claim about the cap rather
than a race against the clock; a slow-but-finishing child would let a serial run paint its second
row mid-assertion.

NOTES (2026-08-28): the two fixtures are the parent half and the child half of ONE upstream's
script, joined at load (`fanOutScript`), because a delegation runs against the server its parent is
bound to — there is no second server for the children to talk to. The server the session starts on
plays a one-turn inline script it is never asked; both runs assert its request log stayed empty.

**What.** ISSUES: *No test exercises `/server`'s `switchServer` end-to-end for the cap* and
*No driven test puts more than one delegation live at once* (checklist T-16 step 12). New
`cmd/apogee/e2e_parallel_test.go` modelled on `cmd/apogee/e2e_livestate_test.go:201`: two
stubllm servers; the second `servers:` entry carries `parallel-agents: 2` (binding: pin,
no stub `/props`); the session starts on the first, runs `/server <second>`, then a script
turn emits two independent `sub_agent` calls whose children each block on a slow turn;
`awaitPane` sees two live delegation blocks at once. A control run without the pin sees
them serial. Scripts under `cmd/apogee/testdata/stubllm/`.

**Files:** `cmd/apogee/e2e_parallel_test.go`,
`cmd/apogee/testdata/stubllm/parallel-two-delegations.yaml`,
`cmd/apogee/testdata/stubllm/parallel-child.yaml`

**Tests.** The two e2e runs above; they observe the announced surface (the delegation rows
on screen), not internal state.

**Acceptance.** `go test ./cmd/apogee/ -run 'TestE2E.*Parallel'`

**Commit.** `test(e2e): a /server switch re-follows the fan-out cap — two delegations run at once`

## 31. Under an extra root, a refusal's path spelling is pinned — ✅ DONE (2026-08-28)

**What.** ISSUES: *Under an extra root, a non-escape read refusal now names the RESOLVED
path, not the spelling the model used.* Plan-author call: the resolved spelling is the
contract — the announced-path invariant (plan `2026-08-28 - 01`) governs what a tool
ACCEPTS, not how a refusal quotes a directory. Pin it: `read_file` on a directory reached
through a symlinked extra root refuses with `not a file: <real path>`
(`internal/tools/path_read.go:47`), and state the rule in `readFileErrorMessage`'s doc.

**Files:** `internal/tools/path_read.go`, `internal/tools/path_read_test.go`

**Tests.** The symlinked-extra-root fixture the 2026-08-28 run added, extended with a
directory target; assert the refusal quotes the resolved path and never the symlink
spelling.

**Acceptance.** `go test ./internal/tools/ -run 'ExtraRoot|Symlink|ReadFile'`

**Commit.** `test(tools): an extra-root refusal quotes the resolved path — pinned`

## 32. Stale in-code docs and help text (eight one-line fixes) — ✅ DONE (2026-08-28)

NOTES (2026-08-28): `layout.md`'s sentence gained a trailing clause beyond the item's literal "returns on the next FRAME" — "the hints are never cleared, so no edit is required" — because the prose immediately before it explains WHY the row stands down, and a bare "frame" swap would leave the reader without the reason the ISSUES entry gives for it. The code-side comment (`suggestband.go`) took the literal one-word swap; its surrounding paragraph already carries that reason.

**What.** Doc-only, no behaviour: (a) `internal/agent/agent.go:824` "four levels" →
the eight-name union `domain.EffortOff … domain.EffortMax` (ISSUES: *`SetEffortOverride`'s
doc still says "four levels"*); (b) `internal/security/doc.go:115-117` names
`tools.RunHookSubprocess` as the Mechanism door the inventory covers (*The si-34 exec-site
inventory does not name the hook door*); (c) `internal/agent/treesnapshot.go:106-107`
"repo-local filter-driver refusal" → "repo-local command-config refusal (every repo-local
key whose value is a program git executes)" (*Two docs still describe the git funnel's
guarantee…*, code half); (d) `cmd/apogee/root.go:110-111` drops "hint": "the startup
server's `model:`, else ask the server" (*`--model`'s help still calls … a "hint"*);
(e) `cmd/apogee/e2e_egress_test.go:196` cites `docs/design/mcp-client.md §3; ADR 0012's
2026-07-26 amendment` (*`e2e_egress_test.go` cites the wrong source*); (f)
`internal/tui/suggestband.go:154` and `layout.md:1445-1446` both say the row returns on
the next FRAME once the run is live again (*Two docs say the suggestion row "returns on the
next edit"*); (g) `internal/domain/doc.go:71` "seven" → "eight" (*`internal/domain/doc.go:71`
still counts seven*).

**Files:** `internal/agent/agent.go`, `internal/security/doc.go`,
`internal/agent/treesnapshot.go`, `cmd/apogee/root.go`, `cmd/apogee/e2e_egress_test.go`,
`internal/tui/suggestband.go`, `layout.md`, `internal/domain/doc.go`

**Tests.** None new; `go vet` over the touched packages.

**Acceptance.** `go build ./... && go vet ./internal/agent/ ./internal/security/ ./cmd/apogee/ ./internal/tui/ ./internal/domain/ && ! grep -n 'four levels' internal/agent/agent.go && ! grep -n 'seven variants' internal/domain/doc.go && ! grep -n "model: hint" cmd/apogee/root.go`

**Commit.** `docs(code): eight stale doc comments and one help line corrected`

## 33. ADR and manual amendments (five prose fixes) — ✅ DONE (2026-08-28)

NOTES (2026-08-28): ADR 0060's amendment adds one clause beyond the item's literal text — that leaving with its separator stays what an UNNAMED segment does (decision 5's absence, not narrowness). Without it the corrected sentence would read as though the effort segment can never leave the footer, which is the opposite of decision 5.

NOTES (2026-08-28): ADR 0041 decision 9 took both halves the item names: its inline clause now reads "the ` *` / ` ~` marker pair of decision 8", and the dated amendment sits at the end of that paragraph pointing at decision 8's own amendment, rather than being spliced into the middle of the untouched-and-still-governing list.

NOTES (2026-08-28): the test-drivers.md rewrite names the MCP row's cell ("what a third-party MCP server does with a call") as a limit alongside the egress row's — both rows are tagged T-18, which the item's list covers, and leaving it unclassified would have reproduced the approximate split the item exists to close.

**What.** Doc-only: (a) ADR 0060 D6 (`docs/adr/0060-…:101`) amended, dated, to say the
effort word sits in the left run and truncates with an ellipsis like its neighbours; only
the mode marker drops whole (ratified); (b) ADR 0041 decisions 8 and 9
(`docs/adr/0041-the-config-file-is-watched.md:154-155`, `:165`) amended, dated, to the
` ~` watcher marker (`internal/tui/settings.go:315`) and the last-source-wins rule
(`:313-314`) (ISSUES: *ADR 0041 still says a watcher apply journals the ` *` marker*);
(c) ADR 0056 `:93-94` "repo-local filter-driver refusal" → the command-config wording of
item 32(c) (*Two docs…*, ADR half); (d) `docs/manual/configuration.md:517-521` gains the
profile-load arrival: a `/model` profile load that moves the session likewise arrives at the
new server's cap (`sessionMover.move`, `cmd/apogee/upstream.go:290`) (*The manual documents
the cap following a `/server` switch, never the `/load` case*); (e)
`docs/design/test-drivers.md:667-674` rewritten so the paragraph and the column agree: the
"Not observable" column holds pointer cells (which name an instrument) and limit cells; the
four irreducible claims stay as listed, and the other limit cells (T-13, T-15, T-18, T-25,
T-11, T-22, T-23 prose) are named as accepted limits with their proxy, not as pointers
(*The design doc's "Not observable" split is approximate*).

**Files:** `docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`,
`docs/adr/0041-the-config-file-is-watched.md`,
`docs/adr/0056-terminal-fail-fast-and-session-scratch.md`,
`docs/manual/configuration.md`, `docs/design/test-drivers.md`

**Tests.** None (prose).

**Acceptance.** `! grep -n 'filter-driver refusal' docs/adr/0056-terminal-fail-fast-and-session-scratch.md && ! grep -n 'journals its \` \*\` marker' docs/adr/0041-the-config-file-is-watched.md && grep -n 'profile load' docs/manual/configuration.md && ! grep -n 'drops whole when the footer is too narrow' docs/adr/0060-effort-is-detected-passively-dialected-per-server-and-picked.md`

**Commit.** `docs: ADR 0060/0041/0056, the manual's cap paragraph and the test-drivers taxonomy match the code`

## 34. Strike the nine residual sections from `ISSUES.md`

**What.** Depends on items 1–33. Remove the nine "… residuals — deferred out of the … run"
sections from `ISSUES.md` (from `### Effort detection and the effort picker — residuals…`
through the end of `### Symlinked-skill-reads residuals…`, the separator rules included)
— every entry is closed by an item above, and the closed trail is the closeout's CHANGELOG
write. Also amend the "Audit residue" C-03 bullet: it is closed by item 2 — delete the
bullet (the changelog is the trail). Nothing else in the file changes.

**Files:** `ISSUES.md`

**Tests.** None.

**Acceptance.** `! grep -n 'deferred out of the' ISSUES.md && ! grep -n 'C-03 — unattended runs' ISSUES.md && grep -n '^## Parked / deferred work' ISSUES.md`

**Commit.** `docs(issues): the nine run-residual sections are closed`

## 35. The workspace e2e proves every tool touched the real tree

**What.** Residual of plan `2026-08-28 - 01` item 6, never recorded in `ISSUES.md`: in
`TestE2EAnnouncedWorkspace` only the `read_file` and `terminal` receipts prove their content
came from the real tree; the `list_dir` and the two write receipts are asserted on their own
text alone (`cmd/apogee/e2e_announced_test.go:373-395`), and `b.txt` is read back from disk
separately at `:397-403`. Tighten the three weak receipts: seed the real tree with a
uniquely-named third file before the run and assert the `list_dir` receipt names it (a
listing cannot invent it), and read `a.txt` and `b.txt` back from `tree` after the run,
asserting the `write_file` and `edit_existing_file` receipts against the bytes actually on
disk rather than against the receipt wording. The announced-spelling assertions
(`assertEveryToolCallNames`) and the zero-prompt assertion are unchanged.

**Files:** `cmd/apogee/e2e_announced_test.go`,
`cmd/apogee/testdata/stubllm/announced-workspace.yaml`

**Tests.** `TestE2EAnnouncedWorkspace`, extended — mutation-check by pointing the read-back
at a different tree, which must fail.

**Acceptance.** `go vet ./cmd/apogee && go test ./cmd/apogee -run 'E2EAnnouncedWorkspace' -count=1`

**Commit.** `test(e2e): the workspace run's list and write receipts are checked against the real tree`

---

## Suggested version bump

Minor (`0.19.0`): two Driver-parity fixes on the wire (items 2, 7), a new transcript-wire
member (item 10), three security-rule and guard corrections (items 18–20) and the MCP
teardown change (item 21) are user-visible behaviour, not patches. The owner decides; no
item bumps anything.
