# Server fallback follow-ups — plan

**Goal:** close the seven gaps Server fallback (ADR 0105) shipped with: Anthropic `api_error` eligibility, background workflows, stale and over-pinned landings, the TUI's post-fallover settle, Firing re-latching and the Sub-agents announcement. A config without `fallback:` behaves exactly as today, except that a mid-stream Anthropic `api_error` is now re-streamed like any other in-band 5xx.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 28fdf0ac
**Sources:**
- `.capcom/issues/`: anthropic-api-error-fallback, background-workflow-no-fallback, fallback-lander-stale-options, routed-rehome-root-pins, fallover-skips-profile-settle, firing-routed-relatch-gap, cancelled-first-silent-switch
- `docs/plans/archived/2026-11-02 - 00 - midrun-model-fallback-plan.md` (items 6–11 and their NOTES)
- ADR 0105 (+ children), ADR 0082 D2, ADR 0089, ADR 0045 (+amend-1102), ADR 0069, ADR 0057, ADR 0060 D8
- Glossary: Server fallback, Delegation seat, Delegation target, Upstream, Driver

**Ratified design calls:**
- **Scope** (owner, 2026-11-02): the four deferred issues plus the three NOTES gaps E/F/G, filed as issues.
- **Launcher on fallover** (owner, 2026-11-02): follows the fallback entry, as `/server` does.
- **Background workflow on a retired holder** (owner, 2026-11-02): abandons as today (ADR 0089); never falls over.
- **Routed rehome reserve** (owner, 2026-11-02): resolved over the parent's share, exactly as a routed spawn.
- **`api_error`** (writer, per ADR 0082 D2 / ADR 0105 D3): mapped to 500 in `classify`; eligible and retryable through the one `isTransientFault` rule.
- **TUI roster settle** (writer, per ADR 0105 D5): a deferred full rebind at the Turn boundary, not a narrow roster-only act.
- **Announcement** (writer): `FallbackAnswer.First` is replaced by a claim-once `Announce`; the first sibling that applies the switch announces.
- **Firing re-latch** (writer): Driver-side, mirroring `delegationWiring.fallover`; no engine-side target on the answer.

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi: never `go test -race` over a whole package or pattern; one `-run` filter per command (`GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestX' ./pkg/`); never two test runs at once; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`. Pass this to every sub-agent.
- Waits go through conditions with generous backstops (slow-box rule); test durations through `internal/testtime`.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).
- ADR amendments: a dated child Note `adr-0105-amend-MMDD` (the run's date; lettered if that id is taken) via `capcom know add --parent adr-0105`, named on ADR 0105's Status line and reading-order list.

**Out of scope:** switching back to the primary; rebuilding the session resolver when a `servers:` save edits the bound entry's own `fallback:` list; a routed rehome's per-model `SystemPrompt`; an in-flight child's tool roster after a rehome (it keeps its spawn-time set).

**Regression check (2026-11-02, 28fdf0ac):**
- 1: guard folded
- 2: guard folded
- 4: guard folded
- 6: guard folded
- 7: decision applied, guard folded; supersedes ADR 0057 bounds bullet 1 for the host-builder exception
- 8: decision applied, guard folded
- 9: guard folded

## 1. Anthropic in-band api_error counts as 500 — ✅ DONE (2026-11-02)

NOTES (2026-11-02): classify maps the slug through a small `slugStatus` switch (rate_limit_error → 429, api_error → 500, else 0) instead of a second `if` beside the rate_limit one, so the two slug-to-status mappings live in one place; behaviour is exactly the plan's.
NOTES (2026-11-02): TestStream_FaultClass keeps a non-eligible anthropic in-band row by adding an `authentication_error` case beside the recast api_error row, mirroring the move of "any other class" to authentication_error in TestAnthropicParseSSE_InBandError.
NOTES (2026-11-02): ADR amendment filed as `adr-0105-amend-1102` (via `capcom know add --parent adr-0105`), named on ADR 0105's Status line and appended to its reading-order list.

**What:**
Closes the defect that a mid-stream Anthropic `api_error` (that wire's 500) is neither retryable nor Server-fallback eligible.
**Goal:** a code-0 in-band `api_error` classifies as code 500: `Delta.Retryable` true, `UpstreamFault.FallbackEligible` true, fault text `in-band error 500:`; every other unlisted slug stays a plain terminal fault.
**Approach (assumed at the header base):** add an `anthropicAPIError = "api_error"` const beside `anthropicOverloaded`/`providerUnavailable` in internal/provider/fault.go; in `classify`, map a code-0 `api_error` to 500 as `rate_limit_error` maps to 429. `isTransientFault` and `FallbackEligible` stay unchanged. Name `api_error` in the doc comments of `fault.retryable`, `isTransientFault`, `FallbackEligible` and stubllm's `anthropicSlug`. docs/manual/configuration.md "What starts a move": add "and its `api_error` as a 500". Write an ADR 0105 amendment: "`api_error` counts as 500" under D3.
**Regression guard.** In the same edit, stubllm's `anthropicSlug` (internal/stubllm/wire_anthropic.go) returns `slugInvalidRequest`, not `slugAPI`, from its tail for an unlisted code below 500, so `api_error` is rendered only for a scripted 500 and a fixture scripting 409/418/422 over wire anthropic stays terminal as it does on the chat wire; its doc comment says so.
**Files:** internal/provider/fault.go; internal/provider/fault_test.go; internal/provider/stream_test.go; internal/provider/wire_anthropic_stream_test.go; internal/stubllm/wire_anthropic.go; internal/stubllm/wire_anthropic_test.go; docs/manual/configuration.md; .capcom/knowledge/decisions/adr-0105.md; .capcom/knowledge/decisions/adr-0105/adr-0105-amend-MMDD.md
**Read first:** internal/provider/fault.go — classify, isTransientFault, UpstreamFault.FallbackEligible; internal/provider/stream.go — inBandErrorDelta; internal/provider/fault_test.go — TestClassify; internal/provider/stream_test.go — TestStream_FaultClass; internal/provider/wire_anthropic_stream_test.go — TestAnthropicParseSSE_InBandError; internal/stubllm/wire_anthropic.go — anthropicSlug
**Tests:** `TestClassify` row "an in-band anthropic api_error is the 500 it stands for"; `TestStream_FaultClass` api_error row recast to `{Class: FaultInBand, Code: 500, ErrorType: "api_error"}`, eligible and retryable; `TestAnthropicParseSSE_InBandError` gains the api_error 500 case and moves "any other class" to `authentication_error`; `TestAnthropicSlugRendersAPIErrorOnlyFor500` (internal/stubllm: 500 → `api_error`, 422 → `invalid_request_error`, 503 → `overloaded_error`).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestClassify|TestStream_FaultClass|TestAnthropicParseSSE_InBandError|TestAnthropicCodecDecodeWholeErrorBody' ./internal/provider/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAnthropicSlug' ./internal/stubllm/`; `capcom check`
**Closes:** anthropic-api-error-fallback
**Commit:** `fix(provider): an Anthropic in-band api_error is the 500 it stands for`

## 2. Background workflow children consult the session resolver — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the amendment id is `adr-0105-amend-1102b` — `adr-0105-amend-1102` was already taken by item 1; it amends ADR 0105 decision 6 (one-way, per seat) and is named on the Status line and the reading-order list.
NOTES (2026-11-02): `sharedUpstream.rehome` now returns a `rehomeOutcome` (landed / follows / refused-retired) instead of a bool, so the retired refusal is distinct from the follow path; `Agent.rehome` maps the refusal to false (fallbackAbandon).
NOTES (2026-11-02): a watcher of the journal (watch.go foldWatchedEvent) still routes a workflow-owned session-seat `upstream_switched` to foldBackgroundEvent, so it writes no fallback note there; the live path notes and follows it. Pre-existing neighbour, not in this item's files.

**What:**
Closes the defect that a session-seat fault in a background workflow child is abandoned even when the entry names a fallback.
**Goal:** a background workflow child on the session's live holder consults the session seat's resolver and falls over with the seat; a child on a holder that `/server` retired abandons as today and never consults the resolver.
**Approach (assumed at the header base):** `backgroundHost` (internal/agent/background.go) copies `fallback: a.fallback`; `newChildAgentOn` inherits it unchanged. `sharedUpstream` (internal/agent/sharedupstream.go) gains a `retired` flag under its own `mu`; `SwitchUpstream` calls `retire()` on the old holder before installing the fresh one; `consultFallback` (loop.go) abandons when the holder is retired. docs/manual/configuration.md: the one-way paragraph names background workflows and the retired-holder exception. Amendment to ADR 0105 recording both, citing ADR 0089.
**Regression guard.** The TUI follows a switch a background child commits: internal/tui/model.go runs `followFallback` and the fallback note for a `Seat == UpstreamSeatSession` `UpstreamSwitchedEvent` before or inside the `m.workflows.owns` early return, which today skips both. `sharedUpstream.rehome` checks `retired` under `s.mu` and returns a distinct refusal that `consultFallback` maps to `fallbackAbandon` — never the `!moved` follow path (rebind.go) — so a background child whose resolve was in flight when `/server` retired its holder abandons rather than emitting a session-seat switch after the human move.
**Files:** internal/agent/background.go; internal/agent/sharedupstream.go; internal/agent/loop.go; internal/agent/rebind.go; internal/agent/fallback_test.go; internal/agent/sharedupstream_test.go; internal/tui/model.go; internal/tui/fallback_test.go; docs/manual/configuration.md; .capcom/knowledge/decisions/adr-0105.md; .capcom/knowledge/decisions/adr-0105/adr-0105-amend-MMDD.md
**Read first:** internal/agent/background.go — backgroundHost; internal/agent/loop.go — consultFallback; internal/agent/sharedupstream.go — sharedUpstream.rehome; internal/agent/rebind.go — SwitchUpstream; internal/agent/fallback_test.go — newSeatRig, stubResolver; internal/agent/sharedupstream_test.go — TestSharedUpstreamSwitchLeavesBackgroundHost; internal/tui/model.go — workflows.owns branch, followFallback call
**Tests:** `TestFallbackBackgroundChildConsultsTheSessionResolver` (`newSeatRig`, `backgroundHost`, one switch event with `Seat == UpstreamSeatSession`, parent follows); `TestFallbackBackgroundChildOnARetiredHolderAbandons` (no resolver call, no switch event, an ErrorEvent); `TestSharedUpstreamSwitchLeavesBackgroundHost` asserts the old holder is retired; `TestFallbackBackgroundChildRetiredMidResolveAbandons` (a blocking `stubResolver` that retires the holder mid-wait: no move, no switch event, an ErrorEvent); `TestFallbackBackgroundSessionSeatSwitchIsFollowed` (internal/tui: a workflow-owned session-seat switch runs `followFallback` and notes it).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/agent/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSharedUpstream|TestSwitchUpstream' ./internal/agent/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallbackBackground' ./internal/tui/`; `capcom check`
**Closes:** background-workflow-no-fallback
**Commit:** `fix(agent): background workflow children fall over with the session seat`

## 3. The TUI fallback walk lands with live options

**What:**
Closes the defect that the TUI session seat's fallback walk reads options snapshotted when its resolver was built, ignoring later `/settings` and `servers:` edits.
**Goal:** a session-seat fallback walk resolves the fallback entry, root pins and per-model facts from the options live at the moment of the walk.
**Approach (assumed at the header base):** in `sessionFallback.resolver()` (cmd/apogee/fallback.go), call `f.lander()` inside the land closure (`f.lander().arrive(ctx, name)`) rather than once at build, mirroring `delegationWiring.seatFallbackFor`. Fix the `resolver` comment. Headless and daemon resolvers (`newFiringFallback`, `newFiringSeatFallback`) keep their fixed per-run options (ADR 0037).
**Files:** cmd/apogee/fallback.go; cmd/apogee/fallback_test.go
**Read first:** cmd/apogee/fallback.go — sessionFallback.resolver, fallbackLander.arrive; cmd/apogee/delegation.go — seatFallbackFor; cmd/apogee/fallback_test.go — newTUIFallbackSeat, TestTUIFallbackFalloverRunsTheHeartbeatTail; cmd/apogee/wire_settings.go — liveSettings.setServers, liveSettings.update; cmd/apogee/upstream.go — sessionMover.fallover
**Tests:** `TestTUIFallbackLandsWithOptionsEditedAfterTheResolverWasBuilt`: build the resolver, edit via `seat.live.setServers` (backup window 16384 → 32768) and `seat.live.update` (WorkingWindow 4096), `Resolve` a primary report; answer's Upstream/Rebind window 32768, working window 4096; `seat.mover.fallover("backup")` reports 32768.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTUIFallback' ./cmd/apogee/`
**Closes:** fallback-lander-stale-options
**Commit:** `fix(fallback): the TUI walk lands with the options live at the walk`

## 4. A rehomed routed child binds as its Delegation target

**What:**
Depends on item 3.
Closes the defect that a routed child rehomed onto a fallback takes the session's root `context-window:`/`working-window:` pins, unlike a routed spawn on the same fallback.
**Goal:** on the Sub-agents seat (TUI and Firings) a rehome binds window, working window and stream-idle timeout as `resolveDelegationTarget` binds a routed target, and the response-reserve share resolves over the parent's share; the session seat still resolves over the root pins.
**Approach (assumed at the header base):** `fallbackLander` (cmd/apogee/fallback.go) gains `routed bool` and a `pins()` method that, when routed, zeroes `contextWindow`/`workingWindow` and keeps `streamIdle`; `arrive` passes `l.pins()` to `entryUpstream`, and `rebind` takes the pins instead of reading `l.opts.ContextWindow`/`ResponseReserve`. The reserve resolves as `ResolveResponseReserve(entry.ResponseReserve, <parent share>)`: in the TUI through a `sessionFallback.routedLander()` handed to `withFallback` in wire_live.go, reading the live share as `rebindInputs` computes it (a small `liveSettings` accessor in wire_settings.go); in Firings `newFiringSeatFallback` sets it as `bindFiringConfig` does. `seatFallbackFor` (delegation.go) and `newFiringSeatFallback` (wire_firing.go) set `routed = true`.
**Regression guard.** When routed and neither the entry's `context-window:` pin nor the observed window is positive, the rehome states the PARENT's window as the last rank (TUI: `live.window()`; Firing: the bound run's window), as a routed spawn keeps it (`DelegationTarget.ContextWindow`) — never 0, which `RebindSpec` applies as present and leaves the child windowless where today it gets the root pin. Parity with the latched target is checked on the effective binding, not field-for-field: `Rebind.MaxContextTokens == target.ContextWindow`, `Upstream.WorkingWindow == target.WorkingWindow`, `Upstream.StreamIdleTimeout == target.StreamIdleTimeout`, and the reserve against the parent share when the entry states none.
**Files:** cmd/apogee/fallback.go; cmd/apogee/delegation.go; cmd/apogee/wire_firing.go; cmd/apogee/wire_live.go; cmd/apogee/wire_settings.go; cmd/apogee/fallback_test.go; cmd/apogee/delegation_test.go
**Read first:** cmd/apogee/fallback.go — fallbackLander.arrive, fallbackLander.rebind, entryUpstream, rootPinsOf; cmd/apogee/delegation.go — seatFallbackFor, resolveDelegationTarget; cmd/apogee/wire_firing.go — newFiringSeatFallback; cmd/apogee/wire_settings.go — liveSettings.rebindInputs
**Tests:** `TestFallbackLanderRoutedBindsAsTheDelegationTarget` (root pins 131072/65536/0.4/5m, fallback pins nothing: parity with `resolveDelegationTarget`; a fallback whose beat reports no window binds the parent's window; `routed=false` still resolves over root); `TestDelegationFallbackRehomeBindsTheLatchedTargetsWindow` (`newSeatFallbackRig` gains root options; the answer's effective binding equals `lastPush(t)`'s target as the guard states); `TestHeadlessRoutedFallbackTakesNoRootPins` (`newFiringSeatFallback(...).land`).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallbackLander|TestDelegationFallback|TestHeadlessRoutedFallback|TestTUIFallback' ./cmd/apogee/`
**Closes:** routed-rehome-root-pins
**Commit:** `fix(delegation): a rehomed routed child binds as its Delegation target`

## 5. A TUI fallover follows the launcher path

**What:**
Depends on item 4.
Part of fallover-skips-profile-settle (launcher half; item 7 closes it).
**Goal:** after a TUI session-seat fallover, the llama-launcher path follows the fallback entry exactly as `/server <fallback>` would, so `/model`, `/stop-server` and `/unload-model` act on the fallback's launcher (or on none when it has none); a profile load still leaves the launcher alone.
**Approach (assumed at the header base):** add `rootWiring.followFallback(name)` (cmd/apogee/wire_verbs.go beside `switchServer`): it calls `w.mover.fallover(name)`, then `w.launcherPath.follow(entry)` with the entry from the landing (`fallover` returns it or `w.fallback.landing(name)`); `serverHost.FollowFallback` (wire_server.go) calls it. Never inside `arrive`, which a profile load also reaches.
**Files:** cmd/apogee/wire_verbs.go; cmd/apogee/wire_server.go; cmd/apogee/upstream.go; cmd/apogee/fallback_test.go
**Read first:** cmd/apogee/wire_verbs.go — rootWiring.switchServer; cmd/apogee/wire_server.go — serverHost.FollowFallback; cmd/apogee/upstream.go — sessionMover.fallover, sessionMover.arrive; cmd/apogee/launcher.go — launcherPath.follow, entryLauncherPath; cmd/apogee/fallback.go — sessionFallback.landing; cmd/apogee/fallback_test.go — newTUIFallbackSeat
**Tests:** `TestTUIFallbackFalloverFollowsTheLauncherPath`: launcher-fronted primary, plain fallback → after fallover the launcher path is empty; launcher-fronted fallback → the path names the fallback's launcher.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTUIFallback' ./cmd/apogee/`
**Commit:** `fix(tui): a fallover follows the fallback entry's launcher`

## 6. A TUI fallover settles the fallback profile at the Turn boundary

**What:**
Depends on item 5.
**Goal:** after a TUI session-seat fallover, at the Turn's end boundary the session runs the same rebind `/server` runs: the fallback profile's `tools:` roster is applied, an effort override the fallback model does not offer is cleared (ADR 0060 D8), profile and hint notices show; the transcript gets no second model-change note.
**Approach (assumed at the header base):** `tui.ServerFallbackResult` (internal/tui/tui.go) gains `ObservedEffort domain.EffortSupport`, filled from the landing's observed beat in `sessionMover.fallover` (cmd/apogee/upstream.go). `tui.Model.followFallback` (internal/tui/heartbeat.go) stashes `pendingRebind = &rebindIntent{model, window, dialect, effort, quietSeed: true}` instead of nil; `releaseEngine` → `applyPendingRebind` runs `rootWiring.rebind` at the boundary. Recast `TestFallbackSessionSeatSwitchIsNotedAndFollowed`'s "no rebind" check. Write an ADR 0105 amendment recording items 5–6 (launcher follow; settle at the boundary because `SwapTools` refuses mid-Exchange).
**Regression guard.** The stashed intent is `{model: result.Model, window: result.ObservedWindow, dialect: result.ObservedDialect, effort: result.ObservedEffort, quietSeed: true}` — the OBSERVED window, never `result.ContextWindow`, because `rootWiring.rebind` records its window as the server's observation (`w.live.observe`) and a pinned window there would make a later `/settings` pin clear bind the pin. The effort clear is TUI-only (`Model.applyRebind`), so it is asserted in internal/tui, not cmd/apogee.
**Files:** internal/tui/tui.go; internal/tui/heartbeat.go; internal/tui/fallback_test.go; cmd/apogee/upstream.go; cmd/apogee/fallback_test.go; .capcom/knowledge/decisions/adr-0105.md; .capcom/knowledge/decisions/adr-0105/adr-0105-amend-MMDD.md
**Read first:** internal/tui/heartbeat.go — Model.followFallback, rebindIntent, Model.applyRebind, Model.applyPendingRebind; cmd/apogee/wire_verbs.go — rootWiring.rebind; cmd/apogee/upstream.go — sessionMover.fallover; internal/tui/fallback_test.go — fakeFollow; cmd/apogee/wire_server_test.go — rosterSwitchWiring
**Tests:** `TestFallbackSessionSeatSwitchSettlesAtTheBoundary` (internal/tui: one rebind at the boundary, none mid-Turn, no duplicate model note; `fakeFollow` answers distinct `ContextWindow`/`ObservedWindow` and the boundary rebind carries the observed one; an `ObservedEffort` whose levels exclude a set override clears it with `effortClearedNote`); `TestTUIFallbackSettlesTheFallbackProfileRoster` (cmd/apogee, `rosterSwitchWiring`/`liveSetHas` shape: the live tool roster equals the fallback profile's after the boundary; `fallover` fills `ObservedEffort` from `landing.observed.EffortSupport`).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallbackSessionSeat' ./internal/tui/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTUIFallback' ./cmd/apogee/`; `capcom check`
**Commit:** `fix(tui): a fallover settles the fallback profile at the Turn boundary`

## 7. A Firing with a Driver-built tool set takes the fallback roster

**What:**
Depends on item 6.
**Goal:** in headless, daemon and session-raised Firings whose tool set the Driver builds (`firingSeatTools` returns a registry), the fallback profile's `tools:` roster is offered from the first request after the rehome, as it already is when the engine owns the tool set.
**Approach (assumed at the header base):** the engine's `applyRehome` → `applyRoster` (internal/agent) skips a tool set it does not own. Give the Driver-built Firing tool set the roster from the rehome at the same point: re-derive the seam the Firing registry exposes for a profile roster (the TUI's `toolSet.setProfileRoster` is the model) and apply the answer's `Rebind.Profile.Tools` from the engine's rehome when the Driver supplied that seam, never from another goroutine mid-Exchange. No change when the engine owns the tool set.
**Regression guard.** the Goal binds the Firing's top-level Agent only; a child already running at the rehome keeps the tool set it was spawned with (every child's set is injected at spawn), and children spawned after the rehome take the fallback roster through the parent. The seam is a Driver-supplied builder on `domain.Config` (e.g. `ToolsForRoster func(domain.ToolRosterDelta) *ToolRegistry`) that `applyRehome` calls when `!ownsToolSet`, set by `firingConfig` only (a closure over the final cfg, after the scratch mount, beside `cfg.Tools = firingSeatTools(...)`) and cleared on `childCfg` in `newChildAgentOn`; the registry never rides on `FallbackAnswer`/`RebindSpec`, and the TUI wiring gets no builder (item 6 owns the TUI settle). This supersedes ADR 0057 bounds bullet 1 and `composesDefaultRoster`'s doc (internal/agent/construct.go) — an injected `Config.Tools` is the host's verbatim — for the host-builder exception, recorded in an ADR 0057 amendment.
**Files:** internal/agent/rebind.go; internal/agent/fallback_test.go; internal/agent/subagent.go; internal/agent/setprofile.go; internal/agent/construct.go; internal/domain/config.go; cmd/apogee/wire_firing.go; cmd/apogee/fallback_test.go; .capcom/knowledge/decisions/adr-0057.md; .capcom/knowledge/decisions/adr-0057/adr-0057-amend-MMDD.md
**Read first:** internal/agent/rebind.go — Agent.applyRehome, Agent.followUpstream; internal/agent/setprofile.go — Agent.applyRoster; internal/agent/construct.go — composesDefaultRoster; internal/agent/subagent.go — newChildAgentOn; cmd/apogee/wire_firing.go — firingConfig, firingSeatTools; internal/agent/fallback_test.go — newSeatRig
**Tests:** `TestFallbackRehomeAppliesTheRosterToADriverToolSet` (internal/agent); `TestFallbackRehomeKeepsANarrowedChildsSubset` (internal/agent: a tools-narrowed unrouted child keeps its subset after the rehome); `TestHeadlessFallbackAppliesTheFallbackRoster` (cmd/apogee: `sub-agents-choice: model` Firing, fallback profile drops a tool; the request after the rehome omits it).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallbackRehome' ./internal/agent/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessFallback' ./cmd/apogee/`
**Closes:** fallover-skips-profile-settle
**Commit:** `fix(fallback): a Firing's own tool set takes the fallback profile roster`

## 8. Firings re-latch the Sub-agents target after a fallover

**What:**
Depends on item 4.
**Goal:** in headless, daemon and session-raised Firings, once the Sub-agents seat has fallen over, a routed child spawned later dials the fallback directly and sends nothing to the primary; an unresolvable fallback target pushes nil (ADR 0045 amend-1102); no `SetDelegationSeat` (ADR 0069).
**Approach (assumed at the header base):** mirror `delegationWiring.fallover`: `newFiringSeatFallback` (cmd/apogee/wire_firing.go) wraps its land with `lander.arrive`; on a landing it computes `resolveDelegationTarget(landing.entry, landing.dial.APIKey, landing.observed, …)`, sets `target.Fallback = resolver`, and pushes it through a late-bind slot (`atomic.Pointer` to a setter, shaped like `projectRunSlot`) carried on `firingRouting`. `run.Spec` (internal/run/run.go) gains `BindDelegationLatch func(set func(*agent.DelegationTarget))`; `Once` calls it with `a.SetDelegationTarget` right after `agent.New`; `raise` wires the slot.
**Regression guard.** item 8's arrive wrapper in newFiringSeatFallback keeps item 4's routed lander (routed=true, parent reserve share) and never rebuilds a plain fallbackLander. `newFiringSeatFallback` keeps its signature (the slot is a field the resolver or `firingRouting` exposes), so item 4's `TestHeadlessRoutedFallbackTakesNoRootPins` call still compiles. The Goal's "an unresolvable fallback target pushes nil" is unreachable here (`arrive` lands only on a Reachable beat naming a model, `resolveDelegationTarget`'s own nil conditions): it stands as a mirror of `delegationWiring.fallover` and no Acceptance checks it.
**Files:** cmd/apogee/wire_firing.go; cmd/apogee/e2e_fallback_test.go; internal/run/run.go; internal/run/run_test.go
**Read first:** cmd/apogee/wire_firing.go — newFiringSeatFallback, firingRouting, projectRunSlot, raise; cmd/apogee/delegation.go — delegationWiring.fallover; internal/run/run.go — Spec, Once; cmd/apogee/e2e_fallback_test.go — killHeldPrimary
**Tests:** `TestHeadlessRoutedFallbackRelatchesLaterSpawns` (two sequential `sub_agent` calls; the primary is an httptest handler that stays listening and counts chat requests — discovery answers until a "dead" flag, then 503 on everything — and its chat count stays 1 after the second `sub_agent` call; the second child sends one request to the fallback); `TestOnceBindsTheDelegationLatch` (internal/run).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessRoutedFallback' ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestOnceBindsTheDelegationLatch' ./internal/run/`
**Closes:** firing-routed-relatch-gap
**Commit:** `fix(headless): Firings re-latch the Sub-agents target after a fallover`

## 9. The first sibling to apply a Sub-agents switch announces it

**What:**
Depends on item 8.
**Goal:** a Sub-agents-seat fallover emits exactly one `upstream_switched` event, from the first agent that applies the switch, even when the agent that started the resolver run was cancelled; the session seat's replace-lands rule is unchanged.
**Approach (assumed at the header base):** `fallbackRun` (cmd/apogee/fallback.go) gains `announced atomic.Bool`; `Resolve` attaches `answer.Announce = func() bool { return run.announced.CompareAndSwap(false, true) }`. `FallbackAnswer` (internal/agent/agent.go) replaces `First` with `Announce func() bool` (doc updated; recast every `First` reader and assertion — grep `\.First\b` in internal/agent and cmd/apogee). `Agent.announcesRehome` (internal/agent/rebind.go) becomes `seat == session || (answer.Announce != nil && answer.Announce())`, called only after the replace moved.
**Regression guard.** The recast covers every `\bFirst\b` in internal/agent and cmd/apogee — code, tests, comments, not `FirstLine` — checked with `grep -rnw First internal/agent cmd/apogee | grep -v FirstLine` (unrelated prose at cmd/apogee/root.go and schedule_test.go stays); the `First: true` literals in internal/agent/fallback_test.go (`resolverAnswering`, `spareAnswer`, :277) become an `Announce` that claims true. Two assertions invert rather than rename: cmd/apogee/fallback_test.go:265 flips to "the waiter's `Announce()` is true"; :218 claims `moved.Announce()` first, then asserts `late.Announce()` is false.
**Files:** cmd/apogee/fallback.go; cmd/apogee/fallback_test.go; cmd/apogee/delegation_test.go; internal/agent/agent.go; internal/agent/rebind.go; internal/agent/fallback_test.go
**Read first:** cmd/apogee/fallback.go — fallbackResolver.Resolve, fallbackRun; internal/agent/agent.go — FallbackAnswer; internal/agent/rebind.go — Agent.rehome, Agent.announcesRehome; internal/agent/fallback_test.go — firstOnceResolver, spareAnswer; cmd/apogee/fallback_test.go — TestFallbackResolverWalksForwardNeverBack
**Tests:** `TestFallbackRoutedSiblingsAnnounceWhenFirstIsCancelled` (internal/agent: starter cancelled mid-grace → one event from a sibling); `TestFallbackResolverAnnounceIsClaimedOnce` (cmd/apogee: concurrent claims yield exactly one true); `TestFallbackRoutedSiblingsAnnounceOnce` still passes; `TestFallbackResolverWalksForwardNeverBack` (:218) and `TestFallbackResolverRunOutlivesTheFirstCallersCancel` (:265) recast as the guard states.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallback' ./internal/agent/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFallbackResolver|TestDelegationFallback' ./cmd/apogee/`; `go build ./...`
**Closes:** cancelled-first-silent-switch
**Commit:** `fix(fallback): the first sibling to apply a Sub-agents switch announces it`
