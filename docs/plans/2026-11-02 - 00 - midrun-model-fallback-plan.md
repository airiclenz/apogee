# Mid-run model fallback — plan

**Goal:** a `servers:` entry can name fallback servers; when that entry's server fails mid-Turn, apogee waits out a grace window, then rehomes the seat to the first live fallback and resends the failed request, so the Turn continues. Every Driver gets it; a config without `fallback:` behaves exactly as today.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 81b692b1
**Closes:** midrun-model-fallback
**Sources:**
- `.capcom/issues/midrun-model-fallback.md`; Seqcom request `apogee-midrun-fallback` (run 20261031T1223Z)
- ADR 0085 (`adr-0085-future`: fallback pools), ADR 0028 (+amend-0813), ADR 0082, ADR 0047, ADR 0018, ADR 0031, ADR 0075 D10, ADR 0103, ADR 0066, ADR 0069
- Glossary: Upstream, Upstream attempt, Heartbeat, Driver, Event lines, Event journal, Delegation seat, Delegation target, Sub-agent server

**Ratified design calls** (owner, 2026-11-02 grill):
- **Scope:** engine-level; every Driver (TUI, `apogee daemon`, `apogee headless`) gets it.
- **Opt-in:** off unless configured; no `fallback:` → today's behaviour, byte for byte.
- **Config:** per-`servers:`-entry `fallback: [names]` (ordered) and `fallback-after: <duration>` (default `2m`); `--endpoint` one-run entries have none.
- **Trigger:** only where a Turn would be abandoned today for an upstream fault — transport/connection failure, timeout or idle cut, HTTP 5xx, 429, 529, a cut stream after the ADR 0082 re-stream budget. Any other 4xx (400, 401, 403, 413, …), overflow, empty or capped replies never trigger.
- **Grace:** once triggered, probe the primary's heartbeat for up to `fallback-after:`; recovered → resend on the primary, no switch.
- **Mid-Turn:** discard the partial response, rehome the seat (one beat binds the fallback's model), resend the failed request; a smaller window falls to the ADR 0018 fold.
- **One-way, per seat:** first agent to trigger on a seat runs grace+switch; every agent on that seat moves; never switches back by itself. A routed child (sub-agents-server) follows its own entry's list.
- **List walk:** failed seat's list in order, one beat each, no grace, no chaining (a fallback's own `fallback:` ignored); all down → abandoned as today (headless exit 3).
- **Bad config:** unknown name, self, or duplicate in `fallback:` refused at load (headless exit 2; TUI/daemon config error naming the entry).
- **Reporting:** new `upstream_switched` Event kind {from server/model, to server/model, fault, waited}, additive in event-line `v:2`, in the Event journal; TUI note, headless stderr line, daemon log line. Exit codes unchanged.
- **TUI:** switches automatically; `/server` moves back.
- **Resume:** starts on the configured primary (ADR 0028 D5); the record's model reads the fallback's after a switch.
- **Bench arm:** none — not a Reaction; acts only after the primary already failed.
- **Design record:** item 1 writes a new ADR realising ADR 0085's future Note, with dated amendments to ADR 0028 and ADR 0085.

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi: never `go test -race` over a whole package or pattern; one test only (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`); never two test runs at once; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`. Pass this to every sub-agent.
- Waits go through conditions with generous backstops (slow-box rule); test durations through `internal/testtime`.

**Out of scope:** switching back to the primary; fallback on model-quality signals; a bench arm; a headless flag for `fallback:`; answering the Seqcom request (owner action after release).

**Regression check (2026-11-02, 81b692b1):**
- 1: guard folded (ADR 0045, ADR 0075 and ADR 0085 amendments in Acceptance; `Server fallback:` title; how it differs from `SeatFallbackNote`)
- 2: guard folded (`fallback:` as a comparable value type, so `ServerEntry` keeps `!=`; manual greps in Acceptance)
- 3: guard folded (read faults classified in both codecs' `parseSSE`; the anthropic `overloaded_error` slug counts as 529)
- 4: guard folded (foldCases row, headless JSON stub, alias test and count prose; the count is amended by item 1's `adr-0075-amend-1102`)
- 5: guard folded (`sharedUpstream` naming; ownership stays per Agent; `SwitchUpstream` installs a fresh holder)
- 6: recast (a cancel returns `turnCancelled`; `StreamResetEvent` before the resend; the resolver answer carries a `RebindSpec`; fallen-over state in the resolver; public aliases)
- 7: recast (switch returned to `step` as a new outcome; `RebindSpec` applied; per-agent switch generation; Driver-read cfg fields guarded)
- 8: guard folded (spec resolved as `sessionMover.move` does; `ResolveWithin` keys; bounded recovered predicate; text/json reporting; sanitized notice)
- 9: guard folded (only Depth-0 switches set the record's model; daemonfire_test harness; TUI clause dropped; sanitized log line)
- 10: guard folded (Depth-0 fold updates opts and observed model; resolver follows the bound entry; `seedOptions`)
- 11: recast (re-point `delegationWiring`'s server at the fallback; resolver attached in `resolveFiringRouting` too; one resolver per Sub-agents seat); supersedes ADR 0045 decision 4 via `adr-0045-amend-1102`
- 4 (re-check): recast (`Seat` member, event-line `seat`, `"session"` / `"sub-agents"`)
- 5 (re-check): recast (the holder carries the generation counter of item 6 guard (b))
- 6 (re-check): recast (one home per piece of state: single-flight and fallen-over state in the Driver-built resolver instance per seat with a `first` flag; generation-only `sharedUpstream`; one event per seat); the `/server`-installs-a-fresh-resolver clause dropped
- 7 (re-check): guard folded (`Seat` on the event; per-request generation; binding re-applied at the top of `step()`, child inherits the applied generation; accessor rule with grep, `VisionRefusal`/`ServerEphemeral` in scope)
- 8 (re-check): guard folded (resolver per item 6 guard (a)–(c); single-flight test)
- 9 (re-check): recast (record model and daemon wording gated on `Seat == "session"`, not Depth)
- 10 (re-check): recast (heartbeat-follow tail and fold gated on `Seat == "session"`; note shows both seats)
- 11 (re-check): guard folded (one shared Sub-agents resolver per item 6 guard (a)–(c)); yields to ADR 0069 (no `SetDelegationSeat` on fallover)
- 6 (re-check, completion): guard folded (at most one recovered resend per Turn, the engine's recovered-spent signal to the resolver, flap test; the shared run on a detached context)
- 8 (re-check, completion): guard folded (reads item 6's recovered-spent signal to skip grace; shared work on the detached context; cancel-the-runner test)
- 11 (re-check, completion): guard folded (`d.target` re-pointed on fallover, the fallback's target pushed into the latch, a fresh resolver on `Retarget`/relist; relist-after-fallover test)

## 1. Design record: ADR for per-entry server fallback — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the new record is ADR 0105 (next free number after adr-0104); each of the four amendments is named on its record's Status line and appended to its reading-order sentence; the Upstream Note links the new `Server fallback` term in prose.

NOTES (2026-11-02): read item 11's section (and item 4's Seat member) because item 1's regression guard points there for the content of `adr-0045-amend-1102` and `adr-0075-amend-1102`; no edits outside item 1's Files.

NOTES (2026-11-02): consequential edit — .capcom/knowledge/decisions.md: made necessary by the new adr-0105 record (the decisions index lists every record; added "- `adr-0105` — A server entry may name fallback servers, and a failed seat falls over one-way" after the adr-0104 line)

**What:**
**Goal:** a new decision record (next free `adr-NNNN`) records every ratified call above; ADR 0028 and ADR 0085 each carry a dated amendment naming it; the glossary has a `Server fallback` Note.
**Approach (assumed at the header base):** `capcom know add --parent decisions` for the record, each section a child Note, house shape as `capcom know show "ADR 0104" --with-children`. It realises `adr-0085-future` (fallback pools behind the Responder seam, ADR 0031's wire-silent engine kept: the engine receives an injected resolver and `UpstreamSpec`s, never config); cites ADR 0047 (key-source chain rejection does not apply: this chains servers, not keys) and ADR 0082 (trigger after its re-stream budget). Amendments: `adr-0028-amend-1102` (a mid-Turn rehome is allowed for fallback only; `/server` and `/model` stay idle-only) and `adr-0085-amend-1102` (the future Note is realised), each named on its record's Status line and reading-order list. Glossary Note under `glossary-identity`, linked from `Upstream`.
**Regression guard.** Also add the ADR 0045 amendment (see 11) and make the `Server fallback` glossary Note state how it differs from a Delegation seat falling back to the session's Upstream (`SeatFallbackNote`). `adr-0045-amend-1102` covers decision 4's fallen-over re-latch. `adr-0075-amend-1102` records the 26th line kind, `upstream_switched`, added within `v:2`. That supersedes decision 4's "twenty-five" (adr-0075-decision.md:46) by amendment, not by rewrite. Name each amendment on its record's Status line and reading-order list. The glossary Note's title starts `Server fallback:` (as `Upstream: …` does).
**Files:** .capcom/knowledge/decisions/ (new record and children; adr-0028, adr-0085, adr-0045, adr-0075 amendments); .capcom/knowledge/glossary/glossary-identity/
**Read first:** .capcom/knowledge/decisions/adr-0085/adr-0085-future.md — fallback pools future Note; .capcom/knowledge/decisions/adr-0028.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0085.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0104.md — house record shape, amend children;
.capcom/knowledge/glossary/glossary-identity/upstream.md — Upstream Note (link to the new term); .capcom/knowledge/decisions/adr-0075/adr-0075-decision.md — decision 4 kind count
**Tests:** `capcom check`
**Acceptance:** `capcom check` reports no problems; `capcom know show "ADR 0028" --with-children | grep -c amend-1102` ≥ 1, and the same for ADR 0085, ADR 0045 and ADR 0075; `capcom know show "Server fallback"` prints the Note.
**Commit:** `docs(adr): record per-entry server fallback`

## 2. Config: `fallback:` and `fallback-after:` on a server entry — ✅ DONE (2026-11-02)

NOTES (2026-11-02): `FallbackList` holds the canonical JSON array of trimmed names (not a separator-joined string) because a server name is free text, so no separator is safe; it mirrors `RequestExtra` (own `UnmarshalYAML`/`MarshalYAML`, string kind so the unknown-key walk does not descend), with `NewFallbackList(names...)` and `Names()` for later items to reuse. `ServerEntry` stays comparable; the existing `!=` sites compile unchanged.

NOTES (2026-11-02): the decoder also refuses shapes the plan did not enumerate — a lone scalar, a mapping, an empty list, `null`, and a null/blank/nested item — on the `request-extra:`/`price:` precedent; a `fallback-after:` with no `fallback:` list is accepted (never read), and a whitespace-only `fallback-after:` is refused as `stream-idle-timeout:`'s is.

NOTES (2026-11-02): `ParseFallbackAfter`/`ResolveFallbackAfter` live in config.go (beside `ResolveStreamIdleTimeout`) rather than registry.go, since `fallback-after:` is an entry key, not a registry row; `ResolveFallbackAfter` has no production caller yet — it is for the resolver of item 8.

**What:**
**Goal:** a `servers:` entry accepts `fallback:` (list of server names) and `fallback-after:` (a duration, default `2m`, `0` or negative refused); `ValidateServers` refuses an unknown name, the entry's own name, or a duplicate, naming the entry and the bad name; a one-run `--endpoint` entry carries no fallback; the manual documents both keys.
**Approach (assumed at the header base):** extend `config.ServerEntry` and its `UnmarshalYAML`; parse/resolve the duration the way `ParseStreamIdleTimeout`/`ResolveStreamIdleTimeout` do; validation inside `ValidateServers`, so `ResolveOptions`, `settingsApplier.reloadServers` and the key-source write path all get it. Unknown-key detection picks up the yaml tags. Docs: `docs/manual/configuration.md` §"The servers you run models on" — both keys, the trigger classes, grace, one-way, no chaining.
**Regression guard.** `ServerEntry` stays comparable with `!=` — store `fallback:` as a comparable value type (a canonical joined-string newtype with a `Names() []string` accessor), never a slice; every later item that carries the list (domain.Config, DelegationTarget, UpstreamSpec, resolver) reuses that type. It has its own `UnmarshalYAML`/`MarshalYAML`, as `RequestExtra` does. The `!=` invariant at config.go:1629, :1678-1680 and :1754 holds. The `!=` sites in config_test.go (TestApplyConfigHoldsTheStartupEntry, TestServerEntry{StreamIdleTimeout,RequestExtra,Price}RoundTrips) compile unchanged.
**Files:** internal/config/config.go; internal/config/config_test.go; docs/manual/configuration.md
**Read first:** internal/config/config.go — ServerEntry, ServerEntry.UnmarshalYAML, ValidateServers, RequestExtra (comparability), resolveStartupEntry (ephemeral --endpoint entry); internal/config/registry.go — ParseStreamIdleTimeout, validateStreamIdleTimeout;
internal/config/config_test.go — TestServerEntryRequestExtraRoundTrips, TestApplyConfigHoldsTheStartupEntry
**Tests:** table tests for parse (list, duration, default, refusals); `ValidateServers` refusals with the exact error text; a YAML round trip of the `fallback:` type that also compares two entries with `!=`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 ./internal/config/`; `go vet ./internal/config/`; `grep -c 'fallback-after:' docs/manual/configuration.md` ≥ 1 and `grep -c 'fallback:' docs/manual/configuration.md` ≥ 1
**Commit:** `feat(config): fallback and fallback-after keys on a server entry`

## 3. Provider: a fault class on every failed Delta — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the enum is `provider.FaultClass` (string values: transport, timeout, status, cut, in-band, local, cancelled; zero `FaultNone`), carried as `Delta.Fault provider.UpstreamFault{Class, Code, ErrorType}`. The exported type is called `UpstreamFault` so it cannot be confused with the unexported `fault` that `classify` returns. The struct also carries the in-band slug (`ErrorType`) because `FallbackEligible` needs it for `provider_unavailable` and `overloaded_error`.

NOTES (2026-11-02): the item's five classes cover upstream faults only, so two more were added so that every DeltaError gets a class: `FaultLocal`, for the limits apogee enforces itself (marshal failure, text cap, tool-call caps, malformed-only stream, thinking-block encode failure, a line longer than the scanner buffer), and `FaultCancelled`, for a stream the caller's own context ended. Neither is eligible.

NOTES (2026-11-02): the read-fault mapping widens the regression guard's literal "idle → idle, else isTransientReadError → cut". A network timeout on a body read joins the idle cut as `FaultTimeout` (the goal names one "idle/timeout" class). Any non-transient read failure other than bufio.ErrTooLong, such as a connection reset, is `FaultCut` and is eligible, while Retryable stays false for it as before. One helper, `readFault`, does this sorting for both codecs.

NOTES (2026-11-02): a single rule, `isTransientFault(code, errType)`, now decides both classify's retryable verdict and FallbackEligible's status and in-band arms. classify's behaviour is unchanged (TestClassify passes as before), and the rendered "in-band 0:" text is unchanged.

NOTES (2026-11-02): wire_anthropic_stream_test.go was not in Files. Two of its exact-Delta expectations (tool-call count cap, malformed-only stream) gained `Fault: UpstreamFault{Class: FaultLocal}`. This follows directly from the item's new field and is not a change to behaviour.

NOTES (2026-11-02): consequential edit: internal/provider/doc.go. The package map's fault.go line now names UpstreamFault and FallbackEligible, made necessary by the new exported type in fault.go.

**What:**
**Goal:** every error `provider.Delta` carries a fault class that tells apart: transport (connection failure after retries), idle/timeout, HTTP status with its code, cut stream, in-band error with its code. `Retryable` keeps its current meaning on every path.
**Approach (assumed at the header base):** a new exported field on `provider.Delta` (a small enum plus status code), set in `stream.go` where `DeltaError` is built for a `send` failure, in `statusDelta`, in `inBandErrorDelta`, and on the `isTransientReadError` / `errStreamIdle` paths. One predicate `FallbackEligible()` on the class, owned by `internal/provider`: transport, idle/timeout, cut, status 5xx/429/529, in-band 5xx/429/529/`provider_unavailable`; everything else false. No engine change in this item.
**Regression guard.** The cut-stream and mid-body idle faults are built in `openaiCodec.parseSSE` (wire_openai.go) and `anthropicCodec.parseSSE` (wire_anthropic_stream.go), so classify the read fault there: `errors.Is(err, errStreamIdle)` → idle, else `isTransientReadError` → cut. `FallbackEligible` also counts the `anthropicOverloaded` slug (an in-band `overloaded_error`, the mid-stream 529) as eligible. `classify` keeps its code at 0, so TestClassify (fault_test.go:116-120) and the rendered "in-band 0:" text are unchanged.
**Files:** internal/provider/stream.go; internal/provider/responder.go; internal/provider/fault.go; internal/provider/wire_openai.go; internal/provider/wire_anthropic_stream.go; internal/provider/stream_test.go
**Read first:** internal/provider/stream.go — Client.Stream, statusDelta, inBandErrorDelta, isTransientReadError; internal/provider/fault.go — classify, fault; internal/provider/wire_openai.go — openaiCodec.parseSSE;
internal/provider/wire_anthropic_stream.go — anthropicCodec.parseSSE, anthropicStream.fail; internal/provider/fault_test.go — TestClassify
**Tests:** a table driving each path through a stub HTTP server (refused connection, 500, 529, 429, 400, 401, 413, cut body, idle cut, in-band error) asserting class and `FallbackEligible()`; anthropic-wire rows for a cut body, an idle cut and an in-band `overloaded_error` (eligible).
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 ./internal/provider/`
**Commit:** `feat(provider): classify upstream faults on the failing delta`

## 4. Event: `upstream_switched` — ✅ DONE (2026-11-02)

NOTES (2026-11-02): `Seat` is typed — `domain.UpstreamSeat` with `UpstreamSeatSession` ("session") and `UpstreamSeatSubAgents` ("sub-agents"), aliased in apogee.go beside the event (house pattern of `SubAgentPhase`); the line's `seat` member carries the plain string. `Waited` travels as `data.waited_ms` (whole milliseconds, as `upstream_attempt`'s clocks), and `Notice()` rounds it to the second.

NOTES (2026-11-02): `TestKindsAreTwentyFive` renamed `TestKindsAreTwentySix`; a `TestUpstreamSwitchedEventNoticeNamesBothServers` test pins the Notice sentence (internal/domain/events_test.go, not on the item's Files line); decode round trip carries both seat values and `lossyView` truncates `Waited` to the millisecond.

NOTES (2026-11-02): consequential edit — internal/eventjson/decode.go: Decode's doc comment on lossy points now names `UpstreamSwitchedEvent.Waited`'s millisecond truncation, made necessary by the new decoder.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** `domain.UpstreamSwitchedEvent` {FromServer, FromModel, ToServer, ToModel, Fault, Waited, Seat} exists with a `Notice()` one-liner; event lines encode and decode it as kind `upstream_switched` under `v:2`; the kind count, the manual's kind list and the public alias list include it.
**Approach (assumed at the header base):** event in `internal/domain/events.go` (house pattern: `MalformedChunksEvent.Notice`); `eventjson` kind constant, `Kinds()`, `encode` switch, `eventDecoders`; bump `TestKindsAreTwentyFive` to the new count (rename it), `TestEncodeMapsEveryEventVariant`, decode round-trip; `docs/manual/headless.md` kind list and its count; the alias list in `apogee.go` and `example_test.go`. Notice text: `apogee: <from-server> failed (<fault>); continuing on <to-server> (<to-model>) after waiting <waited>`.
**Regression guard.** Add a `foldCases` row for `domain.UpstreamSwitchedEvent` in internal/tui/fold_test.go. It is inert for now; item 10 changes it. Append `domain.UpstreamSwitchedEvent{…}` to TestHeadlessFormatJSONStreamsEveryEvent's stub list after `ContextEstimateEvent`, in `Kinds()` order before the frames. Change the "twenty-five" / "twenty-three Event variants" prose (`grep -rn 'twenty-five' internal/eventjson docs/manual`: encode.go:9, encode_test.go, headless.md:266 and :284). ADR 0075 decision 4's count is amended by item 1's `adr-0075-amend-1102`. `UpstreamSwitchedEvent` gains a `Seat` member, event-line member `seat`, value `"session"` (the session's own Upstream, whoever on it faulted — parent or unrouted child) or `"sub-agents"` (a routed Sub-agents seat); encode/decode/round-trip and the manual's kind entry name it.
**Files:** internal/domain/events.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; docs/manual/headless.md; apogee.go; example_test.go; internal/tui/fold_test.go; cmd/apogee/headless_test.go
**Read first:** internal/eventjson/encode.go — Kinds, encode, kind constants; internal/eventjson/decode.go — eventDecoders; internal/eventjson/encode_test.go — TestKindsAreTwentyFive; internal/eventjson/decode_test.go — roundTripEvents, lossyView;
internal/domain/events.go — MalformedChunksEvent.Notice; internal/tui/fold_test.go — foldCases, TestFoldEventCoversEveryEventVariant; cmd/apogee/headless_test.go — TestHeadlessFormatJSONStreamsEveryEvent;
apogee.go — Event variant alias block (apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased)
**Tests:** encode/decode round trip carrying `seat` with each of its two values; `TestManualListsEveryEventLineKind` passes.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 ./internal/eventjson/ ./internal/domain/`; `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualListsEveryEventLineKind|TestHeadlessFormatJSONStreamsEveryEvent' ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -count=1 -run TestFoldEventCoversEveryEventVariant ./internal/tui/`; `GOMEMLIMIT=2GiB go test -count=1 -run TestEveryDomainEventVariantIsAliased .`
**Commit:** `feat(eventjson): upstream_switched event kind`

## 5. Engine: a shared seat upstream for a parent and its unrouted children — ✅ DONE (2026-11-02)

NOTES (2026-11-02): consequential edit — internal/agent/doc.go: made necessary by the new sharedupstream.go (TestDocMapNamesEveryFile requires every file in the doc.go map)

NOTES (2026-11-02): buildAgent and newDelegateAgent now take the *sharedUpstream holder; newAgent keeps its provider.Responder parameter and wraps it, so test callers and resumeAgent are unchanged. The holder exposes responder(), current() (Responder + generation under one lock) and replace() (bumps and returns the generation); reads are nil-safe for bare &Agent{} test literals.

NOTES (2026-11-02): seat_test.go needed no change — its holder-identity comparisons (child.upstream == parent.upstream) still compile and now assert the stronger shared-by-handle fact; the switch/background-host test is named TestSharedUpstreamSwitchLeavesBackgroundHost so the acceptance pattern runs it.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** a parent agent and every child created without a routed Delegation target read their upstream through one shared, mutex-guarded `sharedUpstream` holder, so replacing the holder's upstream is seen by all of them at their next request; a routed child has its own `sharedUpstream`. No behaviour change.
**Approach (assumed at the header base):** a small `sharedUpstream` type in `internal/agent` holding the `provider.Responder`; `Agent.upstream` reads (in `streamResponse`/`collectCompletion`, `SwitchUpstream`, `closeOwnedUpstream`, `SetModel` forwarding) go through it; `newChildAgentOn` shares the parent's `sharedUpstream` instead of copying the pointer; a routed child gets a fresh one. Refactor only — one deep type, no new exported API.
**Regression guard.** The holder is named `sharedUpstream` (file internal/agent/sharedupstream.go, tests `TestSharedUpstream*` in sharedupstream_test.go), never "seat" — `Agent.seat`, `delegationSeat` and the `TestSeat_` tests already own that word; acceptance runs `-run 'TestSharedUpstream|TestSwitchUpstream'`. The holder holds the Responder and carries the generation counter used by item 6's regression guard (b); its unexported replace bumps it. Ownership stays per Agent (`Agent.ownsUpstream`), so an unrouted child's `closeOwnedUpstream` never closes the session's client (seat_test.go:132/255 hold). `SwitchUpstream` installs a fresh `sharedUpstream` for the parent rather than replacing the shared one. `backgroundHost` keeps the holder it copied, so a `/server` during a background workflow leaves that workflow's agents on their old client, as today. The test replaces through the holder's own unexported method. The test files that read or assign `.upstream`/`.ownsUpstream` move to the holder.
**Files:** internal/agent/agent.go; internal/agent/construct.go; internal/agent/background.go; internal/agent/subagent.go; internal/agent/rebind.go; internal/agent/collect.go; internal/agent/sharedupstream.go; internal/agent/sharedupstream_test.go; the agent test files that touch `.upstream`/`.ownsUpstream` (statemachine_test.go, toolresultfloor_test.go, dialer_test.go, routedspawn_test.go, seat_test.go, subagent_test.go, switchupstream_test.go, background_test.go)
**Read first:** internal/agent/subagent.go — newChildAgentOn, delegation.upstreamOwned; internal/agent/construct.go — newAgent, resumeAgent; internal/agent/agent.go — Agent.ownsUpstream, Close, closeOwnedUpstream;
internal/agent/rebind.go — SwitchUpstream, Rebind; internal/agent/collect.go — collectCompletion; internal/agent/background.go — backgroundHost; internal/agent/seat_test.go — spawnOn
**Tests:** the holder's generation advances on each replace; a child created before the parent's `sharedUpstream` is replaced (through its unexported method) streams from the replacement; a routed child does not; an unrouted child's Close leaves the parent's client open; `SwitchUpstream` on the parent leaves a background host on its old client.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSharedUpstream|TestSwitchUpstream' ./internal/agent/`; `go vet ./internal/agent/`
**Commit:** `refactor(agent): share one upstream holder between a parent and its unrouted children`

## 6. Engine: the fallback seam and the grace resend

**What:**
Recast at the regression check (2026-11-02).
Depends on items 3 and 5.
**Goal:** an Agent built with a fallback resolver (new Option, re-exported from `apogee.go`) consults it at the point the Turn would be abandoned for a `FallbackEligible` fault; a resolver answer "primary recovered" resends the same request on the same `sharedUpstream` with a fresh re-stream budget; "none" abandons exactly as today; with no resolver the loop is unchanged.
**Approach (assumed at the header base):** `completion` carries the fault class from the Delta; in `respondAndReview`, before `emitLoopFault`, call the resolver — the Driver-built resolver instance for the seat (guard (a)) holds the single-flight (the first caller runs it, concurrent callers wait for and share its answer) and the "fallen over" state, so a later fault on the fallback goes to the resolver's list remainder, never back; a fault on an older `sharedUpstream` generation skips the resolver (guard (b)). Resolver interface (engine-side, wire-silent): given the fault, returns one of recovered / switch-to `UpstreamSpec` + `RebindSpec` + server label / none, with a `first` flag. A cancel during the wait returns `turnCancelled`, as the re-stream hold-off does.
**Regression guard.** Use `sharedUpstream`, not "seat", in item text and file names. The resolver's "switch" answer carries an `UpstreamSpec` plus a full `RebindSpec` (prompt template, model profile, window, output cap — what `rebindSpecFor` builds) resolved Driver-side after the beat, so the engine reads no config (ADR 0031). One home per piece of state, stated identically in items 6, 8, 10 and 11 — (a) the resolver instance (Driver-built, one per seat per session/Firing, shared by every agent on that seat including all routed siblings) owns the single-flight and the list position / fallen-over state: concurrent or later callers reporting a fault on the same failed upstream get the same cached answer, and the answer carries a `first` flag true only for the caller that ran grace+walk; (b) the engine's `sharedUpstream` owns only a generation counter: a request that faulted on a generation older than the current one resends on the current upstream without consulting the resolver; (c) each `sharedUpstream` swaps itself on a "switch" answer, and only the `first` caller emits the `UpstreamSwitchedEvent`, so N agents on a seat produce one event. A cancel during fallover (runner or waiter) returns `turnCancelled`: rollback and resume, no `ErrorEvent` (loop.go:572-581 holds). A "recovered" resend emits `StreamResetEvent` first, as the re-stream path does. `apogee.go` aliases the resolver interface, its answer type and the fault class beside the Option. Bounded recovery: within one Turn the engine reports to the resolver whether a "recovered" resend has already been spent this Turn (a `t` latch, like `capRetrySpent`); at most one "recovered" resend per Turn; a second eligible fault in the same Turn after a recovered resend skips the grace window and goes straight to the list walk (this is item 8's same-Turn signal — item 8 reads it); a test pins that a primary that flaps (recovers, fails again) cannot loop the Turn. The resolver's shared run (grace + walk) runs on a context detached from the first caller's (`context.WithoutCancel` plus its own `fallback-after`-sized backstop), so one caller's cancellation never fails the waiters; each waiter still honours its own ctx while waiting, and a cancelled waiter stops waiting and returns `turnCancelled` as above.
**Files:** internal/agent/loop.go; internal/agent/collect.go; internal/agent/sharedupstream.go; internal/agent/agent.go; internal/agent/fallback_test.go; apogee.go
**Read first:** internal/agent/loop.go — respondAndReview, emitLoopFault, holdOffRestream, restreamBudget; internal/agent/collect.go — completion, collectCompletion;
internal/agent/rebind.go — SwitchUpstream, UpstreamSpec; internal/agent/background.go — backgroundHost; apogee.go — Option, WithDelegationTarget;
internal/agent/subagent.go — newChildAgentOn
**Tests:** scripted responder failing with each eligible class and with 400/401: resolver called only for eligible; recovered → `StreamResetEvent`, then the second request succeeds; none → `ErrorEvent` and abandoned as today; a cancel during the resolver wait → `turnCancelled`, no `ErrorEvent`; a fault on a generation older than the holder's current one (bumped through its unexported replace) resends on the current upstream without calling the resolver; a primary that flaps (recovers, fails again) cannot loop the Turn — one recovered resend, then the next eligible fault reaches the resolver flagged recovered-spent.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/agent/`
**Commit:** `feat(agent): consult a fallback resolver before abandoning a Turn on an upstream fault`

## 7. Engine: mid-Turn rehome onto the fallback

**What:**
Recast at the regression check (2026-11-02).
Depends on items 4 and 6.
**Goal:** a resolver answer "switch" rehomes the `sharedUpstream` mid-Turn — partial response discarded, new upstream dialled, model bound, request re-armed for the new model (pre-request Moment fires again), re-stream budget reset — and the Turn continues; one `UpstreamSwitchedEvent` is emitted per switch; `/server` and `/model` (`SwitchUpstream`, `Rebind`) still refuse mid-Exchange.
**Approach (assumed at the header base):** an unexported, ungated rehome sharing `SwitchUpstream`'s body (`binding().applyTo`, `dialOptions`, dial, `recalibrate`) plus `SetModel`, then the answer's `RebindSpec` applied as `Rebind` does. `respondAndReview` returns the switch to `step` as a new turn outcome. `step` then runs `restoreDeferred` → rehome → `armRequest` → `predictiveFoldDue`/`refold` → `MomentPreRequest`, mirroring the `foldFolded` branch. A smaller window goes through the existing ADR 0018 predictive guard. The `sharedUpstream` swap makes children on it follow.
**Regression guard.** Use `sharedUpstream` naming; the mid-Turn rehome applies the resolver's `RebindSpec` the way `Rebind` does (applyProfile) after the ungated upstream swap. The `sharedUpstream` carries a switch generation and the binding. Every agent on it re-applies the binding on its own goroutine at the top of `step()`, before `autoPrune` — not at `armRequest`, which runs after autoPrune/autoCompact/composeUserMessage and would judge the boundary fold against the primary's window and spend `t.foldSpent` — so the parent and siblings never speak to the fallback with the primary's window, profile, parsers or effort dialect; a child inherits its parent's APPLIED generation, never the holder's current one. Each request is stamped with the holder's generation; a fault on an older one resends on the current upstream without calling the resolver (item 6 guard (b)). The `UpstreamSwitchedEvent` states the seat that moved, not the emitter's Depth: `Seat: "session"` when the rehomed holder is the session's (parent or unrouted child), `"sub-agents"` for a routed seat (item 4); items 9 and 10 key on it. Every exported Agent accessor a Driver calls during a Turn (`func (a *Agent) [A-Z]` bodies reading `a.cfg`) is guarded for the rehome — `Vision`, `ServerName`, `Profile`, `ThinkingEffort` and `VisionRefusal` (it reads `cfg.ServerEphemeral`, called from Update via clipboard.go and wire_engine.go) among them. The rehome's doc comment names `adr-0028-amend-1102` as the one exception to the idle-only class (rebind.go:150-156; ADR 0011).
**Files:** internal/agent/rebind.go; internal/agent/loop.go; internal/agent/sharedupstream.go; internal/agent/agent.go; internal/agent/subagent.go; internal/agent/fallback_test.go; internal/agent/switchupstream_test.go
**Read first:** internal/agent/loop.go — step, armRequest, refold, predictiveFoldDue, respondAndReview, visionRefusal; internal/agent/rebind.go — Rebind, SwitchUpstream, recalibrate;
internal/agent/agent.go — Vision, ThinkingEffort; cmd/apogee/wire_engine.go — lateEngine.Vision, lateEngine.VisionRefusal; internal/tui/clipboard.go — serverAcceptsImages, visionRefusal;
internal/domain/events.go — EventBase; internal/agent/subagent.go — newChildAgentOn
**Tests:** switch mid-Turn → second request goes to the fallback responder with the fallback's model and `RebindSpec` profile; the pre-request Moment fires again and deferred corrections survive; event fields; a child on the `sharedUpstream` streams from the fallback afterwards; a child triggers → the parent's next request binds the fallback's window and model, and its boundary fold is judged against the fallback's window; an unrouted child's switch emits `Seat: "session"`; N agents on one `sharedUpstream` → one `UpstreamSwitchedEvent`; a sibling sent on the primary that faults after the switch resends on the fallback without calling the resolver; a child spawned after the rehome carries the parent's applied generation; `Vision()` and `VisionRefusal()` reads racing the rehome are clean under `-race`; a routed child does not move; `TestSwitchUpstreamRefusedMidExchange` still passes.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback|TestSwitchUpstream' ./internal/agent/`; `grep -n 'func (a \*Agent) [A-Z]' internal/agent/*.go` — every accessor whose body reads `a.cfg` and that a Driver calls mid-Turn takes the rehome's guard
**Commit:** `feat(agent): rehome the shared upstream onto its fallback mid-Turn and resend`

## 8. Drivers: the shared fallback resolver, wired for headless

**What:**
Depends on items 2 and 7.
**Goal:** `apogee headless` with a primary that dies mid-run and a live `fallback:` server finishes the run on the fallback with exit 0 and reports the switch once: one `upstream_switched` event line under `--format json`, one stderr line (the event's sanitized `Notice()`) under text; with the fallback also down it exits 3 as today; a primary back within `fallback-after:` gets the resend; a bad `fallback:` name exits 2.
**Approach (assumed at the header base):** one resolver in a new `cmd/apogee/fallback.go` beside `dial.go`, built from a server entry and the resolved config: grace = condition wait polling the primary's `Monitor().Beat` up to `fallback-after:`; list walk = per entry, the spec resolved as `sessionMover.move` does, plus one beat (the shape of `resolveDelegationTarget`). Plumbed through `run.Spec` to the agent Option; `firingConfig` supplies it, so the daemon shares it. Headless stderr via `narrationSink` picking up the event's `Notice()`.
**Regression guard.** The shared resolver in cmd/apogee/fallback.go returns the `RebindSpec` built with `rebindSpecFor` for the fallback entry and model; The resolver is implemented per item 6's guard (a)–(c): the single-flight and the list position / fallen-over state live in the resolver instance (one per seat per session/Firing, shared by every agent on that seat), whose cached answer carries `first` true only for the caller that ran grace+walk; none of it lives in the engine beyond the `sharedUpstream` generation. Resolve the `UpstreamSpec` as `sessionMover.move` does, factored into fallback.go so item 10 shares it: `ResolveStreamIdleTimeout`/`ContextWindow`/`WorkingWindow`/`ResponseReserve`, `MaxOutputTokens`, `ServerName`/`Description`, `Ephemeral=false`. Never use bare `bindingOfEntry` → `upstreamSpec`, which states idle cut 0 and an empty server name. The Firing resolver resolves every fallback entry's key with `keys.ResolveWithin(entry, in.roots.workspace)`. A fault after a recovered resend in the same Turn skips grace and goes to the list walk: the resolver reads the engine's same-Turn "recovered already spent" signal (item 6) to skip grace. The shared work (grace + walk) runs on the detached context of item 6 (`context.WithoutCancel` plus its own `fallback-after`-sized backstop); each waiter selects on its own ctx only. `narrationSink` prints the line via `sanitize.StripEscapesToLine(ev.Notice())`, and its line-family doc comment is updated.
**Files:** cmd/apogee/fallback.go; cmd/apogee/fallback_test.go; cmd/apogee/upstream.go; cmd/apogee/wire_firing.go; internal/run/run.go; cmd/apogee/headless.go; cmd/apogee/e2e_fallback_test.go
**Read first:** cmd/apogee/upstream.go — sessionMover.move; cmd/apogee/dial.go — bindingOfEntry, upstreamSpec; cmd/apogee/wire_firing.go — firingConfig, resolveFiringRouting; internal/run/run.go — Spec, Once;
cmd/apogee/headless.go — narrationSink.narrate; cmd/apogee/e2e_upstream_test.go — headlessUpstream; internal/heartbeat/heartbeat.go — Monitor.Beat; internal/agent/rebind.go — UpstreamSpec
**Tests:** concurrent and later callers on one resolver instance reporting a fault on the same failed upstream → one grace+walk, the same cached answer, `first` true for one caller only; e2e with two `stubllm` servers (primary `Close()`d after the first Turn kind; fallback scripted); `testtime.Fixed` grace in config; bad-name config → exit 2; recovered (stub chat 503 once with discovery up → resend on the primary, no switch); a second fault after a recovered resend (the engine's recovered-spent signal set) → list walk without grace; the first caller's ctx cancelled mid-grace → a waiter still receives the run's answer, and a waiter whose own ctx is cancelled abandons its wait; all down (both stubs `Close()`d between requests) → exit 3; text → one stderr line, json → one `upstream_switched` line; the fallback's idle timeout and server name are bound.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback|TestHeadlessFallback' ./cmd/apogee/`
**Commit:** `feat(headless): fall over to a server entry's fallback mid-run`

## 9. Records and daemon: the model after a switch, and the log line

**What:**
Recast at the regression check (2026-11-02).
Depends on item 8.
**Goal:** after a switch, the session record's model is the fallback's (headless, daemon); `apogee daemon` logs one line per switch naming the schedule and the event's notice; a resumed session starts on the configured primary.
**Approach (assumed at the header base):** `run`'s `eventTap` captures `ToModel` from an `UpstreamSwitchedEvent` with `Seat == "session"` into a record-model field of its own that sets the record's `Meta.Model`; the daemon gains a line from the firing's event stream (a sink wrapper beside its existing composition-notice logging in `daemonfire.go`). Resume needs no code (ADR 0028 D5) — a test pins it.
**Regression guard.** The record's `Meta.Model` update and the daemon's model wording are gated on `Seat == "session"`, not on Depth — an unrouted child's fault moves the session seat and must update the record; a `"sub-agents"` switch is logged naming the sub-agents server and never changes the record's model. `tap.model`, the `differingModel` yardstick for `SubAgentUsage.Model`, is left as is. The daemon logs via `sanitize.StripEscapesToLine(ev.Notice())`. `TestDaemonFallback` is built on daemonfire_test.go, not `newDaemonFireHarnessOn`, which repoints every entry at one upstream and executes nothing. It builds its wiring with `run.Once` and per-entry endpoints (primary and fallback stubs). The TUI's record model is item 10's (`host.SetModel`).
**Files:** internal/run/run.go; internal/run/transcript.go; internal/run/run_test.go; cmd/apogee/daemonfire.go; cmd/apogee/daemonfire_test.go; cmd/apogee/e2e_fallback_test.go
**Read first:** internal/run/run.go — eventTap, eventTap.Emit, differingModel, Once; internal/run/transcript.go — transcriptFold.fold; cmd/apogee/daemonfire.go — daemonWiring.fire; cmd/apogee/daemon.go — daemonLog.line;
cmd/apogee/daemonfire_test.go — newDaemonFireHarnessOn, stubRunner; cmd/apogee/wire_firing.go — raise
**Tests:** record model after a `"session"` switch, including one an unrouted child (Depth 1) triggered; a `"sub-agents"` switch leaves the record's model and the child yardstick unchanged and is logged naming the sub-agents server; daemon log line text, escapes stripped; resume after a switched session dials the primary.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 ./internal/run/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDaemonFallback|TestResumeAfterFallback' ./cmd/apogee/`
**Commit:** `feat(daemon): log a server fallback and record the fallback's model`

## 10. TUI: fall over with the heartbeat following, and a transcript note

**What:**
Recast at the regression check (2026-11-02).
Depends on item 8.
**Goal:** in the TUI a mid-Turn fallback continues the Turn, the transcript shows one note naming from/to server and model, the heartbeat thereafter beats the fallback (no `pendingRebind` back to the primary's model), and `/server <primary>` moves back.
**Approach (assumed at the header base):** the TUI wiring hands the same resolver to the engine, added in `lateEngine.seedOptions` (pre-bound path included); on a switch it does what `sessionMover.move` does after `SwitchUpstream`: `holder.Swap(dial, dial.Monitor())`, `host.SetModel(<fallback model>)`, `live.followEntry`, `caps.follow` — sharing that tail with `move`, not copying it. Note folded in `tui/fold.go` like `foldServerSwitch`.
**Regression guard.** The heartbeat-follow tail (`holder.Swap`, `host.SetModel`, `followEntry`, `caps.follow`) is gated on `Seat == "session"`, not on Depth; the transcript note shows both seats, naming the server. `host.SetModel` makes the TUI record's model the fallback's. The fold of a `Seat == "session"` `UpstreamSwitchedEvent` updates `opts.Endpoint`/`HostAlias`/`Model`/`ContextWindow` and refreshes the startup box as `foldServerSwitch` does, without a `RecordChoice`. That keeps `/server <primary>` from answering "already on". It also sets `hb.observedModel`/`observedWindow` to the fallback's, so no second "model changed" note follows. The resolver is the one instance per seat of item 6's guard (a), owning the single-flight and the fallen-over state; the engine holds only the `sharedUpstream` generation. The resolver answers for the entry the session is bound to at fault time (`liveSettings.boundEntry` / holder), and "none" at once for an entry with no `fallback:`.
**Files:** cmd/apogee/upstream.go; cmd/apogee/wire_live.go; cmd/apogee/wire_engine.go; cmd/apogee/fallback.go; internal/tui/fold.go; internal/tui/fold_test.go; internal/tui/heartbeat.go; internal/tui/fallback_test.go
**Read first:** cmd/apogee/upstream.go — sessionMover.move, upstreamHolder.Swap; internal/tui/heartbeat.go — foldServerSwitch, observeBinding, applyRebind, rebindNote; internal/tui/picker.go — switchToServer;
cmd/apogee/wire_engine.go — lateEngine.seedOptions; cmd/apogee/wire_settings.go — liveSettings.followEntry; cmd/apogee/wire_session.go — sessionHost.SetModel
**Tests:** driven TUI test per `docs/design/test-drivers.md`: primary stub closed mid-Turn → note row text, reply from the fallback, next beat targets the fallback without a second "model changed" note; `/server <primary>` afterwards moves back; after `/server C` (no `fallback:`) a fault on C is abandoned as today; an unrouted child's (`Seat: "session"`) switch runs the heartbeat-follow tail; a `"sub-agents"` switch shows the note naming that server and leaves the holder alone.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/tui/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTUIFallback' ./cmd/apogee/`
**Commit:** `feat(tui): fall over to a fallback server mid-Turn with a transcript note`

## 11. Sub-agent seat: a routed child follows its own entry, one-way

**What:**
Recast at the regression check (2026-11-02).
Depends on item 8.
**Goal:** a child routed to `sub-agents-server` falls over by that entry's own `fallback:` list; once that seat has fallen over, the Delegation target's beats watch the fallback entry and never re-latch the dead primary for the rest of the session. An unusable fallback still pushes nil, so spawns use the session's Upstream as today.
**Approach (assumed at the header base):** the resolver is attached where both callers of `resolveDelegationTarget` build the target: `delegationWiring` and `resolveFiringRouting`. It is built from the sub-agents-server entry, and every routed child on the Sub-agents seat shares that one instance. On fallover `delegationWiring`'s server is re-pointed at the fallback entry. The latched `DelegationTarget` carries the resolver to `newChildAgentOn`.
**Regression guard.** Adopt all three G: lines — (i) on fallover, re-point `delegationWiring`'s server at the fallback entry (server swap + generation++) so beats on the fallback still push nil when unusable (spawns then use the session's Upstream as today); item 1 also writes a dated amendment `adr-0045-amend-1102` to ADR 0045 decision 4 for the fallen-over re-latch, named on its Status line and reading-order list; (ii) attach the resolver where both callers of `resolveDelegationTarget` build the target (`delegationWiring` and `resolveFiringRouting` in cmd/apogee/wire_firing.go); add wire_firing.go to Files and a headless routed-fallback test; (iii) routed siblings share the one resolver instance for the Sub-agents seat per item 6's guard (a)–(c): that instance owns the single-flight and the fallen-over state, each routed child's own `sharedUpstream` holds only its generation and swaps itself on a "switch" answer, and only the `first` caller emits, so N siblings run one grace and emit one event (`Seat: "sub-agents"`). ADR 0045 decision 4 ("every unusable beat still pushes nil into the latch", adr-0045-decision.md:51-75) is kept, and its fallen-over re-latch is amended by `adr-0045-amend-1102`. The item yields to ADR 0069 ("the line moves on the human doors and nowhere else", cmd/apogee/delegation.go:730): the fallover calls no `SetDelegationSeat`. Consistent with the single shared resolver instance per Sub-agents seat: the fallover also re-points `d.target` at the fallback name (keeping `d.configured`), so relist's unchanged-key path keeps the fallback rather than reinstalling the primary; it pushes the fallback's target into the latch under `d.mu`, as `Retarget` does, so a routed child spawned before the next beat dials the fallback, not the dead primary; and `Retarget` and a key-moving relist build a fresh resolver, as `/server` does for the session seat, so a seat moved by a human door is never left fallen over.
**Files:** cmd/apogee/delegation.go; cmd/apogee/delegation_test.go; cmd/apogee/wire_firing.go; cmd/apogee/e2e_fallback_test.go; internal/agent/delegationtarget.go; internal/agent/subagent.go
**Read first:** cmd/apogee/delegation.go — delegationWiring.land, relist, Retarget, observe, resolveDelegationTarget, targetName; cmd/apogee/wire_firing.go — resolveFiringRouting;
internal/agent/delegationtarget.go — DelegationTarget, delegationLatch, SetDelegationTarget; internal/agent/subagent.go — newChildAgentOn;
cmd/apogee/naming.go — routedNamingUpstream; cmd/apogee/wire_settings.go — setSubAgentsServer
**Tests:** routed seat primary dies → child continues on its fallback, event names the sub-agents server; the next beat watches the fallback entry; the fallback down too → the beat pushes nil and a spawn uses the session's Upstream; N routed siblings on a dead Sub-agent server → one grace, one event; a headless routed child falls over (`resolveFiringRouting`); after a fallover a `servers:` save runs relist and the target stays the fallback; a routed child spawned between the fallover and the next beat streams from the fallback; `/sub-agents-server <primary>` (`Retarget`) after a fallover gets a fresh resolver that graces the primary again.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDelegationFallback|TestHeadlessRoutedFallback' ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/agent/`
**Commit:** `feat(delegation): routed sub-agent seats fall over by their own entry`
