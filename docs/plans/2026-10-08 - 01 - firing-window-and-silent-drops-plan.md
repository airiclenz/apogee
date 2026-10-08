# Firing window binding and silent stream drops — plan

**Goal:** An unpinned headless or daemon run binds the window its server advertises, and an unknown window is announced honestly. A fold that cannot succeed stops repeating every Turn. A reply that lost chunks to a malformed stream, or a recording that dropped one, says so instead of looking clean.

**Date:** 2026-10-08
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** d29cf00e
**Closes:** apogee-headless-window-compaction-loop; apogee-malformed-count-unsurfaced

**Sources:**
- `docs/adr/0024-*.md` (decision 6: `context-window:` is a pin, unset means discover)
- `docs/adr/0018-*.md` (2026-08-02 amendments: the unknown-window ceiling)
- `docs/adr/0028-*.md` (2026-08-13 follow-up: the bind resolves the same pin)
- `docs/adr/0031-*.md` (wire-silent engine, Driver parity)
- `bd show apogee-headless-window-compaction-loop`, `bd show apogee-malformed-count-unsurfaced`, `bd show apogee-record-gzip-discovery-drop`

**Ratified design calls** (owner, 2026-10-08):
- **Firing window:** an unpinned Firing binds the observed window (`beat.ContextWindow`); the pin still wins — the same rule as a session rebind.
- **Unknown window:** keep ADR 0018's conservative ceiling; reword `notice.WindowUnknown` to say what happens.
- **Futile fold:** once a fold saturates, the predictive guard honours the latch and stops re-folding every Turn.
- **Root churn:** the stand-down covers every agent, root included — owner, 2026-10-08.
- **Re-arm:** the stand-down is damping, not a gate (ADR 0018 §7) — it re-arms on growth; owner, 2026-10-08.
- **Malformed on success:** a new note event `domain.MalformedChunksEvent` (the `RefClippedEvent` pattern), rendered by every Driver and saved as a session note.
- **Recorder malformed:** count undecodable events per turn and warn on stderr; the Script schema is unchanged.
- **Recorder gzip:** strip `Accept-Encoding` outbound, as `CassetteRecorder.rewrite` does.
- **Record of the Firing rule:** a dated amendment to ADR 0024, not a new ADR.

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi box: never race-run a whole heavy package; targeted tests as `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; never two test runs at once.

**Out of scope:**
- `apogee-l8s` (capture task; no code fix).
- Extending the stubllm Script schema to replay malformed SSE.
- Changing the 3072-token ceiling's value or `compactMaxTokens`.

**Regression check (2026-10-08, d29cf00e):**
- 1: guard folded (supersedes the observed-window rejection recorded at `wire_firing.go:410-416` and `wire_firing_test.go:975-982`; lands before item 2)
- 2: guard folded (depends on item 1)
- 3: recast — only the predictive call site honours `compactSat`, only with no known window, root included; yields to the `foldTable` doc for `foldOverflow` (`compact.go:240-249`)
- 3 (second round): recast — the latch re-arms on growth (owner call, ADR 0018 §7 honoured: damping, not a gate); only the predictive call site latches, through a helper extracted from `foldFor`; both bite tests calibrate first and must fail on the base tree; a growth re-arm test added
- 4: guard folded — owns every guard the new variant trips (alias, `foldCases`, eventjson `Kinds`, the stream test); yields to ADR 0075 decision 4 (dated amendment)
- 5: guard folded — headless text narration case; one package per `go test`
- 6: guard folded
- 7: guard folded

## 1. A Firing binds the observed window when nothing is pinned — ✅ DONE (2026-10-08)

NOTES (2026-10-08): CONTEXT.md left unedited — its Rebind/pin passage states no Firing exception, so the conditional edit the item names had nothing to change; docs/manual/headless.md already words the notice as "the server did not advertise one and no context-window: pins it", now accurate.

NOTES (2026-10-08): the bind in firingConfig is also gated on beat.Answered, so an unanswered beat binds 0 explicitly rather than relying on its ContextWindow being zero.

NOTES (2026-10-08): added beyond the named tests — a daemon subtest ("a server that advertises a window never says it", binding 131072) in TestDaemonFireSaysOnceWhenTheContextWindowIsUnknown, and the headless advertised-window case as a subtest of TestHeadlessSaysWhenTheContextWindowIsUnknown; the WindowUnknown constant's text and hintNotice's "Budget and auto-compaction inactive" clause are left to item 2.

**What:** Fix for `apogee-headless-window-compaction-loop` defect (2): an unpinned headless/daemon run against a server advertising 1M tokens was managed as a 3072-token window.
**Goal:** a headless or daemon Firing whose entry and top level set no `context-window:` binds `cfg.Context.MaxContextTokens` to the window its beat observed; a pin still wins over the beat; an unanswered beat or a zero observed window binds 0. No `WindowUnknown` notice is printed when an observed window is bound.
**Approach (assumed at the header base):** in `cmd/apogee/wire_firing.go`, `bindFiringConfig` passes an observed window of 0 to `rebindSpecFor` and runs before the beat; bind the beat's `ContextWindow` in `firingConfig` after its `observeServer` beat (see the guard). Rewrite the two comments that justify the 0 (the "nothing beats here" note above the `rebindSpecFor` call and the "aligning them would mean changing what a Firing binds" note near the `WindowUnknown` emit) to state the new rule. Add a dated amendment to ADR 0024 decision 6: every Driver binds pin, else observed; a Firing is no exception. Update the CONTEXT.md Rebind/pin passage if it states the Firing exception.
**Regression guard.** Bind in `firingConfig` after the beat: when `spec.MaxContextTokens == 0` (no pin), set `spec.MaxContextTokens` and `cfg.Context.MaxContextTokens` to `beat.ContextWindow` before `hintNotice` runs. Keep `bindFiringConfig` and its `rebindSpecFor` call free of any beat — the beat dials `spec.Model`, which `rebindSpecFor` returns, and `probeContextConfig` relies on `bindFiringConfig` observing nothing.
The `/schedule` beat (`scheduleWiring.fire`, `schedule.go`) sets `ContextWindow: w.live.observed()`, so the TUI-raised Firing obeys the amendment's "every Driver" too. Prose rule: every comment, test message or doc saying a Firing binds "the pin or nothing", derives its Budget "from configuration alone", or cannot trip the oversize warning is rewritten — `grep -rn 'configuration alone\|pin or nothing\|observed window of 0\|hard-coded observed window\|binds no observed window\|leaves at zero' --include='*.go' --include='*.md' . | grep -v -e CHANGELOG.md -e docs/plans/archived -e docs/skill-runs` (once `SystemShare > 0`, headless stderr and the daemon log can carry the Oversize line).
This supersedes the rejection recorded at `wire_firing.go:410-416` and `wire_firing_test.go:975-982` (a `--parallel 8` box binding its per-slot window), per the ratified Firing-window call; the ADR 0024 amendment names it. Item 1 lands before item 2.
**Files:** cmd/apogee/wire_firing.go; cmd/apogee/wire_firing_test.go; cmd/apogee/headless_test.go; cmd/apogee/daemonfire_test.go; cmd/apogee/schedule.go; cmd/apogee/schedule_test.go; internal/notice/window.go; docs/adr/0024-*.md; CONTEXT.md
**Read first:** cmd/apogee/wire_firing.go — firingConfig, bindFiringConfig; cmd/apogee/wire_settings.go — rebindSpecFor; cmd/apogee/upstream.go — hintNotice; cmd/apogee/wire_firing_test.go — TestFiringConfigSaysWhenTheModelIsNotAdvertised; cmd/apogee/daemonfire_test.go — TestDaemonFireLogsTheCompositionsNotices; cmd/apogee/schedule.go — scheduleWiring.fire; cmd/apogee/probecontext.go — probeContextConfig
**Tests:** a `wire_firing_test.go` table: (pin set, beat 1048576) ⇒ pin; (no pin, beat 1048576) ⇒ 1048576; (no pin, beat unanswered) ⇒ 0. Recast `TestFiringConfigSaysWhenTheModelIsNotAdvertised`'s unpinned rows 1, 2 and 4: unpinned+advertised binds 131072, the hint names the window (row 2 credits base `vendor/model`), row 4 says nothing; add a `ContextWindow: 0` unpinned row so the unknown clause and the bare-notice else stay covered. `TestDaemonFireLogsTheCompositionsNotices` expects `hintNotice("my-alias", HintTrusted, 131072, 131072)` and its comment is rewritten. Add a headless case where an advertised window suppresses the notice, and a `TestScheduleFiring` case binding the session's observed window.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFiringConfig|TestHeadless.*Window|TestDaemonFire.*Window|TestDaemonFireLogsTheCompositionsNotices|TestScheduleFiring' ./cmd/apogee/`
**Commit:** `fix(firing): bind the observed context window when nothing is pinned`

## 2. The unknown-window notice says what apogee does — ✅ DONE (2026-10-08)

NOTES (2026-10-08): hintNotice's clause now reads `(context window unknown — requests bounded by a conservative assumption, Budget inactive)`; it keeps the `context window unknown`/`unknown` and `Budget` substrings, so wire_firing_test.go and upstream_test.go needed no change.
NOTES (2026-10-08): cmd/apogee/daemonfire_test.go, cmd/apogee/wire_firing_test.go and cmd/apogee/headless_test.go are listed in Files but reference notice.WindowUnknown by symbol (no hand-typed copy), so they needed no edit; likewise the TUI heartbeat "window unknown" case asserts via unknownWindowNote (= notice.WindowUnknown), which window_test.go pins verbatim — only heartbeat_test.go's comment claiming compaction does nothing changed.

**What:** Fix for `apogee-headless-window-compaction-loop` defect (1): the notice says automatic compaction is inactive while ADR 0018's conservative ceiling bounds every request. Depends on item 1.
**Goal:** `notice.WindowUnknown` reads exactly `context window unknown — apogee bounds each request by a conservative assumption and the Budget is inactive; set context-window: in config.yaml`, and every Driver that emits it (TUI rebind note, headless stderr, daemon log) emits that string.
**Approach (assumed at the header base):** change the constant in `internal/notice/window.go`; the TUI's `unknownWindowNote` and `firingConfig` already reference it. Rule for prose: every live doc or comment quoting "automatic compaction and the Budget are inactive" is updated (`grep -rn "compaction and the Budget are inactive" --include='*.go' --include='*.md' . | grep -v -e docs/plans/archived -e docs/skill-runs -e CHANGELOG.md`); archived plans and CHANGELOG history stay. `hintNotice`'s own "context window unknown" clause gets the same correction if it claims compaction is off.
**Regression guard.** Widen the prose rule to every live sentence saying compaction is inactive or does nothing on an unknown window: `grep -rniE -A1 'auto-compaction|automatic compaction|compaction' --include='*.go' --include='*.md' --include='*.yaml' . | grep -iE 'inactive|do nothing'` (same excludes); the files it finds are in Files. Any rewording of `hintNotice`'s clause keeps the `context window unknown` and `Budget` substrings pinned by `wire_firing_test.go:1001` and `upstream_test.go:1250` (`unknown`, `Budget`), or updates those tests too.
The eight goldens rendering the old note are regenerated with `GOMEMLIMIT=2GiB go test -race -count=1 -run '<the Acceptance names>' ./cmd/apogee/ -update`, and the diff is read before committing.
**Files:** internal/notice/window.go; internal/notice/window_test.go; internal/notice/doc.go; cmd/apogee/upstream.go; cmd/apogee/daemonfire.go; cmd/apogee/daemonfire_test.go; cmd/apogee/wire_firing_test.go; cmd/apogee/headless_test.go; internal/tui/heartbeat_test.go; internal/provider/discovery.go; internal/agent/rebind.go; internal/agent/routedspawn_test.go; internal/config/defaults/config.yaml; docs/manual/headless.md; docs/manual/configuration.md; CONTEXT.md; cmd/apogee/testdata/frames/{t15-cancelled-delegation,t19-tools-umbrella-folded,popup-sessions,popup-approval,popup-picker,popup-dropdown,workflow-stages,t04-step-cap-block}.txt
**Read first:** internal/notice/window.go — WindowUnknown; internal/notice/window_test.go — TestWindowUnknownIsTheOneSpelling; cmd/apogee/upstream.go — hintNotice; internal/tui/heartbeat.go — unknownWindowNote; internal/tui/heartbeat_test.go — TestDelayedConnectNotesOnce; cmd/apogee/e2e_popups_test.go — popup-* tuitest.Golden calls; internal/tuitest/golden.go — Golden, updateGolden
**Tests:** `TestWindowUnknownIsTheOneSpelling` pins the new text; `TestWindowUnknownNamesTheKeyThatFixesIt` still passes; the TUI heartbeat "window unknown" case asserts the new string as rendered; the eight regenerated goldens carry the new note.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/notice/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDelayedConnect|Window' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Window|Hint' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EPopupFramesPrompts|TestE2EPopupFramesLists|TestE2EDelegationStepCap|TestE2EOutcomeCancelledDelegationCarriesTheFailureTone|TestE2EToolsUmbrellaFoldsOverThreshold|TestE2EWorkflowStages' ./cmd/apogee/`
**Commit:** `fix(notice): say the unknown window bounds requests instead of disabling compaction`

## 3. A saturated fold stops re-folding every Turn

**What:** Recast at the regression check (2026-10-08). Fix for the churn in `apogee-headless-window-compaction-loop`: with no window, the protected prefix plus the summary cannot fit the 3072-token ceiling, the `foldEstimate` row saturates, but the predictive guard's `foldOverflow` row reads no latch and folds again on every Turn of every agent.
**Goal:** once an agent's fold has saturated (`turnLifecycle.foldSaturated`), the predictive guard and the estimate-driven trigger do not fold again on that agent while no window is known and it has saturated; the give-up message is emitted once per agent; the run continues on its full history.
**Approach (assumed at the header base):** in `internal/agent`, the predictive guard (`requestExceedsWindow`, called from the loop) folds through `refold` (`foldOverflow`) without consulting the saturation latch; gate that call site as the guard says. Do not change `compactUnknownWindowTranscriptTokens` or `compactMaxTokens`.
**Regression guard.** Apply every G: line of regression-2.md as binding — leave foldTable[foldOverflow].open and refold untouched; gate only the predictive call site (the requestExceedsWindow check the loop makes before sending), only when !deriveGrowthBounds(b).windowKnown, reading a.turns.compactSat directly (never autoFoldArmed, never compactFailed); the known-window path, the reactive emergency fold after a wire overflow, and /compact (foldOnDemand) keep today's behaviour, each pinned by a test (siblings of TestFailedFoldStandDownDoesNotBlockTheEmergencyFold and TestCompactOnDemandIgnoresTheStandDownLatch). Reword the Goal as "the predictive guard and the estimate-driven trigger do not fold again on that agent while no window is known and it has saturated". Owner call (Ratified design calls line: "**Root churn:** the stand-down covers every agent, root included — owner, 2026-10-08."): with no window known, a predictive fold whose result still exceeds the unknown-window ceiling sets compactSat too, so a depth-0 agent stands down mid-Exchange as a child does; openExchange's latch handling is unchanged. Bite test per regression-2's G: — the child's first user message (delegation brief) plus summary exceed the ceiling, sized with unknownWindowCeilingChars, a.midExchangeCompaction = true, counted with countCompactionErrors and scriptedCompactResponder; add a root (depth-0, no window, mid-Exchange) bite test too. The foldTable doc comment for foldOverflow stays true (the row itself reads no latch; the gate is at the call site) — say so in the item so the implementer does not "fix" it.
The item yields to that `foldTable` doc (`compact.go:240-249`): the gate lives at the call site in `loop.go`, and the doc comment is not edited.
**Regression guard (second round).** Owner call (Ratified design calls line: "**Re-arm:** the stand-down is damping, not a gate (ADR 0018 §7) — it re-arms on growth; owner, 2026-10-08."): when compactSat is set, record the transcript estimate at that moment (cleared with the latch); the gated predictive guard stays quiet until the transcript estimate exceeds uncalibratedRoomMargin × that recorded estimate, then fires once more (a normal predictive refold) and re-records. Both bite tests call `calibrate(a)` (`predictiveguard_test.go`) before Step and size with `unknownWindowCeilingChars` after calibrating — stub turns carry no usage, so uncalibrated the guard fires only past 2× the ceiling — and must fail on the base tree. At the predictive call site only — when refold returns foldFolded and !windowKnown and historyExceedsAllocation() — call foldSaturated and emit the existing saturation notice through one helper extracted from foldFor (same wording and remedy suffix); a skip or decline never latches; refold/foldFor and the reactive overflow fold do not latch ("overflow never saturates", `compact_test.go` `TestFoldTable`, stays green). The latch (with its re-arm) persists across Exchanges until the history drops under the ceiling (/clear, Rebind, RestoreSession), so "stops re-folding every Turn" means damped for the rest of the session, not cleared at each Exchange. ADR 0018 §7 is honoured, not reversed — no ADR amendment; the `loop.go` comments around the predictive guard must describe this damping.
**Files:** internal/agent/loop.go; internal/agent/compact.go; internal/agent/turn.go; internal/agent/unknownwindow_test.go; internal/agent/predictiveguard_test.go; internal/agent/autocompact_guard_test.go
**Read first:** internal/agent/loop.go — step (predictive guard block), refold, requestExceedsWindow; internal/agent/compact.go — foldFor (saturation notice), foldTable, deriveGrowthBounds, historyExceedsAllocation;
internal/agent/turn.go — foldSaturated, autoFoldArmed; internal/agent/predictiveguard_test.go — calibrate; internal/agent/unknownwindow_test.go — unknownWindowCeilingChars
**Tests:** a child bite test in `unknownwindow_test.go`: no window, `calibrate(a)` before Step, the child's first user message (delegation brief) plus the summary over the ceiling (sized with `unknownWindowCeilingChars`), `a.midExchangeCompaction = true`, `scriptedCompactResponder` scripted for 10 tool Turns ⇒ at most one compaction call (`countCompactionErrors`) and exactly one saturation message (fails on the base tree); the same for a root (depth-0, no window, mid-Exchange, calibrated). A growth re-arm test: after the latch, a transcript past `uncalibratedRoomMargin` × the recorded estimate refolds exactly once more. A skip or decline never latches; `TestFoldTable`'s "overflow never saturates" stays green. With `compactSat` set: the reactive emergency fold after a wire overflow still runs, `/compact` still folds, and a known-window predictive fold still fires. `TestPredictiveGuardWithoutAKnownWindowUsesTheConservativeCeiling` still passes for the first, unsaturated fold.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'UnknownWindow|PredictiveGuard|AutoCompact|EmergencyFold|CompactOnDemand|StandDown|GrowthBounds|FoldTable' ./internal/agent/`
**Commit:** `fix(agent): stop re-folding every turn once a fold has saturated`

## 4. The engine emits a note when a successful reply dropped malformed chunks

**What:** Fix for `apogee-malformed-count-unsurfaced`, engine half: `collectCompletion` drops `Delta.MalformedChunks` on a successful `DeltaDone`.
**Goal:** a completion whose stream ended successfully with `MalformedChunks > 0` yields exactly one `domain.MalformedChunksEvent{Count}` from the loop; its `Notice()` returns `the reply dropped N malformed stream chunks — text may be missing` (singular `chunk` for 1); a fault path is unchanged and emits no extra event.
**Approach (assumed at the header base):** add the variant to `internal/domain/events.go` next to `RefClippedEvent` (note, never a fault); carry the count through `completion` in `internal/agent/collect.go`; emit after the Done handling in `streamResponse` / the loop's completion path in `internal/agent/loop.go`.
**Regression guard.** Item 4 owns every guard a new domain Event variant trips at build or test time (the apogee.go alias, tui foldCases, the eventjson Kinds count, the cmd/apogee stream test and any sealed-set test regression-3.md names), so the tree is green after item 4's commit; add those files to its Files and targeted runs of those tests to its Acceptance. Item 5 keeps the Driver rendering and the session note. Every Acceptance command names ONE package per `go test` invocation (Pi box) — split item 4's agent+provider and item 5's eventjson+run lines.
Concretely: `MalformedChunksEvent = domain.MalformedChunksEvent` in `apogee.go`'s variant block and `_ apogee.MalformedChunksEvent` in `example_test.go`; a `foldCases` row with `wantEntries` 0 (item 5 flips it to 1); emit from `streamResponse`/`respondAndReview`, never `collectCompletion` (`TestCollectCompletionWithoutObserverIsSilent` pins it silent and the compaction summarizer shares it).
The line kind is `malformed_chunks` with data `{"count":N}`, placed before the frames in `Kinds()`; `TestKindsAreTwentyTwo` is re-pinned to 23 and renamed; every "twenty-two" line-kind count becomes twenty-three (`grep -rn "twenty-two" internal/eventjson docs/manual/headless.md`); `docs/manual/headless.md` lists the kind; `TestHeadlessFormatJSONStreamsEveryEvent` emits the event after `WorkflowPhaseEvent` and its "nineteen" comment is fixed. The item yields to ADR 0075 decision 4: its count trail gains a dated amendment for the new kind.
**Files:** internal/domain/events.go; internal/domain/events_test.go; internal/agent/collect.go; internal/agent/loop.go; internal/agent/malformed_test.go; apogee.go; example_test.go; internal/tui/fold_test.go; internal/eventjson/encode.go; internal/eventjson/encode_test.go; cmd/apogee/headless_test.go; docs/manual/headless.md; docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md
**Read first:** internal/agent/loop.go — streamResponse; internal/agent/collect_test.go — scriptedDeltas; internal/domain/events.go — RefClippedEvent; apogee.go — Event variant alias block; internal/tui/fold_test.go — foldCases; internal/eventjson/encode.go — Kinds; internal/eventjson/encode_test.go — TestKindsAreTwentyTwo; cmd/apogee/headless_test.go — TestHeadlessFormatJSONStreamsEveryEvent
**Tests:** agent test on the Delta fake — `newAgent(baseConfig(sink), scriptedDeltas{…text, a tool call, {Kind: provider.DeltaDone, MalformedChunks: 2}})` (no malformed SSE: the Script schema is out of scope) ⇒ one event with Count 2 and the turn otherwise succeeds; zero malformed ⇒ no event; a fault path ⇒ the existing fault and no note. `Notice()` table for 1 and 2. The eventjson golden carries the new kind and count.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Malformed|CollectCompletion' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Malformed' ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestEveryDomainEventVariantIsAliased .`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFoldEvent|TestProgressSaveTrigger' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessFormatJSONStreamsEveryEvent|TestManualListsEveryEventLineKind' ./cmd/apogee/`
**Commit:** `fix(agent): report malformed stream chunks a successful reply dropped`

## 5. Every Driver shows and records the malformed-chunk note

**What:** Fix for `apogee-malformed-count-unsurfaced`, Driver half. Depends on item 4.
**Goal:** `MalformedChunksEvent` renders its `Notice()` text as a note in the TUI transcript, the headless text transcript and the headless JSON stream, and is persisted as a session note entry — the same Drivers and routes `RefClippedEvent` takes.
**Approach (assumed at the header base):** add a case beside each `RefClippedEvent` case: `internal/tui/transcript.go`, `internal/run/transcript.go`; follow `RefClippedEvent`'s session-recording path (`EntryKindNote`). The headless JSON kind lands in item 4.
**Regression guard.** Acceptance one package per `go test` invocation; see the item 4 entry for which guards moved to item 4.
The headless text transcript gets its own case: `narrationSink.narrate` (`cmd/apogee/headless.go`) prints nothing for `RefClippedEvent` today, so add a `MalformedChunksEvent` case printing its `Notice()` line, with a `cmd/apogee` test. Flip item 4's `foldCases` row to `wantEntries` 1.
**Files:** internal/tui/transcript.go; internal/run/transcript.go; internal/run/transcript_test.go; internal/tui/transcript_test.go; internal/tui/fold_test.go; cmd/apogee/headless.go; cmd/apogee/headless_test.go
**Read first:** internal/run/transcript.go — transcriptFold.fold; internal/tui/transcript.go — transcript.apply, addRefClipped; internal/tui/transcript_test.go — TestRefClippedNoteIsOneHostLineAtItsOwnRun; internal/tui/fold_test.go — foldCases
**Tests:** per Driver, feed a `MalformedChunksEvent{Count: 2}` and assert the exact `Notice()` string appears; a `--format text` headless run prints it; a session round-trip shows a note entry.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/run/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Malformed|RefClipped|TestFoldEvent|TestProgressSaveTrigger' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Malformed' ./cmd/apogee/`
**Closes:** apogee-malformed-count-unsurfaced
**Commit:** `fix(drivers): show and record the malformed-chunk note`

## 6. stubllm record asks the upstream for an uncompressed reply

**What:** Fix for `apogee-record-gzip-discovery-drop`: the recorder forwards the client's implicit `Accept-Encoding: gzip`, so `capture` sees gzip bytes and `fileProbe` drops the discovery reply.
**Goal:** `stubllm record` against an upstream that gzips whenever the request accepts gzip records the discovery block, a non-streamed reply and a streamed reply exactly as against an uncompressed upstream; the client still decodes every reply.
**Approach (assumed at the header base):** in `NewRecorder`'s `Rewrite` (`internal/stubllm/record.go`), delete `Accept-Encoding` on the outbound request, mirroring `CassetteRecorder.rewrite` in `internal/stubllm/capture.go`.
**Regression guard.** The `ReverseProxy` has a nil Transport, so `DefaultTransport` adds its own implicit gzip and decodes the reply itself: word the `record.go` comment by that mechanism (the recorder drops the client's `Accept-Encoding` so its transport decodes the reply), not as "asks for an uncompressed reply". Add that clause to `docs/design/test-drivers.md` "Recording a fixture", whose "forwards `/v1/*` verbatim" would otherwise be false for this header. The planned gzip test stays valid.
**Files:** internal/stubllm/record.go; internal/stubllm/record_test.go; docs/design/test-drivers.md
**Read first:** internal/stubllm/record.go — NewRecorder (Rewrite), Recorder.capture, Recorder.fileProbe; internal/stubllm/capture.go — CassetteRecorder.rewrite; internal/stubllm/record_test.go — TestRecorderCapturesDiscovery, TestRecorderRecordsANonStreamedReplyAsText, recorderProxy; docs/design/test-drivers.md — Recording a fixture
**Tests:** a hand-made `httptest.NewServer` upstream that gzips `/v1/models`, `/props` and completions when `Accept-Encoding` contains gzip; cases modelled on `TestRecorderCapturesDiscovery` ("a list and props") and `TestRecorderRecordsANonStreamedReplyAsText`, plus a streamed reply. Must fail on the base tree.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/stubllm/`
**Closes:** apogee-record-gzip-discovery-drop
**Commit:** `fix(stubllm): ask the upstream for an uncompressed reply when recording`

## 7. stubllm record warns about stream events it could not decode

**What:** Fix for the recorder half of `apogee-malformed-count-unsurfaced`: `fillFromStream` `continue`s on an unmarshal failure, silently. Depends on item 6 (same files).
**Goal:** for each recorded turn whose stream held K > 0 undecodable data events, `stubllm record` writes one line `turn N: K undecodable stream events dropped` to its warning output; a clean stream writes nothing; the Script YAML is unchanged.
**Approach (assumed at the header base):** count the failures in `fillFromStream` (`internal/stubllm/record.go`); report through the recorder's existing log/stderr writer (inject an `io.Writer` on the `Recorder` if none exists, wired to stderr by the `stubllm record` command).
**Regression guard.** N is the 1-based position in `Script().Turns` (not `capture.n`, a 0-based arrival number that a failed request also consumes). Carry the count on the capture or in a side map keyed by n, never as a `Turn` field (Turn is the Script schema). Write the warnings in `Close`, after `settleInflight`, in Script order — never from `finish`, which runs on each request's own proxy goroutine. Wire the writer as `cmd.ErrOrStderr()` in `newRecordCommand` so `main_test`'s `SetErr` captures it; a nil writer means `io.Discard`, so the four `NewRecorder` call sites keep working.
**Files:** internal/stubllm/record.go; internal/stubllm/record_test.go; cmd/stubllm/main.go; cmd/stubllm/main_test.go
**Read first:** internal/stubllm/record.go — capture.fillFromStream, capture.turn, Recorder.finish, Recorder.Close, NewRecorder; cmd/stubllm/main.go — newRecordCommand; cmd/stubllm/main_test.go — run; internal/stubllm/record_test.go — recorderProxy
**Tests:** an upstream streaming two malformed `data:` events among valid ones ⇒ the warning writer holds exactly `turn 1: 2 undecodable stream events dropped`; a clean stream ⇒ empty; a `cmd/stubllm` case sees the line on the command's `SetErr` writer.
**Acceptance:**
- `go build ./cmd/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/stubllm/`
**Commit:** `fix(stubllm): warn when a recorded stream had undecodable events`
