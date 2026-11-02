# The seat owns its fallover, and every door's entry ranks are pinned

**Goal:** Every engine door's server-entry ranks are pinned by characterisation tests, so door drift fails a test. The engine owns a seat's fallover follow-through (announce-once, the Sub-agents re-latch) for every Driver. The background host's copy is guarded, and `/server` and a fallover share one arrival tail.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 00d5bf9f
**Sources:**
- `docs/reviews/architecture-review-2026-11-02.html` — cards #2, #1, #8, #6
- ADR 0105 (+ `adr-0105-amend-1102`, `-1102b`, `-1102c`) · ADR 0024 D6 (amended 2026-10-08) · ADR 0045 D4 (+ `adr-0045-amend-1102`) · ADR 0083 part 4 (+ its 2026-09-30 amendment) · ADR 0089 · ADR 0028 D3 · ADR 0039 D2 · ADR 0031
- `docs/plans/archived/2026-11-02 - 01 - fallback-followups-plan.md` (its Driver-side re-latch call is reversed)
- Glossary: Upstream, Delegation target, Server fallback, Parallel agents

**Ratified design calls** (owner, 2026-11-02, unless marked writer):
- **ADR 0083 part 4:** yield — no entry-binding type; the rank-table pins (items 1–2) are kept so door drift fails a test (owner, 2026-11-02).
- **Re-latch:** engine-owned from a Driver-resolved `FallbackAnswer.Target` (Bypass, Vision, ParallelAgents); `delegationLatchSlot` and `run.Spec.BindDelegationLatch` are deleted; the TUI landing hook keeps the beat re-point, drops the push.
- **Announce-once:** engine-owned, keyed by (resolver, target server); `Announce` leaves the public `FallbackAnswer`.
- **Seat and resolvers:** one unexported seat value in `internal/agent`, no public `Seat` type; the four Driver resolver constructors collapse into one.
- **Arrival:** event-driven, `FollowFallback` stays (a landing-hook commit fires before the switch can be refused); the TUI roster settles at the Turn boundary (`ToolsForRoster` stays, `SwapTools` refuses mid-Exchange).
- **Background host:** a field-coverage guard plus one shared-handles value; never built through `buildAgent`.
- **In-session Firing:** its description and forced-dialect drops stay byte-identical (issue firing-description-scrub-parity).
- **Order:** rank pins, then the seat, then the fallover fixes.
- **Firing fallover width** (writer): the engine applies `FallbackAnswer.ParallelAgents` (0: leave alone) to the root on a landed session-seat rehome; the TUI source leaves it 0, as `caps` owns the session width (ADR 0039 D2).
- **Root-bound writes** (writer): the tree-shared latch carries the root's width setters, so a write from any depth lands on the root.

**Standing requirements:**
- skills: coding-standards
- Test-run rules (pass to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never run `-race` or `-cover` over a whole package or a pattern. Never run two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); the tui `Model` is copied by value (ADR 0011: no `strings.Builder` by value). Test durations go through `internal/testtime`.
- Refactor items keep model-facing bytes identical: orientation lines (entry description included), the far width (`stateFarWidth`), `upstream_switched` payloads, notice text (`fallbackNote`, headless stderr, `daemonSwitchLine`, `SeatFallbackNote`), request bodies after a rehome.

**Out of scope:**
- Review #11 (issue engine-composition-unify); reviews #3, #4, #5, #7 (plans 05–07); in-session Firing orientation parity (issue firing-description-scrub-parity); `/server` timing; an engine-commit arrival callback; one entry-resolution function (ADR 0083 part 4).

Renumbered after the 2026-11-02 check: old→new 1→1, 2→2, 4→3, 5→4, 6→5, 7→6, 8→7, 9→8, 12→9, 14→10, 15→11, 16→12, 17→13; old 3, 10, 11 and 13 dropped.

**Regression check (2026-11-02, 00d5bf9f):** (new numbers; old in parentheses)
- 1 (1): guard folded — the pins are kept as permanent characterisation tests (owner).
- 2 (2): guard folded — its WRONG-at-base rows are flipped by items 9 and 8 (owner).
- old 3, 10, 11, 13: dropped — yields to ADR 0083 part 4 (2026-09-15 ruling, 2026-09-30 amendment): no entry→engine-facts function; the arrival-site ladders and the hand-ranked `Config.EffortDialect` stand.
- 3 (4): guard folded — `cmd/apogee/delegation_test.go` joins Files.
- 4 (5): guard folded — a resolver-keeping switch constructor; every moved-field reader moves.
- 5 (6): guard folded — `cmd/apogee/delegation_test.go` joins Files.
- 6 (7): recast — a nil-latched target is admitted; `SetDelegationSeat` clears the armed resolver.
- 7 (8): recast — four guards folded; supersedes `adr-0045-amend-1102`'s at-once push sentence.
- 8 (9): guard folded — owns the `firingConfig` reorder, lands before item 9.
- 9 (12): recast — re-anchored at the Firing door, no entry-binding dependency.
- 11 (15): guard folded — the session-seated child's empty latch survives the copy; value held by value.
- 12 (16): guard folded — Depends re-anchored to items 7 and 9.
- 13 (17): recast — no glossary Note; the `adr-0045-amend-1102` note names the superseded sentence.
- Re-check round (2026-11-02, 00d5bf9f; new numbers):
  - 5: guard folded — the announce claim keys on the resolver only when its dynamic type is comparable, else on the seat holder.
  - 6: guard folded — re-latch only on the first landing per (resolver, `answer.Server`) (straggler row); the same comparability guard.
  - 7: recast — the engine re-latches when the resolver's landing commits, so an Esc mid-grace keeps today's at-once routing on both Drivers; the cancelled-callers check is a rig test, not headless.
  - 8: guard folded — the "constant for the run" / "collapses to a constant" comments are corrected.
  - 9: guard folded — Goal qualified for a stated far width; filled on the Firing lander only (TUI answer pinned at 0); a session-seated child's latch carries the root's setters.
  - 13: guard folded — `--title` on `capcom know add`, the bare-`Announce` grep, `adr-0045.md`'s Status line, ADR 0105 decisions 6 and 8 named.

## 1. Pin how the session, Firing and probe doors resolve an entry — ✅ DONE (2026-11-02)

NOTES (2026-11-02): door C' rebuilds scheduleWiring.fire's inline beat closure in the test (model bound, live.observed(), caps.current(), live.observedDialect()) — the closure is not callable without running raise; the session state it reads is driven through liveSettings.observe and parallelAgentsCap.follow/observe.
NOTES (2026-11-02): the dialect column for A and B' is the Monitor binding's forced dialect (the session states the forced key there); for B it is what rebindSpecFor states (none — rootWiring.rebind lays the beat's own over the spec), each with its reason in the want.

**What:** test-only; no production change.
- Matrix: entry pinned/unpinned × root keys set/unset × beat (window, slots, dialect, model) present/absent × forced `effort-dialect:` × keyed/unkeyed.
- Columns: every rank (window, working, reserve, max output, stream idle, width, dialect, served model) and name/description/ephemeral.
- Doors: A startup bind (`serverBinder.build` recorder); B rebind (`rebindInputs` + `rebindSpecFor`); B' `/server` (`entryUpstream`, live pins); C headless/daemon Firing (`firingConfig`, stub beat); C' in-session Firing (`firingBinding` + `schedule.go`'s beat); G probe (`probeContextConfig`).
- Deliberate differences carry a one-line reason: B has no working window (next-start); C' drops description and forced dialect, takes the session's width; G observes nothing.

**Regression guard.** Kept (owner, 2026-11-02): the pins are permanent characterisation tests of today's per-door ranks (incl. routed doors E and F differing on purpose, the Reachable gate, root stream-idle on routed doors) and live in cmd/apogee test files, no entry-binding reference.

**Files:** cmd/apogee/entryrank_test.go
**Read first:** cmd/apogee/wire_server.go — serverBinder.build; cmd/apogee/wire_settings.go — liveSettings.rebindInputs, rebindSpecFor, liveSettings.firingBinding; cmd/apogee/fallback.go — entryUpstream;
cmd/apogee/wire_firing.go — firingConfig; cmd/apogee/schedule.go — scheduleWiring.fire; cmd/apogee/probecontext.go — probeContextConfig
**Tests:** new `TestEntryRankSessionDoors`, `TestEntryRankFiringDoors`, `TestEntryRankProbeDoor`.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestEntryRank' ./cmd/apogee/`

**Commit:** `test(cmd): pin how the session, Firing and probe doors resolve a server entry`

## 2. Pin how the fallback and routed doors resolve an entry — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the unattended D and F landers are built by hand as newFiringFallback / newFiringSeatFallback build them (in.opts, in.roots; routed over the parent firingConfig hands resolveFiringRouting): both constructors bury the lander in a closure with a real Monitor beat, so a stub beat cannot reach them. The TUI landers come from newSessionFallback(...).lander() / .routedLander() with only key and beat overridden.
NOTES (2026-11-02): added an entryRanks `namedOn` column (the endpoint the namer names an unrouted child on), stated only by the unattended D door, to carry the firing-namer-dead-primary WRONG-at-base row; the width WRONG-at-base row reads the Firing's composed cfg.ParallelAgents. The stub beats rank the entry's forced dialect in, as each Driver's entry Monitor does.
NOTES (2026-11-02): the routed "parent's window" rank is unreachable in the matrix (a beat that names no model names no window, so the row does not land); TestFallbackLanderRoutedBindsAsTheDelegationTarget keeps covering it.

**What:** test-only; depends on item 1 (same file, same matrix).
- Doors: D session-seat landing and F routed landing via `fallbackLander.arrive` with a stub beat and key, both Drivers' resolvers; E via `resolveDelegationTarget`.
- Routed scope rows state ADR 0045 D4: no root `context-window:`/`working-window:`, reserve inherits the parent's.
- Two rows pin today's wrong value under a `// WRONG at base:` comment naming issue and fixing item: a Firing's width after a session-seat fallover (firing-fallover-width-stale, item 9) and its namer's unrouted binding (firing-namer-dead-primary, item 8) stay the primary's.

**Regression guard.** Kept (owner, 2026-11-02); its two WRONG-at-base rows are flipped by the width and namer fix items (items 9 and 8), not by a binding.

**Files:** cmd/apogee/entryrank_test.go
**Read first:** cmd/apogee/fallback.go — fallbackLander.arrive, fallbackLander.pins, newFiringFallback, sessionFallback.routedLander; cmd/apogee/delegation.go — resolveDelegationTarget;
cmd/apogee/wire_firing.go — newFiringSeatFallback; cmd/apogee/naming.go — newFiringNamer; cmd/apogee/fallback_test.go — TestFallbackLanderBindsTheFallbackEntry
**Tests:** new `TestEntryRankFallbackDoors`, `TestEntryRankRoutedDoors`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestEntryRank' ./cmd/apogee/`

**Commit:** `test(cmd): pin how the fallback and routed doors resolve a server entry`

## 3. One seat-resolver constructor for every seat and Driver — ✅ DONE (2026-11-02)

NOTES (2026-11-02): `newSeatResolver(kind, src seatSource)` returns a `*seatResolver` that embeds `*fallbackResolver` and records the latest landing (`latestLanding`); the routed flag comes from `kind` (subAgentsSeat). The parent a routed child keeps comes from the source's lander func (`firingLander(in, keys, parent)` on the Firing, `sessionFallback.routedLander` in the TUI), not from a separate source field. Firing sources are `firingSessionSource` (fallback.go) and `firingSubAgentsSource` (wire_firing.go). The TUI Sub-agents seat is built inside `delegationWiring.rearmLocked`, and `withFallback` now calls it.
NOTES (2026-11-02): `sessionFallback` keeps its per-name `landed` map, which the landing hook feeds, so `sessionMover.fallover(name)` reads exactly what it did at the base. Its `build` closure is replaced by a `binding` reader (holder.Binding). cmd/apogee/wire_live.go needed no edit: `newSessionFallback`'s signature and `withFallback(w.fallback.routedLander)` are unchanged.
NOTES (2026-11-02): consequential edit — cmd/apogee/entryrank_test.go: made necessary by the removal of newFiringFallback/newFiringSeatFallback (three doc comments re-pointed at firingSessionSource/firingSubAgentsSource)
NOTES (2026-11-02): consequential edit — cmd/apogee/naming.go: made necessary by the removal of newFiringSeatFallback (doc comment re-pointed at firingSubAgentsSource)

**What:**
**Goal:** one constructor builds every seat resolver in `cmd/apogee` (both seats, both Drivers) from a TUI-live or Firing-fixed source; each exposes its latest landing; every landing binds as at the base.
**Approach (assumed at the header base):**
- `newSeatResolver(kind, src)` replaces `newFiringFallback`, `newSessionFallback`'s `build` closure, `delegationWiring.seatFallbackFor` and `newFiringSeatFallback`.
- The source supplies options (`in.opts` or `live.options()`), key (`keys.ResolveWithin` or `keys.Resolve`), primary beat, routed flag, parent and landing hook. `sessionFallback`'s per-primary laziness stays as a wrapper.
- Driver-only: no change under `internal/`.

**Regression guard.** `cmd/apogee/delegation_test.go` joins Files, as the item edits it: its `newSeatFallbackRig` calls `withFallback(func() fallbackLander)` and reads `wiring.fallback.primary`/`.recovered` as `*fallbackResolver` (owner, 2026-11-02).

**Files:** cmd/apogee/fallback.go, cmd/apogee/delegation.go, cmd/apogee/wire_firing.go, cmd/apogee/wire_live.go, cmd/apogee/fallback_test.go, cmd/apogee/delegation_test.go
**Read first:** cmd/apogee/fallback.go — newFiringFallback, newSessionFallback; cmd/apogee/delegation.go — withFallback, seatFallbackFor; cmd/apogee/wire_firing.go — newFiringSeatFallback;
cmd/apogee/wire_live.go — newSessionFallback call; cmd/apogee/upstream.go — sessionMover.fallover; cmd/apogee/delegation_test.go — newSeatFallbackRig
**Tests:** new `TestSeatResolverBindsEverySeatAndDriverAlike` (four seat × Driver cases); existing, by the Acceptance pattern.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestSeatResolver|TestFallbackLander|TestFallbackResolver|TestHeadlessRoutedFallback|TestTUIFallback|TestDelegationFallback|TestEntryRank' ./cmd/apogee/`

**Commit:** `refactor(cmd): one constructor builds every seat's fallback resolver`

## 4. The engine holds an Agent's seat as one value — ✅ DONE (2026-11-02)

NOTES (2026-11-02): re-derived from "delegation.seed assigns the seat": buildAgent now takes the seat whole (`seatBinding`) and assigns it once in the Agent literal — newAgent passes a holder of its own, newDelegateAgent(cfg, d) passes d.seating (its separate `up` parameter is dropped) — so seed no longer copies upstream/fallback/appliedGen field by field.
NOTES (2026-11-02): the constructors are `ownedSeat(up, resolver)` (fresh owned holder at generation 0 — serves New, Resume and a routed spawn with target.Fallback), `seatBinding.inherit()` (unrouted child, background host) and `seatBinding.switched(up)` (SwitchUpstream: ownedSeat keeping the resolver); the Agent field is `seating`, the type lives in sharedupstream.go so the doc.go package map needs no new file.
NOTES (2026-11-02): consequential edit — internal/agent/background_test.go, internal/agent/dialer_test.go, internal/agent/routedspawn_test.go, internal/agent/seat_test.go, internal/agent/statemachine_test.go, internal/agent/toolresultfloor_test.go: made necessary by moving .upstream/.appliedGen/.ownsUpstream/.fallback into Agent.seating (readers moved with the value, per the item's guard).

**What:**
**Goal:** an Agent's seat (holder, resolver, applied generation, owns-upstream) is one unexported `internal/agent` value, assigned whole wherever an Agent is built or switched.
**Approach (assumed at the header base):**
- `seatBinding` with `inherit()` (unrouted: shares holder and resolver) and `routed(target)` (fresh holder at generation 0, the target's resolver) constructors.
- `newChildAgentOn`, `delegation.seed`, `backgroundHost` and `SwitchUpstream` assign it once; drop the field-by-field `fallback`/`appliedGen` copies. `consultFallback` keeps reading retired off the shared holder.
- A routed child keeps its own client and `wireTap`: the seat holds state, not a shared Responder.

**Regression guard.** A third constructor (switched: fresh owned holder at generation 0, resolver kept) serves `New`, `Resume` and `SwitchUpstream`, so a `/server` never drops the session's fallover. Every reader of `.upstream`/`.fallback`/`.appliedGen`/`.ownsUpstream` moves with the value, tests and `collect.go` included (`grep -n '\.\(upstream\|appliedGen\|ownsUpstream\)\b\|a\.fallback\b' internal/agent/*.go`); the field is not named `seat`, which `delegationseat.go` already holds.

**Files:** internal/agent/agent.go, internal/agent/subagent.go, internal/agent/construct.go, internal/agent/background.go, internal/agent/rebind.go, internal/agent/loop.go, internal/agent/collect.go, internal/agent/sharedupstream.go, internal/agent/sharedupstream_test.go, internal/agent/fallback_test.go, internal/agent/switchupstream_test.go, internal/agent/subagent_test.go
**Read first:** internal/agent/construct.go — delegation.seed; internal/agent/subagent.go — newChildAgentOn; internal/agent/background.go — backgroundHost; internal/agent/rebind.go — SwitchUpstream;
internal/agent/agent.go — New, Resume; internal/agent/loop.go — consultFallback; internal/agent/fallback_test.go — switchAway
**Tests:** new `TestSeatBindingInheritance`, `TestSeatBindingSwitchKeepsTheResolver` (after `switchAway`, a fault on the new holder still consults the resolver); existing `TestFallbackBackgroundChild`, `TestFallbackSwitchByAnUnroutedChildMovesTheSessionSeat`.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestSeatBinding|TestFallback|TestSharedUpstream' ./internal/agent/`

**Commit:** `refactor(agent): an Agent's seat is one value`

## 5. The engine announces a routed fallover once, on every Driver — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the claim key's comparability test is `reflect.ValueOf(r).Comparable()`, not the plan's `reflect.TypeOf(r).Comparable()`: the Type form reports true for a struct holding an interface field whose dynamic value is a func, which would still panic the hash; the Value form checks the dynamic contents.
NOTES (2026-11-02): the claim set lives on `delegationLatch` (`announced map[fallbackClaim]struct{}`, `claimFallbackKey`, `claimAnnouncement`), under the latch's own mutex; the session seat's announce-on-landed-replace is unchanged.
NOTES (2026-11-02): the test helper `firstOnceResolver` (and `claimOnce`, `alwaysAnnounces`) is renamed/removed: with no claim left to hand out it is now `spareSeatResolver`, a plain spare-answering stub; `claimsAnnounce` is removed from cmd/apogee/fallback_test.go and its assertions dropped from the resolver and delegation tests.

**What:**
**Goal:** a routed fallover emits exactly one `upstream_switched` Event per seat move with any resolver; `FallbackAnswer` has no `Announce` field.
Fix — routed-fallover-unannounced: with `FallbackAnswer.Announce` nil, `Agent.announcesRehome` never announces a routed fallover (bench, embedders). Depends on item 4.
**Approach (assumed at the header base):**
- A tree-shared claim set on `delegationLatch`, keyed by (resolver identity, `answer.Server`), claimed in `announcesRehome` once a routed child's replace moved; the session seat is unchanged.
- Delete `fallbackRun.announced` and the CAS in `fallbackResolver.Resolve`; update the `FallbackAnswer` docs (`agent.go`, `apogee.go`). Payload byte-identical.

**Regression guard.** `cmd/apogee/delegation_test.go` joins Files, as the item edits it: removing `Announce` breaks its claim assertions (`claimsAnnounce` users, `TestDelegationFallbackSiblingsShareOneGrace`) (owner, 2026-11-02).
Comparability (owner, 2026-11-02): the claim keys on the resolver only when `reflect.TypeOf(r).Comparable()`, else on the seat holder, so an embedder resolver of an uncomparable dynamic type (a func adapter, a struct holding a slice; `FallbackResolver` is public in `apogee.go`) never panics "hash of unhashable type"; the `FallbackResolver` doc (`agent.go`, `apogee.go`) states the contract.

**Files:** internal/agent/agent.go, internal/agent/rebind.go, internal/agent/delegationtarget.go, internal/agent/fallback_test.go, cmd/apogee/fallback.go, cmd/apogee/fallback_test.go, cmd/apogee/delegation_test.go, apogee.go
**Read first:** internal/agent/rebind.go — announcesRehome; internal/agent/agent.go — FallbackAnswer; internal/agent/delegationtarget.go — delegationLatch; cmd/apogee/fallback.go — fallbackResolver.Resolve;
internal/agent/fallback_test.go — claimOnce, TestFallbackRoutedSiblingsAnnounceOnce; cmd/apogee/fallback_test.go — claimsAnnounce; cmd/apogee/delegation_test.go — TestDelegationFallbackSiblingsShareOneGrace
**Closes:** routed-fallover-unannounced
**Tests:** new `TestFallbackRoutedSwitchAnnouncedWithAResolverThatSetsNoClaim` (bites at base); new `TestFallbackRoutedSwitchAnnouncedWithAnUncomparableResolver` (a func-adapter resolver: one Event, no panic); keep `TestFallbackRoutedSiblings*`; delete `TestFallbackResolverAnnounceIsClaimedOnce`.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFallbackRouted' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFallbackResolver|TestHeadlessRoutedFallback|TestDelegationFallback' ./cmd/apogee/`

**Commit:** `fix(agent): the engine announces a routed fallover once, on every Driver`

## 6. The engine re-latches a fallen-over Sub-agents seat — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the root's far-width setter is bound at the end of seedTopLevel (`a.delegation.stateWidth = a.stateFarWidth`), not in the `&delegationLatch{}` literal, so gofmt does not realign the block of trailing comments above it.
NOTES (2026-11-02): the comparability guard is one helper (`comparableResolver`, delegationtarget.go), now shared by item 5's `claimFallbackKey` and the new `delegationLatch.relatch`; a seat whose resolver is uncomparable is never re-latched (identity with the armed resolver cannot be established), and never panics.
NOTES (2026-11-02): no Driver fills `FallbackAnswer.Target` yet (cmd/apogee is outside this item's Files; item 7 hands it over), so this item changes no shipped behaviour; the Driver pushes stand. A Driver that calls `SetDelegationSeat` after its first `SetDelegationTarget` leaves the latch disarmed until its next usable beat re-arms it.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** a routed child's landed rehome with `FallbackAnswer.Target` set re-latches the tree's Delegation target and the root's far width to `Target.ParallelAgents`, unless a `/sub-agents-server` or relist replaced the landing resolver.
Depends on item 5.
**Approach (assumed at the header base):**
- Add `FallbackAnswer.Target *DelegationTarget`; nil re-latches nothing.
- The latch carries the root's width setter, bound when the root Agent is built (`seedTopLevel`), so `stateFarWidth` runs on the root from any depth.
- Guard: the latch arms on the resolver of the last non-nil target latched; a nil-latched target is admitted; `SetDelegationSeat` (the human door) clears the armed resolver; re-latch only while the armed resolver is the landing Agent's seat resolver.
- The Driver pushes stay in this item and write the same target; item 7 removes them.

**Regression guard.** A nil-latched target is admitted and the armed resolver is cleared by `SetDelegationSeat` (owner, 2026-11-02): the dead primary's unusable beats latch nil before the grace ends, so a guard on the latched target's `Fallback` alone refuses the ordinary TUI fallover.
Straggler (2026-11-02): re-latch only on the first landing per (resolver, `answer.Server`) — item 5's claim — so a later consult of a cached switch run (`fallbackResolver.runs` keeps a switch) moves no latch onto a dead fallback. Comparability (owner): compare the armed resolver with the child's only when both dynamic types are comparable (item 5's guard), never panicking on an embedder's resolver.

**Files:** internal/agent/agent.go, internal/agent/rebind.go, internal/agent/delegationtarget.go, internal/agent/delegationwidth.go, internal/agent/delegationseat.go, internal/agent/construct.go, internal/agent/fallback_test.go
**Read first:** internal/agent/rebind.go — rehome, announcesRehome; internal/agent/delegationtarget.go — delegationLatch, SetDelegationTarget; internal/agent/delegationwidth.go — stateFarWidth, forgetFarWidth, statedDelegationWidth;
internal/agent/delegationseat.go — SetDelegationSeat; internal/agent/construct.go — seedTopLevel, applyConstructionSeed; internal/agent/subagent.go — newChildAgentOn (routed fallback = target.Fallback);
cmd/apogee/fallback.go — fallbackResolver.Resolve, execute; internal/agent/fallback_test.go — newSeatRig, TestFallbackRoutedChildConsultsItsTargetsResolver
**Tests:** new `TestFallbackRoutedRehomeRelatchesTheTreeAndStatesTheFarWidth`, `TestFallbackRoutedRelatchYieldsToANewerSeat` (with a nil-latched-target row that re-latches, a `SetDelegationSeat` row that does not, a straggler row — a later consult of the cached run moves no latch — and an uncomparable-resolver row that does not panic).
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFallbackRouted|TestDelegationTarget|TestSeatBinding' ./internal/agent/`

**Commit:** `refactor(agent): the engine re-latches a fallen-over Sub-agents seat`

## 7. Drivers hand the target over; the latch slot is deleted — ✅ DONE (2026-11-02)

NOTES (2026-11-02): re-derived from the assumption that the engine's landing observation sits in rebind.go's rehome: the resolver consult it must observe is loop.go's consultFallback, which now calls rebind.go's new consultResolver (a one-line change in loop.go).
NOTES (2026-11-02): mechanism for guard (b): consultResolver re-latches from any Sub-agents-seat switch answer (cancelled or not, before the rehome, so a refused rehome still re-latches, as the Driver push did); a wait cancelled before the answer starts awaitLanding, which re-consults the same resolver on context.WithoutCancel (joining the detached run) and re-latches from its switch. It runs only while the latch is armed on that resolver (delegationLatch.isArmedOn).
NOTES (2026-11-02): the re-latch's once-per-(resolver, server) claim is now its own map (delegationLatch.relatched), separate from item 5's announcement claim, because a cancelled caller re-latches without moving a holder or announcing. relatch(resolver, server, target) claims, checks and installs in one critical section, and claims only for a comparable resolver.
NOTES (2026-11-02): narrow race: if the run ends without a switch between a caller's cancel and awaitLanding's consult, fallbackResolver has already dropped that run, so the consult starts a new grace window and walk. Its answer is still used only to re-latch.
NOTES (2026-11-02): seatSource.landed now returns the landing's Delegation target (the session seat's hook returns nil). seatResolver.land sets Target.Fallback to itself, puts the target on the answer, and records the latest landing after the hook so latestLanding carries Target. fallover returns nil when `d.fallback != from` (guard a) and keeps the re-point and the targetBinding/targetDialect record (guard c).
NOTES (2026-11-02): firingRouting gained `subAgents *seatResolver` (the resolver target.Fallback carries), which newFiringNamer reads in place of the slot.
NOTES (2026-11-02): a nil target from a landing no longer latches nil, where the Driver push did. This cannot happen: resolveDelegationTarget returns nil only for an unreachable beat or no model, and arrive refuses the landing under the same conditions.
NOTES (2026-11-02): TestDelegationFallbackRetargetRearmsTheResolver (kept) now also checks that the retired resolver's landing answers nil. TestSeatResolverBindsEverySeatAndDriverAlike now checks that a session-seat landing answers no Target, and its Sub-agents rows read the answered target through the shared helper landedTargetCheck.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** no `delegationLatchSlot`, `run.Spec.BindDelegationLatch` or Driver push of a fallen-over target exists; both Drivers' routed landings set `FallbackAnswer.Target` (the TUI's only past `fallover`'s `d.fallback == from` check); `delegationWiring.fallover` keeps only its human-door guard, the beat re-point and its `targetBinding`/`targetDialect` record.
Depends on items 3 and 6.
**Approach (assumed at the header base):**
- The routed landing builds the target with `resolveDelegationTarget`, exactly as both Drivers do today; the TUI landing sets `answer.Target` only when `fallover`'s `d.fallback == from` check passes (an in-place relist rearms without a push).
- The engine's seat binding re-latches when the resolver's landing commits — it observes the landing, not only an applied rehome — from `answer.Target`, so on either Driver a routed spawn after every caller was cancelled mid-grace (an Esc in the TUI) dials the fallback before its first beat; engine-owned, no Driver push kept.
- `fallover` keeps recording `targetBinding`/`targetDialect`, which `routedBinding` feeds the TUI namer.
- Delete the slot in `wire_firing.go` (type and `raise` wiring), the `run.Spec` field and its call in `run.Once`.
- `newFiringNamer` reads the Sub-agents resolver's latest landing (item 3) instead of the slot.
- Must hold: the root's far width; `/sub-agents-server` beats a late landing.

**Regression guard.** Owner, 2026-11-02 — re-latch engine-owned, latch slot and `BindDelegationLatch` deleted, with four guards: (a) the TUI landing sets `answer.Target` only when `fallover`'s `d.fallback == from` check passes (an in-place relist rearms without a push); (b) on both Drivers the engine's seat binding re-latches when the resolver's landing commits — it observes the landing, not only an applied rehome — using `answer.Target`, so a routed spawn after every caller was cancelled mid-grace (an Esc in the TUI, a cancelled Firing caller) dials the fallback before its first beat, keeping today's at-once routing — engine-owned, no Driver push kept; (c) `fallover` keeps recording `targetBinding`/`targetDialect` that `routedBinding` feeds the TUI namer; (d) `cmd/apogee/delegation_test.go` joins Files and `TestDelegationFallbackRoutedSeatFallsOverOneWay` and `TestDelegationFallbackRehomeBindsTheLatchedTargetsWindow` are recast to read `answer.Target` (`Target.Fallback` == the resolver). This supersedes the Driver push in `adr-0045-amend-1102`'s "pushes the fallback's target into the latch at once … so a routed child spawned before the next beat dials the fallback"; its at-once timing is kept, engine-owned; item 13 records it. The cancelled-callers check is a rig test: no headless door cancels a routed caller mid-grace (delegate bounds and `--max-wall-time` are checked only between Turns).

**Files:** internal/run/run.go, internal/run/run_test.go, cmd/apogee/wire_firing.go, cmd/apogee/delegation.go, cmd/apogee/delegation_test.go, cmd/apogee/naming.go, cmd/apogee/naming_test.go, cmd/apogee/fallback.go, cmd/apogee/fallback_test.go, internal/agent/agent.go, internal/agent/rebind.go, internal/agent/delegationtarget.go, internal/agent/fallback_test.go
**Read first:** cmd/apogee/delegation.go — fallover, seatFallbackFor, land, Retarget, relist; cmd/apogee/wire_firing.go — newFiringSeatFallback, delegationLatchSlot, firingConfig namer call; internal/run/run.go — Spec.BindDelegationLatch, Once;
cmd/apogee/naming.go — newFiringNamer; cmd/apogee/delegation_test.go — newSeatFallbackRig, lastPush, TestDelegationFallbackRetargetRearmsTheResolver; cmd/apogee/naming_test.go — TestFiringNamerFollowsTheLatchedSubAgentsTarget;
cmd/apogee/e2e_fallback_test.go — TestHeadlessRoutedFallbackRelatchesLaterSpawns, newFallingGrunt; internal/run/run_test.go — TestOnceBindsTheDelegationLatch
**Tests:** delete `TestOnceBindsTheDelegationLatch`; recast `TestFiringNamerFollowsTheLatchedSubAgentsTarget`, `TestDelegationFallbackRoutedSeatFallsOverOneWay`, `TestDelegationFallbackRehomeBindsTheLatchedTargetsWindow` (read `answer.Target`); new `TestFallbackRoutedLandingRelatchesAfterCancelledCallers` (internal/agent: lands a fallover, cancels every caller mid-grace, then spawns a routed child and asserts it dials the fallback); new `TestFallbackResolverLandsTheTargetForCancelledCallers` (cmd/apogee rig on the TUI and Firing Sub-agents seat resolvers: a Resolve cancelled mid-grace returns zero, and the landing still commits `answer.Target` with `Target.Fallback` == the resolver); keep `TestHeadlessRoutedFallbackRelatchesLaterSpawns`, `TestDelegationFallbackRetargetRearmsTheResolver` (human door wins).
**Acceptance:**
- `go build ./...`
- `grep -rn 'delegationLatchSlot\|BindDelegationLatch' --include='*.go' .` prints nothing
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestOnce' ./internal/run/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFallbackRouted' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeadlessRoutedFallback|TestFiringNamer|TestDelegationFallback|TestFallbackResolver' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestDelegationFallbackRoutedSeatFallsOverOneWay|TestDelegationFallbackRehomeBindsTheLatchedTargetsWindow' ./cmd/apogee/`

**Commit:** `refactor(cmd): Drivers put the fallen-over target on the answer`

## 8. A Firing names unrouted children on the fallback — ✅ DONE (2026-11-02)

NOTES (2026-11-02): newFiringNamer gains a `fallback *seatResolver` parameter (the run's own seat resolver, routing.fallback); its session reader answers the resolver's latest landing's dial binding and rebind dialect, else the bound binding and dialect. firingConfig now builds routing.fallback before the namer; the firingRouting.fallback field comment names the namer as a follower.
NOTES (2026-11-02): entryrank_test.go — firingOnPrimary also returns the composed firingRouting, and a new landSeatOn helper lands the run's own seat resolver (key and beat stubbed as landOn stubs them) so the unattended D row reads the namer after a real landing; the row's `WRONG at base` namedOn now pins the fallback entry's endpoint (rankEndpoint).
NOTES (2026-11-02): TestFiringNamerFollowsTheSessionSeatFallover drives firingConfig end to end with a scripted fallback upstream; confirmed it and the D unattended rank rows fail with the session reader's fallback branch disabled.

**What:**
**Goal:** after a session-seat fallover a Firing names unrouted children through the fallback's binding and dialect.
Fix — firing-namer-dead-primary: `newFiringNamer`'s session upstream is constant for the run, so it dials the dead primary after a fallover. Depends on item 7.
**Approach (assumed at the header base):** the namer's session-upstream func reads the Firing session-seat resolver's latest landing (item 3), else the bound binding and dialect. Flip item 2's marked namer row.

**Regression guard.** This item lands before the width fix (item 9) and owns the `firingConfig` reorder: the namer composes after the Firing session-seat resolver built by item 3's constructor (owner, 2026-11-02).
Correct every comment saying the Firing namer's session Upstream is constant or never moves (`naming.go`'s "collapses to a constant", `wire_firing.go`'s "constant for the run").

**Files:** cmd/apogee/naming.go, cmd/apogee/wire_firing.go, cmd/apogee/naming_test.go, cmd/apogee/entryrank_test.go
**Read first:** cmd/apogee/naming.go — newFiringNamer, delegationNamer.upstream; cmd/apogee/wire_firing.go — firingConfig; cmd/apogee/fallback.go — newFiringFallback, fallbackLander.arrive, fallbackLanding;
cmd/apogee/naming_test.go — TestFiringNamerFollowsTheLatchedSubAgentsTarget; cmd/apogee/request_extra_test.go — TestFiringNamerSpeaksTheEntrysWire
**Closes:** firing-namer-dead-primary
**Tests:** new `TestFiringNamerFollowsTheSessionSeatFallover` (bites at base); `TestEntryRank`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFiringNamer|TestEntryRank' ./cmd/apogee/`
- `grep -n "constant for the run\|collapses to a constant" cmd/apogee/naming.go cmd/apogee/wire_firing.go` prints nothing

**Commit:** `fix(firing): a Firing names unrouted children on the fallback server`

## 9. A Firing fans out at the fallback's width after a fallover — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the Firing fill is a new `seatSource.width` hook set only by `firingSessionSource` and applied in `seatResolver.land` (never in the shared `fallbackLander.arrive`); the width ladder is one helper, `firingWidth` (wire_firing.go), which `firingConfig` now binds the primary's width with too. The engine applies the answer in `rehome` through a new `Agent.stateSessionWidth` and the latch's `setSessionWidth` (the root's `SetParallelAgents`), and a session-seated child's empty latch is now built by `delegationLatch.rootBound()`, which carries both root setters (stateWidth too, though that latch is never armed).
NOTES (2026-11-02): the e2e test counts a child's request by its last message being exactly the task (`childRequestsFor`), since `requestsCarrying` also matches the parent's follow-up, whose delegation record names the refused tasks; verified it fails with the fill disabled (2 of 3 children refused at ceiling 1).

**What:**
Recast at the regression check (2026-11-02).
**Goal:** after a session-seat fallover a Firing's fan-out ceiling and orientation bounds state the fallback entry's resolved width where no Sub-agent server's far width is stated (`statedDelegationWidth` answers a stated `farWidth` first); the TUI's width is unchanged.
Fix — firing-fallover-width-stale: `FallbackAnswer` carries no width and a Firing has no `caps.follow`, so it keeps the dead primary's width. Depends on items 6 and 8.
**Approach (assumed at the header base):**
- Add `FallbackAnswer.ParallelAgents int` (0: leave alone); a landed session-seat rehome applies it to the root via item 6's root-bound setters (`SetParallelAgents` semantics).
- The Firing source fills it at the Firing door directly: the Firing session-seat resolver built in `firingConfig` ranks the landed entry's width as `firingConfig` ranks the primary's; the TUI source leaves it 0.
- Flip item 2's marked width row.

**Regression guard.** Re-anchored (owner, 2026-11-02): the item lands at the Firing door directly (`firingConfig` / the Firing session-seat resolver / `FallbackAnswer.ParallelAgents` as the item states) with no entry-binding dependency — Depends on items 6 and 8.
Fill site (2026-11-02): the width is filled on the Firing session-seat resolver's lander only, never in the shared `fallbackLander.arrive` the TUI's `newSessionFallback` also lands through; a `TestTUIFallback*` assertion pins the TUI landing's answer at `ParallelAgents` 0. A `run_on: session` child's empty latch also carries the root's width setters (or the setters ride outside the latch), so its landed session-seat rehome reaches the root.

**Files:** internal/agent/agent.go, internal/agent/rebind.go, internal/agent/delegationtarget.go, internal/agent/subagent.go, internal/agent/construct.go, internal/agent/fallback_test.go, cmd/apogee/fallback.go, cmd/apogee/fallback_test.go, cmd/apogee/wire_firing.go, cmd/apogee/entryrank_test.go, cmd/apogee/e2e_fallback_test.go
**Read first:** internal/agent/rebind.go — rehome; internal/agent/agent.go — FallbackAnswer, SetParallelAgents; internal/agent/delegationwidth.go — statedDelegationWidth, fanOutCeiling; internal/agent/subagent.go — newChildAgentOn (session-seat latch);
internal/agent/construct.go — seedTopLevel; cmd/apogee/fallback.go — newFiringFallback, fallbackLander.arrive; cmd/apogee/wire_firing.go — firingConfig;
cmd/apogee/e2e_fallback_test.go — writeFallbackConfig, startHeadless, awaitHeadless
**Closes:** firing-fallover-width-stale
**Tests:** new `TestFallbackSessionSeatRehomeStatesTheAnsweredWidth` (internal/agent; with a session-seated-child row whose landed rehome sets the root's width); new `TestHeadlessFallbackFansOutAtTheFallbackWidth` (bites at base; asserts the enforced ceiling); `TestTUIFallbackResolverAnswersForTheBoundEntry` gains an assertion that the TUI landing's answer carries `ParallelAgents` 0.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFallbackSessionSeat|TestFallbackRouted' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeadlessFallback|TestEntryRank|TestTUIFallback' ./cmd/apogee/`

**Commit:** `fix(firing): a Firing fans out at the fallback's width after a fallover`

## 10. Guard the background host's copy field by field — ✅ DONE (2026-11-02)

NOTES (2026-11-02): the seat test reuses seat_test.go's existing `spawnOn` helper rather than adding one; the Delegation-latch comparison is "both on the tree's latch, or both on a session-seated child's own empty latch" (rootBound gives every session-seated spawn a fresh one), not pointer equality.
NOTES (2026-11-02): `guards` is classed a shared handle (its Dangerous/Breaker pointers are the session's), although backgroundHost's doc lists only the idle-only copies by name.

**What:** test-only; depends on item 4.
- A reflect test over `Agent` fails when a field is absent from an explicit table classing how `backgroundHost` treats it: copied, shared handle, frozen at launch, fresh, or zero by design, each with a one-line reason.
- Reasons state ADR 0089's launch-time snapshot and `adr-0105-amend-1102b`'s holder-by-reference; the `backgroundHost` doc names the guard.
- A second test: a host's children inherit the session seat as a foreground child's do.

**Files:** internal/agent/hostcopy_test.go, internal/agent/background.go
**Read first:** internal/agent/background.go — backgroundHost; internal/agent/agent.go — Agent; internal/agent/fallback_test.go — seatRig.backgroundChild, TestFallbackBackgroundChildConsultsTheSessionResolver;
internal/agent/construct.go — buildAgent, seedTopLevel, delegation.seed; internal/agent/subagent.go — newChildAgentOn
**Tests:** new `TestBackgroundHostCoversEveryAgentField`, `TestBackgroundHostChildrenInheritTheSessionSeat`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestBackgroundHost' ./internal/agent/`

**Commit:** `test(agent): guard the background host's copy field by field`

## 11. One shared-handles value for delegates and the background host

**What:**
**Goal:** the tree's shared handles (journal, consoles, runIDs, workflowLive, latch, contextFiles, dial, now, cancelFoldBound, restreamHoldoff) are one unexported value `delegation.seed` and `backgroundHost` copy whole.
Depends on item 10.
**Approach (assumed at the header base):**
- The host keeps: live settings frozen at launch; `liveMode` as the one live tighten-only view; the holder by reference; a fresh estimator; no owned Upstream or tool set.
- Guards, prompts and tasks stay explicit per site (delegate: `ForSubAgent`/fresh; host: shared).
- Item 10's table shrinks to the new value.

**Regression guard.** The session-seated child still swaps in its own empty latch (`d.latch = &delegationLatch{}`, ADR 0069 D3) inside the copied value, after the copy. The value is held by value, never a pointer (the root rewrites `contextFiles` and `dial` after construction); either keep `Agent`'s fields flat behind pack/unpack helpers, or rewrite the `&Agent{…, delegation: &delegationLatch{}}` literals in `fanout_test.go`.

**Files:** internal/agent/agent.go, internal/agent/subagent.go, internal/agent/construct.go, internal/agent/background.go, internal/agent/hostcopy_test.go, internal/agent/fanout_test.go
**Read first:** internal/agent/subagent.go — delegation, newChildAgentOn; internal/agent/construct.go — delegation.seed; internal/agent/background.go — backgroundHost;
internal/agent/subagent_test.go — TestNewChildAgentOn_SessionSeatGetsAnEmptyLatch; internal/agent/delegationtarget_test.go — TestChildSharesTheParentsDelegationLatch; internal/agent/fanout_test.go — Agent literals naming delegation; internal/agent/contextfiles.go — context-file reload
**Tests:** existing, by the Acceptance pattern (`TestNewChildAgentOn_SessionSeatGetsAnEmptyLatch`, `TestChildSharesTheParentsDelegationLatch`, `TestConstructionSeed_*` included).
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestBackground|TestFallbackBackgroundChild|TestSeatBinding|TestSubAgent|TestNewChildAgentOn|TestChildShares|TestConstructionSeed|TestSeat_' ./internal/agent/`

**Commit:** `refactor(agent): delegates and the background host share one handles value`

## 12. One arrival tail for `/server` and a fallover

**What:**
**Goal:** `rootWiring.switchServer` and `rootWiring.followFallback` share one arrival tail: the mover step, then (only if it succeeded) the launcher follow; a profile load still leaves the launcher alone.
Depends on items 7 and 9.
**Approach (assumed at the header base):**
- `rootWiring.arrive(kind)`, kind a human move or a fallover (which still records the landing's observation); still event-driven via `FollowFallback`.
- ADR 0028 D3 (`/server` idle-only) and `adr-0105-amend-1102c` (settle at the Turn boundary) hold.

**Regression guard.** Re-anchored (owner, 2026-11-02): depends on items 7 and 9, not on the dropped live-settings item; no entry-binding reference.

**Files:** cmd/apogee/wire_verbs.go, cmd/apogee/upstream.go, cmd/apogee/wire_server.go, cmd/apogee/fallback_test.go, cmd/apogee/wire_server_test.go
**Read first:** cmd/apogee/wire_verbs.go — rootWiring.switchServer, rootWiring.followFallback; cmd/apogee/upstream.go — sessionMover.move, sessionMover.arrive, sessionMover.fallover;
cmd/apogee/launcher.go — launcherPath.follow; cmd/apogee/fallback_test.go — TestTUIFallbackFalloverFollowsTheLauncherPath; cmd/apogee/launcher_test.go — TestLauncherVerbsFollowThePathSwap
**Tests:** new `TestProfileLoadLeavesTheLauncherAlone`, `TestRefusedArrivalLeavesTheLauncher`; existing, by the Acceptance pattern.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestProfileLoad|TestRefusedArrival|TestTUIFallback|TestRunRootSwitchServer|TestLauncherVerbs' ./cmd/apogee/`

**Commit:** `refactor(cmd): /server and a fallover share one arrival tail`

## 13. Record the engine-owned follow-through

**What:**
Recast at the regression check (2026-11-02).
**Goal:** the decision records describe the shipped shape.
Depends on items 7, 9 and 12.
**Approach (assumed at the header base):**
- `capcom know add --id adr-0105-amend-1102d --parent adr-0105 --title "ADR 0105 amendment of 2026-11-02: …"`: announce-once and the re-latch are engine-owned (claim keyed by resolver and target server; target and width ride `FallbackAnswer`), superseding the followups plan's "Driver-side" re-latch call; name it on `adr-0105`'s Status line (as amending decisions 6 and 8 — the seat's one event and the resolver-owned single-flight) and reading list.
- A dated `> **Amended 2026-11-02 (…)**` blockquote in `adr-0045-amend-1102` pointing at `adr-0105-amend-1102d`: it names as superseded the sentence "pushes the fallback's target into the latch at once … so a routed child spawned before the next beat dials the fallback" and states the engine-owned equivalent (the engine re-latches from `FallbackAnswer.Target` when the resolver's landing commits, so the at-once timing holds); name the blockquote on `adr-0045.md`'s Status line.
- Prose rule: fix every live comment or doc naming the slot, `BindDelegationLatch`, the `Announce` claim or a Driver-side re-latch (`grep -rn 'delegationLatchSlot\|BindDelegationLatch\|\bAnnounce\b\|announced\.CompareAndSwap\|re-\?latch' cmd internal docs/manual docs/design .capcom/knowledge`); historical text stays, and an ADR's decision, rejected and context Notes are never edited.

**Regression guard.** Owner, 2026-11-02: no glossary Note and no binding wording; `adr-0105-amend-1102d` is kept; the dated note in `adr-0045-amend-1102` names as superseded the sentence "pushes the fallback's target into the latch at once … so a routed child spawned before the next beat dials the fallback" and states the engine-owned equivalent. Depends on items 7, 9 and 12.
Re-check (2026-11-02): `capcom know add` takes the required `--title` (shaped like `adr-0045-amend-1102.md`'s title); the blockquote is named on `adr-0045.md`'s Status line (`adr-0045.md` joins Files); `adr-0105`'s Status line names decisions 6 and 8 (`adr-0105-decision.md`) as amended by 1102d; the prose grep matches bare `Announce` and drops `Driver-side`.

**Files:** .capcom/knowledge/decisions/adr-0105.md, .capcom/knowledge/decisions/adr-0105/adr-0105-amend-1102d.md, .capcom/knowledge/decisions/adr-0045.md, .capcom/knowledge/decisions/adr-0045/adr-0045-amend-1102.md
**Read first:** .capcom/knowledge/decisions/adr-0105.md — Status line, reading list; .capcom/knowledge/decisions/adr-0105/adr-0105-decision.md — decisions 6, 8; .capcom/knowledge/decisions/adr-0045/adr-0045-amend-1102.md — the "pushes ... at once" sentence; .capcom/knowledge/decisions/adr-0045.md — Status line;
internal/agent/agent.go — FallbackAnswer.Announce doc; cmd/apogee/wire_firing.go — delegationLatchSlot and newFiringSeatFallback comments; cmd/apogee/fallback.go — fallbackRun.announced, Resolve doc
**Tests:** none (docs and comments).
**Acceptance:**
- `capcom check` and `go build ./...`
- `capcom know show "ADR 0105" | grep -q 1102d`
- `grep -q 1102d .capcom/knowledge/decisions/adr-0045.md`

**Commit:** `docs(adr): the engine owns a seat's fallover follow-through`
