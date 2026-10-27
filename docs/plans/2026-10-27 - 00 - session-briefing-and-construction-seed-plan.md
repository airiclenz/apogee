# Session briefing module and the Agent's construction seed

**Goal:** The Agent finishes its own construction from one seed, so no Driver runs the boot sequence by hand. One unexported module then owns the pre-first-prompt window, both advise lanes and their held runs, and the Agent passes it lifecycle events.
**Date:** 2026-10-27
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** dc370603
**Sources:**
- `docs/reviews/architecture-review-2026-10-27.html` — items 2 (construction seed) and 1 (session briefing)
- ADR 0076 (D6, A8 in `adr-0076-amend-0908`, amendments 1019 / 1021 / 1021b) · ADR 0069 D6 · ADR 0083 part 2 · ADR 0010 · ADR 0018 D6 · ADR 0031
- Glossary: Driver, Generation, Moment, Exchange, Reaction

**Ratified design calls** (owner, 2026-10-27):
- **lateEngine:** seed only. The ten typed pending fields stay. Bind builds them into the construction seed, and ADR 0083 part 2 stands.
- **Background ledger:** the background manager embeds the core generic held ledger and keeps its own extras.
- **Boundary race:** closed inside the new module, by making lane-replace and window-open one critical section (item 10).
- **Placement:** an unexported `sessionBriefing` type in `internal/agent`. No subpackage.
- **Seed route** (writer, forced by ADR 0010, which says `domain` imports nothing): `domain.Config` gains `Sync`. Seat, target and journal ride construction `Option`s beside `WithDialer`.
- **Runner.Replace** (writer, preserves today's behaviour): when a seeded observe list differs from the one the Runner was built from, the Driver calls `Runner.Replace` before `New`. `New` never does.
- **Once journal:** Once keeps New → holdRecord → openRecordJournal → SetJournal (valid before the first Submit); Once seeds lanes, seat and target only; lateEngine still seeds the journal via WithUndoJournal.

**Closes:** driver-boot-sequence-copied; session-lifecycle-scattered

**Standing requirements:**
- skills: coding-standards
- Test-run rules for this machine (pass them to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never run `-race` or `-cover` over a whole package. Never run two test runs at once.
- Model-facing bytes stay identical: fence text, ladder order, the bridge text, refusal wording, and the first request.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).

**Out of scope:**
- Building an Agent with no upstream (ADR 0036 D3). lateEngine stays.
- A pending-Config overlay on lateEngine (refused by ADR 0083 part 2).
- Review item 8 (one advise call). The landing step stays in Agent.
- Seeding mode, effort and context files. These stay post-construction replays.

**Regression check (2026-10-27, dc370603):**
- 2: guard folded (func-value facade sites wrapped; validate-then-scope seed order; supersedes the `agent.Option` doc comment)
- 3: recast (only Observe and Sync from the Spec's Generation; hold → journal → New; lane-refusal prefix kept; far-width test kept)
- 4: guard folded (Bypass OR-ed true only on the bypass reading; the existing turn-1 advise test is the guard)
- 5: guard folded (Bind takes the base Config; Options forwarded through buildAgent/serverBinder.build); yields to cmd/apogee/doc.go:3-6
- 6: recast (whole pending Generation, confine and scratch seeded; deferral and Bind's announcement removed; far-width test kept)
- 7: guard folded (deferral sites owned by items 3, 4, 6; [Unreleased] CHANGELOG entry); supersedes the CHANGELOG [0.27.0] entry
- 8: guard folded (ledger covers item 9's ops; read-set take; pending() accessor; lane wrappers kept)
- 9: guard folded (m.mu guards the ledger; takeNotes/putBack/markDelivered distinct; locking wrappers; widened acceptance)
- 10: guard folded (lock order; prose rule; test-reach repoint rule; race test records what each input saw)
- 11: guard folded (prose rule; agent.go in Files)
- 12: guard folded (lane-only grep; delegate case dropped; "no Agent" binds module tests only)
- 2 (re-check): guard folded (New/Resume return a typed seed-refusal error, exported from agent and aliased in apogee.go, wrapping SetReactions' error; pinned by item 2's tests)
- 3 (re-check): recast (Once journal ratified call: New → holdRecord → openRecordJournal → SetJournal, journal dropped from the seed and the snapshot-store removal dropped; Bypass asserted by behaviour; setter/deferral grep in Acceptance; seeded lanes validated before Runner.Replace); yields to internal/run/run.go:479-484 (the hold is taken after the Agent is built)
- 6 (re-check): guard folded (Goal names Runner.Replace as the observe list's refuser; pending Generation validated before Runner.Replace; wire_live.go replay-prose rule; deferral grep in Acceptance)

## 1. ADR 0076: a construction seed is not a swap — ✅ DONE (2026-10-27)

NOTES (2026-10-27): the amendment also records the plan's ratified Runner.Replace call (a Driver validates and calls Runner.Replace before New when its seeded observe list differs; New never does) as a refinement of A8's "only caller of Runner.Replace", and the Once-journal call (run.Once keeps SetJournal) — both from the plan header's ratified design calls.

**What:**
**Goal:** ADR 0076 records that seeding both reaction lanes at construction (`Config.Observe`, `Config.Sync`) is not a live swap, and that `SetReactions` stays the one live swap (A8). ADR 0069 records that seat-before-target is structural inside `agent.New`/`agent.Resume`.
**Approach (assumed at the header base):**
- Add the dated child Note `adr-0076-amend-1027` under `adr-0076` with `capcom know add --parent adr-0076`.
- Add it to `adr-0076.md`'s status line and reading list.
- Add a dated line to ADR 0069's decision Note (the D6 seat-before-target text) saying construction now applies the seed's seat and then its target, and Drivers no longer order them.

**Files:** .capcom/knowledge/decisions/adr-0076.md, .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1027.md, .capcom/knowledge/decisions/adr-0069/adr-0069-decision.md
**Read first:** .capcom/knowledge/decisions/adr-0076.md — status line, reading list; .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1021b.md — dated-amendment shape;
  .capcom/knowledge/decisions/adr-0069/adr-0069-decision.md — D6 seat-before-target "Note 2026-09-24" paragraphs (lateEngine.Bind, run.Once)
**Tests:** none (docs).
**Acceptance:** `capcom check`; `capcom know show "ADR 0076"` lists the 1027 amendment.
**Commit:** `docs(adr): record the construction seed as distinct from the live swap`

## 2. The engine takes the whole construction seed

**What:**
**Goal:** `agent.New` and `agent.Resume` take a construction seed and apply it in one fixed order before they announce session-started. The seed is the Sync lane (`domain.Config.Sync`, beside `Config.Observe`), the delegation seat, the delegation target and the undo journal. A seed `SetReactions` would refuse fails construction with the same error. One construction-order test pins this.
**Regression guard.** changing the apogee.New/apogee.Resume facades to variadic must not break any site that holds them as a func value (cmd/apogee/probecontext.go `var newProbeAgent = apogee.New` and its test overrides) — item 2 keeps those sites' types by wrapping in a closure, lists cmd/apogee/probecontext.go in Files, and its Acceptance adds `go vet ./cmd/apogee/`. The seed keeps `SetReactions`' order: validate the unscoped seed Generation first, then `scopeSync` its Sync. Supersedes the `Option` doc comment at internal/agent/agent.go:756-758 ("The root facade forwards none of them"), which this item rewrites. New/Resume return a typed error (an unexported-field struct or sentinel the run package can test with errors.As/errors.Is, exported from agent and aliased in apogee.go per cmd/apogee/doc.go) when the construction seed's lanes are refused, wrapping the same underlying error SetReactions would return; item 2's tests pin it.
**Approach (assumed at the header base):**
- Add `Config.Sync []Reaction` to `internal/domain/config.go`, documented like `Observe`.
- Add `agent.WithDelegationSeat(*DelegationSeat)`, `agent.WithDelegationTarget(*DelegationTarget)` and `agent.WithUndoJournal(*undo.Journal, note string)` to `constructOptions` (`agent.go`, beside `WithDialer`).
- In `buildAgent` (`construct.go`), seed `a.gen.Sync`, run the existing `validateGeneration` over the unscoped seed, and only then scope its Sync as `SetReactions` does. This gives the same checks as `SetReactions`: Generation.Validate, ValidateAll and refuseReservedIDs.
- Then apply the journal, then the seat, then the target (which states the far width), and only then the existing announcement.
- `DeferSessionStart` still exists in this item.
- In `subagent.go`, the child config zeroes `Sync`, because a child inherits its reactions by the existing route.
- Make the `apogee.New`/`apogee.Resume` facades (`apogee.go`) variadic over `agent.Option`, so they stay source-compatible for the bench.
**Files:** internal/domain/config.go, internal/agent/agent.go, internal/agent/construct.go, internal/agent/subagent.go, apogee.go, internal/agent/construct_seed_test.go, cmd/apogee/probecontext.go
**Read first:** internal/agent/construct.go — buildAgent; internal/agent/agent.go — constructOptions, SetReactions, scopeSync; internal/agent/delegationtarget.go — SetDelegationTarget;
  internal/agent/subagent.go — childCfg; apogee.go — New; cmd/apogee/probecontext.go — newProbeAgent
**Tests:** `TestConstructionSeed_*` table: Sync armed; far width equals the target's with a seat seeded; `UndoNote` set; a session-started advise entry seen at the boundary; reserved-id Sync refused with `SetReactions`' wording (including one whose `workspace:` names another root); out-of-scope entry dropped; a child spawned with no `Sync`; a refused seed's error matches the typed seed-refusal error (errors.As/errors.Is) and unwraps to `SetReactions`' error for the same lanes.
**Acceptance:** `go build ./... && go vet ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -count=1 -run 'TestConstructionSeed|TestSetReactions|TestSubAgent|TestSessionStart' ./internal/agent/`
**Commit:** `feat(agent): New and Resume apply the whole construction seed`

## 3. run.Once constructs from the seed

**What:** Recast at the regression check (2026-10-27). Depends on item 2.
**Goal:** `run.Once` constructs its Agent from one seed, and the Agent comes out announced. That seed is Observe and Sync from the Spec's Generation, and the Spec's delegation seat and target. `Once` names neither `DeferSessionStart` nor `AnnounceSessionStart` and calls no seat, target or `SetReactions` setter at construction.
**Regression guard.** only Observe and Sync move from Spec.Generation onto Config — Bypass, Floor and ContextFillNotice stay as the caller composed cfg (the run.go Spec.Generation rule "only Observe and Sync are read" stands); assert Bypass by behaviour (Config.Bypass=true with a session-started advise entry in Spec.Generation.Sync leaves the first request with no advice fence) instead of reading a.Generation(); per the Once journal ratified call, Once keeps New → holdRecord → openRecordJournal → SetJournal and seeds lanes, seat and target only, so no snapshot store is removed and TestOnceRefusedAtConstructionLeavesNoLock stays unchanged in intent — this yields to internal/run/run.go:479-484 (the hold is taken after the Agent is built, so a Firing refused at construction leaves no <id>.lock); before Runner.Replace, Once validates the seeded lanes with the exported Generation.Validate + reactions.ValidateAll so an observe list New would refuse is never swapped into the Runner (keeping the validate-before-Replace order internal/reactions/runner.go and internal/domain/config.go document), and that refusal carries the "apogee: arm the firing's reaction lanes:" prefix; a lane refusal (from Runner.Replace or from New's seed validation, distinguished by a typed error item 2 returns for a refused seed) keeps the exact prefix "apogee: arm the firing's reaction lanes:"; keep TestOnceKeepsTheFarWidthOfTheSpecsTarget (it pins the Option forwarding through Once) instead of deleting it; prose guard by rule — every comment in internal/run and cmd/apogee/wire_firing.go found by `grep -n 'swap door\|arming swap\|SetReactions'` that describes the construction route is rewritten. The run.go:110-113 Spec.Generation rule stands; this item yields to it.
**Approach (assumed at the header base):**
- In `Once` (`internal/run/run.go`), call `agent.New`, then hold the record, then open the record journal and `SetJournal` it (Once journal ratified call).
- Pass `WithDelegationSeat` and `WithDelegationTarget`, and put the Generation's Observe and Sync on `Config`.
- Where the Spec's observe list differs from the Runner's, validate the seeded lanes (`Generation.Validate` + `reactions.ValidateAll`), then call `Runner.Replace` before New (ratified call).
- Keep `holdRecord`'s no-lock-left-behind order.
- Keep `TestOnceKeepsTheFarWidthOfTheSpecsTarget`: it pins the Option forwarding through Once.
- Adapt `TestOnceArmsTheLanes…` and `TestOnceHandsTheSpecsObserveLane…` to the seed.
**Files:** internal/run/run.go, internal/run/run_test.go, cmd/apogee/wire_firing.go, cmd/apogee/wire_firing_test.go
**Read first:** internal/run/run.go — Once, holdRecord, openRecordJournal, Spec.Generation, Result; internal/run/run_test.go — TestOnceRefusedAtConstructionLeavesNoLock, TestOnceKeepsTheFarWidthOfTheSpecsTarget;
  internal/session/store.go — Store.hold; cmd/apogee/wire_firing.go — raise, firingConfig; internal/agent/agent.go — SetReactions, SetJournal; internal/agent/reactions.go — bypassSkips
**Tests:** the adapted `TestOnce*` lane tests; a `TestOnce` row with `Config.Bypass=true` and a session-started advise entry in `Spec.Generation.Sync` whose first request carries no advice fence; `TestOnceRefusedAtConstructionLeavesNoLock` unchanged in intent; a refused lane — from the pre-Replace validation and from New's typed seed refusal — keeps the "apogee: arm the firing's reaction lanes:" prefix, and an observe list the validation refuses never reaches `Runner.Replace`; the headless/daemon firing tests unchanged.
**Acceptance:** `! grep -nE 'DeferSessionStart|AnnounceSessionStart|SetReactions\(|SetDelegationSeat\(|SetDelegationTarget\(' internal/run/run.go`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestOnce' ./internal/run/`, then `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFiring|TestHeadless' ./cmd/apogee/`
**Commit:** `refactor(run): Once constructs its Agent from the seed`

## 4. measureTurn1 constructs from the seed

**What:** Depends on item 2.
**Goal:** `measureTurn1` (`cmd/apogee/probecontext.go`) builds its probe Agent with the lanes and Bypass on `Config`. It names neither `DeferSessionStart` nor `AnnounceSessionStart` and calls no `SetReactions`.
**Regression guard.** cfg.Bypass is the composed value, OR-ed true only for the probe's bypass reading — never copied from a Generation's zero Bypass. `cfg.Observe`/`cfg.Sync` are set from the lanes only on the non-bypass reading, as today; `TestProbeContextLiveArmsTheSyncLaneOnTheFirstAgentOnly` stays green.
**Approach (assumed at the header base):** In `newProbeAgent`'s caller, replace the Generation → edit → `SetReactions` → `AnnounceSessionStart` sequence (the 5ee60135 patch) by setting `cfg.Sync` and `cfg.Observe` (non-bypass reading) and `cfg.Bypass = true` (bypass reading only) before construction.
**Files:** cmd/apogee/probecontext.go, cmd/apogee/probecontext_test.go
**Read first:** cmd/apogee/probecontext.go — measureTurn1, measureContextCost, newProbeAgent, probeContextConfig; cmd/apogee/wire_config.go — projectConfig;
  cmd/apogee/probecontext_test.go — TestProbeContextLiveArmsTheSyncLaneOnTheFirstAgentOnly, TestProbeContextLiveMeasuresSessionStartAdviceAtTurn1; internal/agent/sessionadvise.go — sessionAdviseSpecs
**Tests:** existing `TestProbeContext*`. The existing `TestProbeContextLiveMeasuresSessionStartAdviceAtTurn1` already pins that the measured first turn sees the session-started advise lane, and is the guard.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestProbeContext' ./cmd/apogee/`
**Commit:** `refactor(probe): measureTurn1 constructs from the seed`

## 5. lateEngine.Bind takes a seed-aware constructor

**What:** Depends on item 2. Mechanical signature step only.
**Goal:** `lateEngine.Bind`'s constructor argument receives the base `apogee.Config` plus `agent.Option`s, so Bind can add seed values before construction. Behaviour is unchanged.
**Regression guard.** Bind takes the base Config too (e.g. `Bind(cfg apogee.Config, construct func(apogee.Config, ...apogee.Option) (*apogee.Agent, error))`), and each test closure's cfg edits (ObserveRunner, Observe, Endpoint, Tools…) move into that base cfg. `buildAgent` and `serverBinder.build` take `...apogee.Option` and forward them to `apogee.New`/`apogee.Resume`; the `build:` literals in keysource_test.go and wire_server_test.go, which a `.Bind(` grep misses, change with them. Yields to cmd/apogee/doc.go:3-6 (the root constructs through the public apogee package): `apogee.go` aliases `agent.Option` and forwards the seed Options item 6 passes, so no non-test cmd/apogee file imports internal/agent.
**Approach (assumed at the header base):** Change `Bind`'s `construct` parameter to `func(apogee.Config, ...apogee.Option) (*apogee.Agent, error)` (exact shape is the implementer's), and update `wire_server.go` and the test call sites in `cmd/apogee`. Grep for `.Bind(` and `build: func(` to find them.
**Files:** cmd/apogee/wire_engine.go, cmd/apogee/wire_server.go, cmd/apogee/wire_engine_test.go, apogee.go, cmd/apogee/keysource_test.go, cmd/apogee/wire_server_test.go, plus the other `cmd/apogee/*_test.go` files that call `Bind` or set `build:` (grep at run time; about 11)
**Read first:** cmd/apogee/wire_engine.go — lateEngine.Bind, buildAgent; cmd/apogee/wire_server.go — serverBinder.bind, serverBinder.build; cmd/apogee/wire_engine_test.go — bindOverRunner;
  cmd/apogee/keysource_test.go — serverBinder build literals; internal/agent/agent.go — Option, WithDialer
**Tests:** existing.
**Acceptance:** `go build ./... && go vet ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -count=1 -run 'TestLateEngine|TestServer' ./cmd/apogee/`
**Commit:** `refactor(wire): Bind constructs through a seed-aware constructor`

## 6. lateEngine.Bind seeds instead of replaying

**What:** Recast at the regression check (2026-10-27). Depends on items 2 and 5.
**Goal:** Bind builds the construction seed from its typed pending fields: the whole pending Generation (Floor, Bypass, ContextFillNotice, Observe, Sync), Profile, confine, scratch, seat, target and journal. Every pending value that can refuse is refused before anything is constructed — the observe list by Runner.Replace, everything else by New. After `New` returns, no Bind step can refuse, so a refused bind constructs nothing announced. The seat-before-target replay is gone.
**Regression guard.** the whole pending Generation (Floor, Bypass, ContextFillNotice, Observe, Sync) rides Config; every pending value a session-started run or the Runner's repo `run:` entries read at spawn is seeded before New — at least cfg.ConfineToWorkspace = e.confine and cfg.ScratchDir from pendingScratch — and the implementer verifies each remaining replay (mode, effort, compaction, prune, context files, allow rules) is never read by the boundary's runs, seeding any that is; amend the header Out-of-scope line to drop "confine" from the replay list; this item stops wire_boot.go deferring, removes Bind's AnnounceSessionStart call, and removes DeferSessionStart from the cmd/apogee test helpers (bindOverRunner in wire_engine_test.go and the wire_settings_test.go helper), so item 7 no longer lists cmd/apogee/wire_settings_test.go; recast TestLateEngineReplaysThePendingGeneration (Runner held the list before construction announced); KEEP TestLateEngineBindKeepsTheFarWidthOfThePendingTarget (it pins seat/target Option forwarding through Bind). The same validate-before-Replace rule as item 3 — Bind validates the pending Generation (Generation.Validate + reactions.ValidateAll) before Runner.Replace, so a refused generation never reaches the Runner. Prose rule: rewrite every cmd/apogee comment that describes Bind replaying the generation, seat, target, journal, scratch, confine or profile, found by `grep -n 'replay' cmd/apogee/wire_*.go` (wire_live.go:324-330 calls the bind's replay "the ONE route the sync lane takes into a session").
**Approach (assumed at the header base):**
- The ten pending fields stay (ratified call).
- `pendingGeneration` goes on `Config`, validated and then `Runner.Replace` first when its observe list moved.
- `pendingProfile` goes on `Config.Profile`.
- `pendingSeat`, `pendingDelegation` and `pendingJournal` become Options.
- `e.confine` goes on `Config.ConfineToWorkspace` and `pendingScratch` on `Config.ScratchDir`.
- Mode, effort, compaction, prune, context files and allow rules stay post-construction replays, each verified unread by the boundary's runs.
- Both `Close`-on-refusal paths go.
- Keep `TestLateEngineBindKeepsTheFarWidthOfThePendingTarget`.
- Recast `TestLateEngineReplaysThePendingGeneration`: the Runner held the list before construction announced.
- Recast `TestLateEngineBindRefusedAnnouncesNoSessionStart` as "New refused, nothing announced".
**Files:** cmd/apogee/wire_engine.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_live.go, cmd/apogee/wire_engine_test.go, cmd/apogee/wire_settings_test.go, cmd/apogee/wire_boot_test.go
**Read first:** cmd/apogee/wire_engine.go — lateEngine.Bind, seedReactions, buildAgent; cmd/apogee/wire_boot.go — ObserveRunner/Observe/DeferSessionStart seeding; cmd/apogee/wire_live.go — newLateEngine, seedReactions call, cfg.ScratchDir seed;
  cmd/apogee/wire_engine_test.go — bindOverRunner, TestLateEngineReplaysThePendingGeneration, TestLateEngineBindRefusedAnnouncesNoSessionStart; cmd/apogee/wire_settings_test.go — newSessionAdviseReload;
  internal/agent/dispatch.go — syncPermitCtx, confinementBox; internal/agent/construct.go — announceSessionStart
**Tests:** recast refusal test, for both a refused Generation and a refused Profile; a Generation the pre-Replace validation refuses never reaches the Runner (`recordingRunner` sees no Replace); recast `TestLateEngineReplaysThePendingGeneration`. `TestLateEngineBindAnnounces…OnceInstalled`, `TestLateEngineBindKeepsTheFarWidthOfThePendingTarget`, `TestLateEngineReplaysTheFloorGatesAtTheBind` and `TestLateEngineReplaysTheContextFillNoticeAtTheBind` are kept.
**Acceptance:** `! grep -n 'AnnounceSessionStart\|DeferSessionStart' cmd/apogee/wire_engine.go cmd/apogee/wire_boot.go cmd/apogee/wire_engine_test.go cmd/apogee/wire_settings_test.go`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestLateEngine|TestSessionStartedAdopted' ./cmd/apogee/`
**Commit:** `refactor(wire): lateEngine.Bind seeds the Agent instead of replaying`

## 7. Retire DeferSessionStart and AnnounceSessionStart

**What:** Depends on items 3, 4 and 6.
**Goal:** `domain.Config.DeferSessionStart` and `Agent.AnnounceSessionStart` do not exist. Construction always announces session-started at its end. No comment names the deferral.
**Regression guard.** Item 6 removes the cmd/apogee deferral (wire_boot.go, Bind's call in wire_engine.go, `bindOverRunner`, the wire_settings_test.go helper) and items 3 and 4 those in internal/run and probecontext.go; the grep below is the rule, and any hit it still finds in any file is rewritten here. The item's changelog sidecar entry (embedder-facing, landed at closeout) records: `apogee.Config.DeferSessionStart` and `Agent.AnnounceSessionStart` are removed, and construction announces at its end — superseding the `[0.27.0]` entry (CHANGELOG.md:31) that shipped them.
**Approach (assumed at the header base):**
- Remove the field, the method and `announceConstruction`'s deferral branch.
- Rewrite `TestSessionStart_ADeferredConstructionIsTheDriversToAnnounce`, `TestSessionWindow_ADeferredConstructionFiresThroughItsAnnouncement`, and `TestSessionWindow_AnAdviseEntryArmedAtTheBoundaryRunsOnce` so they seed through the construction instead.
- Prose guard: every comment that names `DeferSessionStart`, `AnnounceSessionStart` or "the Driver announces" is rewritten. Find them with `grep -rn 'DeferSessionStart\|AnnounceSessionStart\|defers it to' --include=*.go .`.
**Files:** internal/domain/config.go, internal/agent/construct.go, internal/agent/agent.go, internal/agent/sessionstart_test.go, internal/agent/reactions_test.go, internal/agent/sessionadvise_test.go
**Read first:** internal/agent/construct.go — AnnounceSessionStart, announceConstruction, announceSessionStart; internal/agent/agent.go — constructionStart;
  internal/domain/config.go — DeferSessionStart, ObserveRunner doc; internal/agent/sessionstart_test.go — TestSessionStart_ADeferredConstructionIsTheDriversToAnnounce; internal/agent/sessionadvise_test.go — sessionAdviseAgent
**Tests:** the rewritten tests above.
**Acceptance:** `! grep -rn 'DeferSessionStart\|AnnounceSessionStart' --include=*.go .`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestSessionStart|TestSessionWindow' ./internal/agent/`
**Commit:** `refactor(agent): construction always announces session-started`

## 8. One generic held ledger for the advise lanes

**What:**
**Goal:** An unexported generic `heldLedger[T]` in `internal/agent/heldledger.go` holds pending → delivered → restore-to-front / forget. It has `hold`, `take` (a subset take: only the entries a predicate accepts become delivered), `restore`, `forget` and `reset`, plus `pending` and the operations the guard below names. It is unlocked; its owner locks it. `sessionAdviseLane` keeps its pending and delivered runs in it. Lane behaviour is byte-identical.
**Regression guard.** the ledger API item 8 fixes already covers item 9's needs — a pending snapshot, prepend-to-pending (putBack), direct deliver (markDelivered) and a take that delivers nothing (takeNotes) — so item 9 drops heldledger.go from Files. `take` gets the read set plus the delivered subset (as `take(runs, answered)` does today): runs outside the read set stay pending, and its predicate tests membership only, so `attachSessionAdvice` stays outside `lane.mu`. The ledger has a `pending()` clone accessor; the lane keeps `waiting`/`restoreDelivered`/`forgetDelivered` as wrappers, so construct.go's callers are untouched.
**Approach (assumed at the header base):**
- `sessionAdviseLane` (`sessionadvise.go`) keeps `replace`/`extend`/`adopt`/`stop` and its ctx lifetime.
- Its `pending`/`delivered` slices and the `take`/`restoreDelivered`/`forgetDelivered` bodies move onto the ledger.
- A restore puts delivered entries ahead of pending.
**Files:** internal/agent/heldledger.go, internal/agent/heldledger_test.go, internal/agent/sessionadvise.go
**Read first:** internal/agent/sessionadvise.go — sessionAdviseLane, adopt, take, waiting, joinAdvice; internal/agent/construct.go — exchangeClosed, exchangeAborted;
  internal/agent/compact.go — adopt call site
**Tests:** `TestHeldLedger_*` table over hold/take/restore/forget/reset/pending and the prepend, direct-deliver and deliver-nothing operations, including a subset take that leaves runs outside the read set pending, and restore order.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeldLedger|TestSessionAdvise|TestCompactAdvise' ./internal/agent/`
**Commit:** `refactor(agent): the advise lanes hold runs in one generic ledger`

## 9. The background manager embeds the held ledger

**What:** Depends on item 8.
**Goal:** `backgroundManager`'s held notes and delivered set live in a `heldLedger[string]`. The manager's own extras stay its own: `takeNotes`, `putBack`, `markDelivered`, `heldNotes` serialization, and the clearing in `stopAllBackground`. Its mutex still guards runs, prompts and retiring, and now the ledger. Background behaviour is unchanged, including the serialized note order.
**Regression guard.** `m.mu` guards the ledger: every ledger op runs under it, `holdFinish`'s retiring check and its hold stay one critical section, and the ledger is a named field (not Go embedding), so no unlocked ledger method is promoted onto `backgroundManager`. `takeNotes` (deliver nothing), `putBack` (prepend to pending) and `markDelivered` stay distinct ops. `hold`, `holdFinish`, `takeIntoExchange`, `restoreDelivered` and `forgetDelivered` stay manager methods that take `m.mu` and delegate, so their callers are untouched; `ResumeWorkflows` holds `restoredNotes` through the ledger under the lock.
**Approach (assumed at the header base):** Replace the `notes`/`delivered` slices in `background.go` with the ledger, and express the extras through ledger operations.
**Files:** internal/agent/background.go
**Read first:** internal/agent/background.go — holdFinish, takeNotes, putBack, takeIntoExchange, markDelivered, ResumeWorkflows, Wake; internal/agent/interject.go — TakeWorkflowNotes
**Tests:** the existing `TestBackground*`, `TestWake_*`, `TestFinishNote*`, `TestSnapshot_RoundTripsAHeldMultiLineSubAgentNote`, `TestRestoreState_ChecksTheHeldWorkflowNotes`, `TestSessionWindow_AWakeThatQueuesHeldNotesShutsTheWindow` and `TestInterject*`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestHeldLedger|TestBackground|TestWake|TestFinishNote|TestSnapshot_RoundTripsAHeld|TestRestoreState_ChecksTheHeldWorkflowNotes|TestSessionWindow_AWake|TestInterject' ./internal/agent/`
**Commit:** `refactor(agent): background notes share the held ledger`

## 10. sessionBriefing owns the window and the session-started lane

**What:** Depends on items 7 and 8.
**Goal:** An unexported `sessionBriefing` (`internal/agent/briefing.go`, Agent field `briefing`) owns the pre-first-prompt window and the session-started lane. It also makes the `domain.ObserveRunner` session-started calls.
- Its methods: `boundaryArmed`, `liveArmed` and `inputArrived`, plus `close`.
- The Agent drives the window only through these. Construction, ClearContext and RestoreSession call `boundaryArmed`. `SetReactions`' newly-armed path calls `liveArmed`. Submit, Wake and step call `inputArrived`. Close calls `close`.
- `boundaryArmed` replaces the lane and opens the window in one critical section under `window.mu`, so an input sees the boundary wholly before or wholly after. This is the ratified race closure.
- The lock order stays `swapMu` → `genMu`(R) → `window.mu` → `lane.mu`. `liveArmed`'s fire only enqueues. Close shuts before it stops.
**Regression guard.** boundaryArmed and liveArmed receive the specs and observe ids already read by the caller (armedLadder/bypassSkips take genMu) — the module never takes genMu, so the lock order holds; prose guard by rule: every comment naming a symbol this item moves or renames (sessionWindow, a.sessionAdvice, startSessionAdvice, extendSessionAdvice, the window's opened/admit/shut) is rewritten, found with `grep -rn 'sessionWindow\|sessionAdvice\b\|startSessionAdvice\|extendSessionAdvice\|window\.\(opened\|admit\|shut\)' internal/agent/`. Test reaches are repointed by rule: every hit of `grep -n 'a\.sessionAdvice\|a\.window\b' internal/agent/*_test.go`. In `TestBriefing_AnInputRacingABoundarySeesItWhole` each input records what it saw (the lane's pending runs, read right after `inputArrived`), and the test asserts "saw the boundary's runs ⇒ window shut at the end"; it lives in internal/agent/briefing_test.go, which item 12 extends.
**Approach (assumed at the header base):**
- Move `sessionWindow` (`agent.go`) into `briefing.go`.
- `announceSessionStart` (`construct.go`) and `fireNewlyArmed` (`agent.go`) delegate to the module. The Agent keeps `swapMu`, the `isDelegate` gates and spec choice.
- Repoint the test reaches into `a.sessionAdvice` and `sessionWindow` mechanically.
**Files:** internal/agent/briefing.go, internal/agent/agent.go, internal/agent/construct.go, internal/agent/loop.go, internal/agent/background.go, internal/agent/sessionadvise.go, internal/agent/sessionadvise_test.go, internal/agent/sessionstart_test.go, internal/agent/sessionadvise_user_test.go, internal/agent/briefing_test.go
**Read first:** internal/agent/construct.go — announceSessionStart; internal/agent/agent.go — sessionWindow.opened, sessionWindow.admit, sessionWindow.shut, fireNewlyArmed;
  internal/agent/sessionadvise.go — startSessionAdvice, sessionAdviseLane.replace; internal/agent/sessionstart_test.go — startRecordingRunner
**Tests:** existing session-start/advise tests. Add `TestBriefing_AnInputRacingABoundarySeesItWhole`: run `inputArrived` concurrently with `boundaryArmed` many times, each input recording the lane's pending runs right after `inputArrived`, then assert an input that saw the boundary's runs leaves the window shut. Run it with `-race` on this one test.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestSessionAdvise|TestSessionStart|TestSessionWindow|TestSetReactions' ./internal/agent/`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBriefing_AnInputRacingABoundarySeesItWhole' ./internal/agent/`
**Commit:** `refactor(agent): one session-briefing module owns the window and session lane`

## 11. sessionBriefing owns the compaction lane, the joins and the Exchange ends

**What:** Depends on item 10.
**Goal:** `sessionBriefing` also owns the compaction-finished lane, and makes the `CompactionFinished` observe call. It exposes these events:
- `foldBegan` returns a staged fold.
- `foldEnded(staged, ran)` adopts the staged runs, or discards them when the fold did not run.
- `joinOpening` and `joinBridge` take a landing func supplied by the Agent.
- `exchangeClosed` and `exchangeAborted`.
These are the only routes the Agent uses for those transitions. The handler call reaches the module as an injected `adviseCaller` func field; production uses `callSessionAdvice`. The opening join keeps the session lane ahead of the compaction lane. A reported run (timeout, error or panic) is never replayed. A cancelled join takes nothing.
**Regression guard.** prose guard by rule: every comment naming a symbol this item moves (compactionAdvice, startCompactionAdvice, joinSessionAdvice, joinCompactionAdvice, staged lane, announceCompactionFinished) is rewritten, found with `grep -rn 'compactionAdvice\|startCompactionAdvice\|joinSessionAdvice\|joinCompactionAdvice\|announceCompactionFinished' internal/agent/`. internal/agent/agent.go is in Files: the `compactionAdvice` field leaves the Agent, and Close stops the compaction lane through the module's `close`.
**Approach (assumed at the header base):**
- `foldFor` (`compact.go`), `joinSessionAdvice` / `joinCompactionAdvice` in `loop.go` (step and refold), and `exchangeClosed`/`exchangeAborted` (`construct.go`) delegate to the module.
- The landing (`attachSessionAdvice`, `bookFiring`, `retireStaleAdvice`) stays in the Agent.
- The background ledger's forget/restore stays a sibling call in `exchangeClosed`/`exchangeAborted`.
**Files:** internal/agent/briefing.go, internal/agent/compact.go, internal/agent/loop.go, internal/agent/construct.go, internal/agent/sessionadvise.go, internal/agent/compactadvise_test.go, internal/agent/agent.go
**Read first:** internal/agent/compact.go — foldFor, announceCompactionFinished; internal/agent/sessionadvise.go — startCompactionAdvice, joinAdvice, callSessionAdvice;
  internal/agent/loop.go — step, refold; internal/agent/agent.go — Close
**Tests:** the existing `TestCompactAdvise*`, `TestSessionAdvise*` and background abort tests.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestCompactAdvise|TestSessionAdvise|TestBackground' ./internal/agent/`
**Commit:** `refactor(agent): the briefing module owns the compaction lane and Exchange ends`

## 12. Briefing tests cross the module's interface

**What:** Depends on item 11. Test-only.
**Goal:** `internal/agent/briefing_test.go` tests the module through its methods alone, with no Agent, using the fake `adviseCaller` and the recording ObserveRunner fake. It covers:
- boundary → live arm → close
- abort, then restore order
- a reported run is never replayed
- fold discard and adopt
- the per-entry deadline and the advise-class default
No test outside `briefing_test.go` reads a lane's internals: the `waiting()` reaches in `sessionadvise_test.go`, `sessionadvise_user_test.go` and `compactadvise_test.go` are rewritten against conversation/Event output or moved into `briefing_test.go`.
**Regression guard.** The lane-reach check greps lane reaches only; `backgroundManager.waiting` is a prompt queue, not a lane. "A delegate starts nothing" is not a module case: the `isDelegate` gates stay in the Agent (item 10), and `TestSessionAdvise_ADelegateAttachesNothing` and `TestCompactAdvise_ADelegateFoldStartsNoRun` cover it. "With no Agent" binds only the module tests; Agent-driven tests moved into briefing_test.go keep their Agent.
**Files:** internal/agent/briefing_test.go, internal/agent/sessionadvise_test.go, internal/agent/sessionadvise_user_test.go, internal/agent/compactadvise_test.go
**Read first:** internal/agent/sessionadvise_test.go — sessionAdviseAgent, answering, blockedUntilCancel, abortAfterTheJoin; internal/agent/compactadvise_test.go — TestCompactAdvise_AFoldThatDidNotRunDiscardsItsRuns;
  internal/agent/sessionadvise_user_test.go — TestSessionAdvise_AUserEntryTakesItsOwnTimeoutElseTheAdviseDefault; internal/agent/sessionstart_test.go — startRecordingRunner; internal/agent/background.go — backgroundManager.waiting
**Tests:** as above.
**Acceptance:** `! grep -n 'Advice\.waiting()\|briefing\.[A-Za-z]*\.waiting()' internal/agent/*_test.go | grep -v briefing_test.go`; `GOMEMLIMIT=2GiB go test -count=1 -run 'TestBriefing|TestSessionAdvise|TestCompactAdvise' ./internal/agent/`
**Commit:** `test(agent): briefing tests cross the module's interface`
