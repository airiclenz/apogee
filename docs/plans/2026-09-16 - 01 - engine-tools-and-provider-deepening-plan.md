# Engine, tools and provider deepening (2026-09-16 architecture review, plan B) — plan

**Goal:** land the engine-, tools- and provider-side candidates of
`docs/reviews/architecture-review-2026-09-16.html` that survived fact-checking, reduced to what the
code and the ADRs support: one Delta collector with the compaction re-stream bug fixed (6); one fold
aftermath (16); one standing-block table (13) and one skill-block renderer (22); the fingerprint
ladder retired (8); one `domain.Usage` (19); one upstream fault classifier (25); the engine's test
upstream scripted through stubllm (20); the write-side scope value (3); one confinement handoff (4);
`gitRead`/`gitWrite` (17); one syntax engine (18). Gated on sibling plan A (`2026-09-16 - 00`)
archiving.

**Date:** 2026-09-16
**Status:** unexecuted
**sized for:** ~200k-context host

**Regression check (2026-09-16, 1da6d6fb):**
- 3: guard folded — decision: `foldResult` (new type) mapped by `refold` onto the existing `foldOutcome`; gate column; `skipped` + error returns; one `emitCompactionError` helper.
- 4: guard folded — `fences []string` per row (context files carry header AND footer).
- 5: guard folded — `MergeSystem` before the wire projection; the three `TestPromptSeam_*` injection pins named.
- 6: guard folded — `internal/probe/doc.go` in Files; the library grep scoped to import lines.
- 7: guard folded — grep narrowed (`middle rung` names other ladders); `probemodel.go` Long text and `ConfigDir` doc in Files.
- 8: guard folded — decision: `session.Usage`'s field names and order, `usageFrame` stays keyed, `domain/doc.go` line; supersedes `internal/run/run.go` `sessionUsage`'s "different field order … written out field by field" note.
- 9: guard folded — decision (same rule); `model.go`/`transcript.go` and their tests in Files, phantom `internal/tui/e2e_usage_test.go` replaced; yields to `internal/tui/fold.go`'s "field set matches session.Usage exactly" note.
- 10: guard folded — three unlisted `Cumulative*` test files in Files.
- 11: guard folded — classify sniffs the raw body; `retryable` read by `inBandErrorDelta` only; yields to `stream.go`'s Retryable doc.
- 12: guard folded — decision: narration+tool-call Turn and `chunks:` (inherited by 13–15); fake-retirement rule + grep; listener-less constructor, pipe transport, `WithMaxRetries(0)`; `maxTokRecordingResponder` struck from Out of scope.
- 13: guard folded — inherits 12; `wire.go`/`server.go` in Files; `compactSpyResponder`; the effort-None assertion recast.
- 14: guard folded — inherits 12; no pair to delete; `authRecorder`/`wireUpstream` keep a recorder; depends on 13.
- 15: guard folded — inherits 12 (`writeToolCallWithText` site).
- 16: guard folded — decision: conditional golden sentence, the retired pop-up gate's references dropped; `TestE2EColdStartHeartbeat` exempt by name; `internal/tui/testdata/` dropped.
- 17: guard folded — tool-result turn scripted with static text; the import count recounted.
- 18: guard folded — decision: `ApprovedEscape` in the regex; the value IS the marker's `writeTarget` enlarged (yields to `docs/design/confinement-execution-contract.md` §3.2); shims until item 20; bite grep; `doc.go` line here.
- 19: guard folded — decision: `ApprovedEscape` in the regex.
- 20: guard folded — decision: `ApprovedEscape` in the regex; copy/move roots reworded.
- 21: guard folded — decision: `doc.go` file-map sentence dropped; the source scan stays, re-pointed (yields to ADR 0051's 2026-08-24 amendment).
- 23: guard folded — decision: raw `SubprocessResult` premise; per-verb timeout kept.
- 24: guard folded — decision: raw `SubprocessResult` premise.
- 25: recast — decision: `syntaxcheck.CheckGo` without the blank short-circuit; first-error-plus-count wording kept.
- 25 (round 2): guard folded — the What's "shared renderer extracted from `syntaxtrailer.go`" struck to match the guard; `syntaxtrailer.go` dropped from Files (the trailer's `line N: msg` rendering and the door's `abs:line:col: msg` differ by design).

## Authoritative sources

- `docs/reviews/architecture-review-2026-09-16.html` — fact-checked 2026-09-16 at `1da6d6fb`; **where an item disagrees with the review, the item wins**.
- ADR 0001, 0010, 0018 D5/D6, 0021 §3, 0023 §6 + addenda, 0026 §3, 0043, 0046 D4 (2026-09-15 note), 0049, 0051 (2026-08-24 amendment), 0056 D4, 0059 D5, 0062 D1–D2, 0065 §6–7, 0071 D1 + 2026-09-07 amendment, 0075 D3/D4/D10, 0076 A9; `docs/design/confinement-execution-contract.md` §2.2, §3, §4; `docs/plans/archived/2026-09-15 - 01` item 16 (`lookGit`/`RunGitQuery` stay) and `2026-09-15 - 00` items 8, 14.

## Review verdicts (fact-check 2026-09-16; HEAD 1da6d6fb)

| # | Candidate | Verdict |
|---|---|---|
| 3 | Write-side scope | **IN, reduced** (18–21) — no `security.*` alias retirement, `readScope` untouched |
| 4 | One confinement handoff | **IN, reduced** (22) — handoff in `internal/subprocess`; console keeps "does not confine" |
| 6 | One Delta collector | **IN, reduced** (2) — two capped wordings stay (ADR 0046 D4) |
| 8 | Fingerprint ladder retired | **IN** (6–7) — `internal/library` goes entirely; ADR 0021 §3 amended |
| 13 | Standing block table | **IN, reduced** (4) — wrap-up stays on `AppendToSystem` |
| 14 | Text grammar seam | **DENIED** — reverses ADR 0071 D1 + 2026-09-07 amendment; pinned ids and `{"raw"}` parity |
| 16 | One fold aftermath | **IN** (3) |
| 17 | gitRead/gitWrite | **IN, reduced** (23–24) — per-verb hardening; `lookGit`/`RunGitQuery` stay (item 16 of `2026-09-15 - 01`) |
| 18 | One syntax engine | **IN, reduced** (25) — Go half; non-Go keeps "no diagnostics available" |
| 19 | domain.Usage | **IN** (8–10) — wire/on-disk structs keep their keys and shapes |
| 20 | Scripted engine upstream | **IN** (12–17) — in-process transport over stubllm's handler, never a Delta player; 10 fakes stay |
| 22 | Skill-block renderer | **IN** (5) |
| 23 | Run index | **DENIED** — unmeasured cost; a second pointer structure |
| 25 | Fault classifier | **IN** (11) — classify→render, texts unchanged |
| 26 | Delegate state value | **DENIED** — plan `2026-09-15 - 00` item 8's call; nothing new since |
| 29 | `resolveWorkdir` twin, interject/buildRequest twins, `MergeSystem` ride along; `observeThinking` (fingerprint inputs) and the approval-cache assert **DENIED** |

## Ratified design calls (owner, 2026-09-16)

- **Scope:** the table above; denied rows recorded here only.
- **Gate:** item 1 verifies plan A is archived.
- **Diagnostics (18):** non-Go files keep "no diagnostics available"; only the Go half changes.
- **Goldens (20):** should any `cmd/apogee` `TestE2E*` frame golden differ, and only by the head/tail framing, it is regenerated after a reviewed diff and listed in NOTES; any other diff is a migration defect (pop-up frames are held goldens).
- **ADR text:** dated in-place amendments, never rewrites.

## Standing requirements

- `skills: coding-standards`.
- Deviations land as a dated `NOTES:` line under the item.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.
- Items cite symbols, never line numbers.

## Out of scope

- Candidates 14, 23, 26 (denied); `security.resolveInRoot`/`safeOpen`/`ErrPathEscape` aliases; `readScope` and `readWorkspaceFileBounded`; `tools.lookGit`, `RunGitQuery`, `internal/agent/treesnapshot.go`; the `console` package's confinement boundary; the ten uncollapsible fakes (`windowResponder`, `blockingResponder`, `foldBlockingResponder`, `blockAtResponder`, `routedResponder`, `gruntResponder`, `requestLogResponder`, `menuRecorder`, `modelBindingResponder`, `closingResponder`); `domain.ModelFingerprint`'s High/Low tiers (the type stays); plan A's items.

---

## 1. Verify plan A is archived — ✅ DONE (2026-09-16)

NOTES (2026-09-16): gate passed — `docs/plans/archived/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md` exists (archived at commit 044c6c5a) and `ls docs/plans/2026-09-16\ -\ 00*` matches nothing; the only untracked plan is `2026-09-16 - 02 - demo-storyboard-rig-plan.md`, outside this gate.

**What.** Confirm `docs/plans/archived/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md`
exists and no file named `2026-09-16 - 00 - *` remains under `docs/plans/`. Plan A rewrites
`cmd/apogee/headless.go`, `cmd/apogee/schedule_test.go` and `internal/tui/*` files this plan also
touches. If the check fails, stop the run.

**Files:** none.
**Read first:** docs/plans/archived/2026-09-15 - 01 - host-config-and-tools-deepening-plan.md — archived plans keep their exact basename (the acceptance's `test -f` path is the right spelling); docs/plans/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md — still untracked and unarchived in the working tree at BASE, so the gate currently fails as intended

**Tests.** none.

**Acceptance.**
- `test -f "docs/plans/archived/2026-09-16 - 00 - host-config-and-tui-deepening-plan.md" && ! ls docs/plans/2026-09-16\ -\ 00* 2>/dev/null`

**Commit:** none (no-op item).

## 2. One collector from `provider.Delta` to reply; a transient fault re-streams the summary — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `collectCompletion` takes the `provider.Request` and calls `a.upstream.Stream` itself (a method on `*Agent`, for the stripper) rather than the plan's `deltas iter.Seq[provider.Delta]` — the acceptance grep (`.Stream(` → 0 in loop.go/compact.go) requires the stream call to live in collect.go; the table test scripts the Deltas through a fake Responder instead.
NOTES (2026-09-16): `joinThinking` moved from loop.go to collect.go with the strip it now owns; the `reply` type is replaced by `completion` (the plan's name), and `assembleResponse` reads the already-stripped content instead of stripping again.
NOTES (2026-09-16): consequential edit — internal/agent/doc.go: made necessary by the new collect.go (the package map's structural test `TestDocMapNamesEveryFile` names every file).

**What.** New `internal/agent/collect.go`: `collectCompletion(ctx, deltas iter.Seq[provider.Delta],
observe func(provider.Delta)) completion` returning `{content, thinking, toolCalls, finish, usage,
served, failed, overflow, retryable, errMsg}` and owning strip + `joinThinking`. `loop.go`
`streamResponse` = collect with the Token/Reasoning/Thinking observer (the `emitted`/`reasoned`
high-water logic stays in the observer), then Calibrate and the UsageEvent at DeltaDone exactly
where they fire today; `compact.go` `compactCompleter.Complete` = collect silently, then the
`Maintenance=true` usage record (no Calibrate). Usage is RETURNED by the collector, never recorded
inside it. The two capped-empty faults stay per caller with their two wordings
(`cappedReplyErrFmt` post-cascade in `reviewedOutcome`; `cappedSummaryErrFmt` + cause in
`Complete`) — ADR 0046 D4's 2026-09-15 note. **Fix (confirmed defect):** `Complete` dropped
`Delta.Retryable`; it now re-streams ONCE on a retryable in-band fault, honouring ctx, with no
`StreamResetEvent` (nothing streamed) — a transient 502 during a summary no longer latches
`compactFailed` for the Exchange. `ctx.Err()` masquerade checks stay at both callers.

**Files:** `internal/agent/collect.go`, `internal/agent/collect_test.go`, `internal/agent/loop.go`, `internal/agent/compact.go`, `internal/agent/compact_test.go`
**Read first:** internal/agent/loop.go — reply, streamResponse, emitVisibleDelta, emitReasoningDelta, respondAndReview, assembleResponse, joinThinking, restreamHoldoff; internal/agent/compact.go — compactCompleter.Complete, cappedSummaryErrFmt, summaryTruncatedMarker; internal/provider/stream.go — Delta, DeltaDone (terminal), Delta.Retryable; internal/agent/compact_test.go — cappedSummaryResponder, summaryEffortResponder, foldOnce, TestCompactCancelMidSummaryLeavesConvUntouched; internal/agent/overflow_test.go — restreamHoldoff override pattern; internal/agent/autocompact_guard_test.go — scriptedCompactResponder; internal/agent/usagetally_test.go — Maintenance-flag pins; internal/agent/loop_cleanup_test.go — UsageEvent hop pin

**Tests.** New table test over scripted Deltas (content/thinking/calls/finish/usage/fault kinds);
new: a Stream yielding `DeltaError{Retryable:true}` once then a good summary → the conversation
folds and the upstream saw two summary requests (`restreamHoldoff` shortened as `overflow_test.go`
does). Existing: `streamsuppress_test.go`, `loop_cleanup_test.go`, the 15 `compact_test.go` tests,
`overflow_test.go`, `usagetally_test.go`, `emptyreply_test.go` unchanged.

**Acceptance.**
- `go test ./internal/agent -run 'Collect|Stream|Compact|Overflow|Usage|EmptyReply|Suppress'`
- `grep -c '\.Stream(' internal/agent/loop.go internal/agent/compact.go` → 0 outside `collect.go`.

**Commit:** `fix(agent): one Delta collector; a transient fault during a summary re-streams instead of failing the fold`

## 3. One fold aftermath — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `foldFor` takes the Turn index alongside ctx and kind (`foldFor(ctx, turn, kind)`) — the ErrorEvent base needs it and the two loop-driven wrappers already carry it; `Compact` passes `a.turns.index`.
NOTES (2026-09-16): `foldResult` carries the fold's own `error` value (returned by `Compact` as-is, `ctx.Err()` on a cancel, `domain.ErrInputPending` on the on-demand refusal) rather than a text re-wrapped with `errors.New` — `errors.Is` on the refusal and the cancel keeps working exactly as before; the fault text on the event is `err.Error()` as today.
NOTES (2026-09-16): the re-entrancy guard (`compacting`) is now held for the on-demand row too (foldFor owns it for every row); the two automatic gates still check it, the on-demand gate does not.
NOTES (2026-09-16): `internal/agent/emergencyfold_test.go` (in Files) needed no change — its six direct `emergencyFold` calls pass against the thin wrapper; the new table test lives in `compact_test.go` (`TestFoldTable`, 32 subtests).

**What.** `internal/agent/compact.go`: `foldFor(ctx, kind foldKind) foldOutcome` (kinds onDemand |
estimate | overflow; outcomes folded | declined | cancelled | faulted(text)) owns the re-entrancy
guard, cancel-vs-fault, the `Source:"compaction"` ErrorEvent, bridge + `anchorAtBridge` inside an
open Exchange, and a latch TABLE in place of the three prose comments: onDemand = no latch, no
bridge, no event; estimate = `foldFaulted` + `foldStandDownSuffix` only when inExchange, saturation
check only after a fold that RAN, bridge iff inExchange; overflow = no estimate latch, bridge
always (ADR 0018 D5/D6). `Compact`, `autoCompact`, `emergencyFold` stay as thin wrappers (six
tests call `emergencyFold` directly); `loop.go` `refold` reads the outcome instead of bool +
`ctx.Err()`, still `restoreDeferred` first and `armRequest` after. `res.Skipped` stays silent on
all three. Ride-along: the stale "Library store … flushed here (library.Store.Close)" sentence in
`agent.go`'s `Close` doc is deleted. `growthBounds` untouched.

**Regression guard.** `foldOutcome` is an existing loop.go type consumed by two exhaustive switches in step() — the new entry returns a NEW type (`foldResult`), and `refold` maps it onto the existing `foldOutcome` (faulted→`foldDeclined`, cancelled→`foldCancelled`; step()'s two switches and `TestRefoldOutcomeMapping` stay on the three-value type). The latch table gains a gate column: onDemand = inExchange→`ErrInputPending` only (no `compactionEnabled`, no latches — `/compact` under `auto-compact: false` never declines silently); estimate = `shouldAutoCompact` + `compacting`; overflow = `compactionEnabled` + `compacting`. The result carries a `skipped` flag, and `Compact` keeps returning `(skipped, err)` — `ctx.Err()` on cancelled, `errors.New(text)` on faulted — so `worker.go`'s `errors.Is(err, context.Canceled)` and `commandrun.go`'s "nothing to compact" note read as today. The saturation notice is emitted through the same one helper (`emitCompactionError(turn, msg)`) so the acceptance count reaches 1 without dropping or re-sourcing that event.

**Files:** `internal/agent/compact.go`, `internal/agent/loop.go`, `internal/agent/agent.go`, `internal/agent/compact_test.go`, `internal/agent/emergencyfold_test.go`
**Read first:** internal/agent/compact.go — Compact, fold, autoCompact, emergencyFold, shouldAutoCompact, foldStandDownSuffix, compactCompleter; internal/agent/loop.go — refold, foldOutcome, step (two `switch a.refold` sites), turnRun.foldSpent; internal/agent/turn.go — foldFaulted, foldSaturated, anchorAtBridge, openExchange; internal/agent/overflowrecovery_test.go — TestRefoldOutcomeMapping, recoveryResponder; internal/agent/autocompact_guard_test.go — countCompactionErrors, compactionErrorTexts, TestAutoCompactFailedFoldStandsDownForTheRestOfTheExchange, TestCompactOnDemandIgnoresTheStandDownLatch; internal/agent/autocompact_test.go — TestOnDemandCompactIgnoresAutoGate, TestAutoCompactOptOutRespected; internal/agent/emergencyfold_test.go — TestEmergencyFoldFaultSurfacesOnceAndKeepsHistory, TestEmergencyFoldCancelIsQuiet; internal/tui/worker.go — startCompact

**Tests.** The 48 tests across `compact_`, `autocompact_`, `autocompact_guard_`, `emergencyfold_`,
`overflowrecovery_`, `predictiveguard_test.go` plus `setlive_test.go`, `routedspawn_test.go`,
`usagetally_test.go` unchanged; new table test drives `foldFor` per kind and asserts the latch row
and its gate column; `TestRefoldOutcomeMapping`, `TestOnDemandCompactIgnoresAutoGate`,
`TestCompactOnDemandIgnoresTheStandDownLatch`, `TestCompactCancelMidSummaryLeavesConvUntouched` and
`countCompactionErrors`' saturation pin unchanged.

**Acceptance.**
- `go test ./internal/agent -run 'Compact|Fold|Overflow|Predictive|Guard|Usage|SetLive|Routed'`
- `grep -c 'Source: *"compaction"' internal/agent/compact.go` → 1.

**Commit:** `refactor(agent): one fold entry with a latch table for the three triggers`

## 4. The standing system message is one block table — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `standingBlocks` is a function returning the table, not a package-level var, and `standingFences` a once-built list — as a var initializer the table is an initialization cycle (row render `contextBlocks` → `fenceContent` → `forgesStandingStructure` → the table); the render column takes the Agent (`func(*Agent) string`, method expressions) rather than the plan's `func() string`.
NOTES (2026-09-16): `doc.go`'s "Thirty files" count was already stale (33 non-test files before this item, 34 after); left untouched as pre-existing drift outside the item's scope.

**What.** New `internal/agent/standingblocks.go`: an ordered table `standingBlocks` in ADR 0023 §6
wire order — prompt, orientation, delegate report, task list, context files — each row `{render
func() string, fence string, ridesAlong bool}`. `loop.go` `standingSystem` walks it, keeping the
"" contract (the two configured sources empty ⇒ nothing seeded, checked BEFORE any block renders).
`contextfiles.go` `forgesStandingStructure` derives its prefix set from the table's fence column ∪
the two advice-fence prefixes. The wrap-up directive stays on `req.AppendToSystem` in
`buildRequest` (per-request, stands alone; `ContextFilesReport.StandingTokens` unchanged). The
ride-along prose in `orientation.go`, `delegatereport.go`, `tasklistblock.go`, `loop.go` collapses
to one sentence pointing at the table. Twin: `buildRequest` and `loopView` stamp MaxTokens, Depth,
ParallelAgents through one `newProjection(msgs, turn)` helper.

**Regression guard.** `forgesStandingStructure` fences SEVEN prefixes and the context-files row owns TWO of them — `contextFileHeader` AND `contextFileFooter` ("## End of workspace context: ") — so the fence column is `fences []string` (context files = {`contextFileHeader`, `contextFileFooter`}; prompt = none) and the new per-row forgery test iterates every fence of a row, not one; `promptseam_test.go`'s footer forgery (`workspaceTextPrefix + "  " + contextFileFooter + "AGENTS.md"`) stays green.

**Files:** `internal/agent/standingblocks.go`, `internal/agent/standingblocks_test.go`, `internal/agent/loop.go`, `internal/agent/contextfiles.go`, `internal/agent/orientation.go`, `internal/agent/delegatereport.go`, `internal/agent/tasklistblock.go`, `internal/agent/doc.go`
**Read first:** internal/agent/loop.go — buildRequest, standingSystem, loopView, systemPrompt; internal/agent/contextfiles.go — forgesStandingStructure, fenceContent, contextBlocks, contextFileHeader, contextFileFooter, workspaceTextPrefix, ContextFilesReport.StandingTokens; internal/agent/orientation.go — orientationBlock, orientationHeader; internal/agent/delegatereport.go — delegateReportBlock, delegateReportFence; internal/agent/tasklistblock.go — taskListBlock, TaskListFence; internal/domain/hooks.go — AdviceFencePrefix, AdviceFenceClosePrefix, Request.AppendToSystem; internal/agent/promptseam_test.go — contextBlock, seedSystemMessage, TestContextSeam_* (forgery/order); internal/agent/contextfiles_test.go — fenceContent tests

**Tests.** New table-driven forgery test: for every row and every fence of it, a context file
spelling that fence is prefixed and the row renders at its index. The 73 order/ride-along/forgery tests
(`TestContextSeam_*`, `TestOrientation_*`, `TestDelegateReport_*`, `TestTaskListBlock_*`,
`TestFenceContent*`), the 13 wrap-up tests, `fanout_test.go` and `hooksynthesis_test.go` projection
equality unchanged.

**Acceptance.**
- `go test ./internal/agent -run 'ContextSeam|Orientation|DelegateReport|TaskList|Fence|WrapUp|Fanout|Hook'`

**Commit:** `refactor(agent): standing system blocks are one ordered table`

## 5. One skill-block renderer and two composition twins — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `Block` is joined by an exported `ResolvedSkill.Expand` — the "no Dir ⇒ token stays literal" rule needs one spelling, and the loop must expand before `clampRef` measures (the plan's "expansion BEFORE the clamp") while `Block` expands for `load_skill`; `Block` re-expanding a clamped body is a no-op, pinned by the table test.
NOTES (2026-09-16): `MergeSystem` copies the slice on the append branch too (pure as the plan words it); `appendOrCreateSystem` derives its `committedLen` bump from the length change.
NOTES (2026-09-16): consequential edit — docs/adr/0023-the-system-prompt-is-a-configured-template-rendered-per-request.md: made necessary by retiring `injectSystemInstructions` (two dated in-place amendments naming `domain.MergeSystem`).
NOTES (2026-09-16): `load_skill.go`'s doc comment now says "the same <skill> wrapper" so the acceptance grep `grep -n '<skill: ' internal/agent/*.go internal/tools/*.go | grep -v _test` reports no hit outside the domain renderer.

**What.** `internal/domain/config.go` `ResolvedSkill.Block(body string) string` renders the
`<skill: …>` header, the `files:` sentence, the `{{SKILL_DIR}}`-expanded body and `</skill>\n`
(stdlib only — ADR 0010). `internal/agent/loop.go` `resolveSkillRefs` expands, clamps
(`clampRef` — expansion BEFORE the clamp measures, as today), then `Block` + "\n"; the `also
matched` line stays outside the wrapper. `internal/tools/load_skill.go` `renderSkillLookup` calls
`Block(s.Body)`; `agent.skillDirToken` alias goes. Twins: `interject.go` and `step` compose the
user message through one `composeUserMessage(ctx, turn, in, interjected bool)`; `agent/wire.go`
`injectSystemInstructions` and `domain/hooks.go` `appendOrCreateSystem` share a pure
`domain.MergeSystem(msgs []Message, text string) []Message` (the wire copy must not touch
`committedLen`).

**Regression guard.** `injectSystemInstructions` runs on `[]provider.Message` inside `toProviderRequest` AFTER the domain→wire projection, so the merge moves BEFORE the projection loop — `msgs := domain.MergeSystem(st.Messages, block)` on the `State()` copy (`State` copies the slice; `Content` is a string, so the Request is untouched) — then project; `TestPromptSeam_InjectedTextNeverEntersHistoryOrSnapshot` stays the guard. `wire_test.go` holds only the effort/interjected projection tests; the injection pins are `promptseam_test.go`'s three `TestPromptSeam_*` tests named below.

**Files:** `internal/domain/config.go`, `internal/domain/config_test.go`, `internal/domain/hooks.go`, `internal/agent/loop.go`, `internal/agent/interject.go`, `internal/agent/wire.go`, `internal/tools/load_skill.go`
**Read first:** internal/domain/config.go — ResolvedSkill, SkillDirToken; internal/agent/loop.go — resolveSkillRefs, clampRef, refBound, step (skills→refs→text Append), skillDirToken; internal/tools/load_skill.go — renderSkillLookup, LoadSkill.Execute; internal/agent/interject.go — Interject; internal/agent/wire.go — toProviderRequest, injectSystemInstructions; internal/domain/hooks.go — Request.appendOrCreateSystem, AppendToSystem, InjectContext, State, firstIndex; internal/agent/filerefs_test.go — TestInterjectResolvesReferencesUnderTheCallersContext; internal/tui/toolregistry.go — loadedSkillName (`<skill: ` opener reader)

**Tests.** New `domain` table test for `Block`; `minilang_test.go` (skill wording, no-Dir omits the
line), `skillmount_test.go`, `load_skill_test.go` rungs, `TestInterjectResolvesReferencesUnderTheCallersContext`,
`interject_test.go`, `promptseam_test.go`'s `TestPromptSeam_NonNativeInjectsMenuAndSuppressesTools`,
`TestPromptSeam_AppendsToSeededSystemMessage`, `TestPromptSeam_InjectedTextNeverEntersHistoryOrSnapshot`
unchanged; TUI `toolshape_test.go` opener literals unchanged.

**Acceptance.**
- `go test ./internal/domain ./internal/agent ./internal/tools -run 'Skill|Interject|Inject|System|Block'`
- `grep -n '<skill: ' internal/agent/*.go internal/tools/*.go | grep -v _test` → the one renderer.

**Commit:** `refactor(domain): one skill-block renderer; the loop's composition twins share helpers`

## 6. The probe record lives beside its writer; `internal/library` goes — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `TestProbeModelRecordReachesTheResolver` was renamed `TestProbeModelRecordLoadsBack` — the item recasts it as a load-back test and no resolver remains for the old name to be true about; still matched by the acceptance `-run 'Probe|Record|Fingerprint'`.
NOTES (2026-09-16): consequential edit — internal/probe/prompts/README.md: made necessary by retiring `library.ProbeBatteryVersion` (the README named it, with a file:line into the deleted package, as the constant's home).
NOTES (2026-09-16): `internal/library/proberecord.go` and its test were moved with `git mv` (the index records the rename); the deletion of `internal/library/{doc,fingerprint,fingerprint_test}.go` is staged via `git rm` — the FILES list names both the old and new paths so the verifier's stage covers the whole move.
NOTES (2026-09-16): the "beside library/ and sessions/" comment on `TestProbeRecordLivesUnderTheApogeeHome` and ADR 0021 / CONTEXT.md's `internal/library` mentions are left to item 7 (the words), which owns the record's wording; `internal/sanitize`, `internal/recall` historical comments stay per the plan.

**What.** Move `internal/library/proberecord.go` (+ test) into `internal/probe` — JSON keys,
`probeRecordKey` digest, dir name `probe`, file perms byte-identical so every existing
`~/.apogee/probe/<digest>.json` keeps loading; `probe.BatteryVersion` becomes the one const.
Repoint `cmd/apogee/probemodel.go` and `cmd/apogee/wire.go` (`ProbeDir`). Delete
`internal/library/fingerprint.go` (+ test), `doc.go`, the package, and
`domain.FingerprintResolver` (+ its `domain/doc.go` line); zero non-test callers (verified).
`TestProbeModelRecordReachesTheResolver` becomes a test that the record the probe wrote loads back.

**Regression guard.** `internal/probe/doc.go` joins Files: its file map names `proberecord.go` (`TestDocMapNamesEveryFile` in `internal/probe/docmap_test.go` goes red otherwise) and its "persisted by the composition root through internal/library" sentence is rewritten. Historical comments in `internal/sanitize/sanitize.go`, `internal/recall/store.go` and `internal/recall/doc.go` still name the package, so the acceptance check is scoped to import lines.

**Files:** `internal/probe/proberecord.go`, `internal/probe/proberecord_test.go`, `internal/probe/battery.go`, `internal/probe/doc.go`, `internal/library/` (deleted), `internal/domain/fingerprint.go`, `internal/domain/doc.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/probemodel_test.go`, `cmd/apogee/wire.go`
**Read first:** internal/library/proberecord.go — ProbeRecord, SaveProbeRecord, LoadProbeRecord, probeRecordKey, ProbeDir, ProbeBatteryVersion; internal/probe/battery.go — BatteryVersion; cmd/apogee/probemodel.go — recordProbeFingerprint; cmd/apogee/wire.go — resolveRoots, stateRoots; internal/probe/doc.go — file map + internal/probe/docmap_test.go TestDocMapNamesEveryFile; cmd/apogee/probemodel_test.go — TestProbeModelRecordReachesTheResolver, TestProbeRecordLivesUnderTheApogeeHome, TestProbeModelWarnsAboutAnOldFormatRecord; internal/domain/fingerprint.go — FingerprintResolver; internal/domain/doc.go — file map

**Tests.** The five moved `proberecord_test.go` tests; the 17 `probemodel_test.go` tests (record
path/perm assertions) unchanged in outcome.

**Acceptance.**
- `go build ./... && go test ./internal/probe ./internal/domain ./cmd/apogee -run 'Probe|Record|Fingerprint'`
- `test ! -d internal/library && ! grep -rn '"github.com/airiclenz/apogee/internal/library"' --include=*.go .`

**Commit:** `refactor(probe): the probe record lives beside its writer; the unread fingerprint resolver goes`

## 7. The words match: what a probe record buys today

**What.** Depends on item 6. Rewrite every claim that rested on the deleted resolver: the probe
report's `effectLine` "now resolves at medium confidence" and `fingerprintLines` "identity resolves
as it did before" (`internal/probe/model.go`) → the record is a stored signature the next probe
compares against; `docs/manual/probe.md` "rises from low to medium confidence" likewise;
`CONTEXT.md` "identity is resolved offline at startup" and the retired-terms "Library" entry's
"serves … the identity a Model profile … is keyed on" (profiles key on the label string); ADR 0021
§3 gains a dated amendment (the middle rung had one reader, deleted by ADR 0076 A9; persistence
stays for drift detection); ADR 0071's "serve Validated sets and `probe model`" gets a trailing
note. Rule for the rest: every comment naming the identity ladder or its middle rung — `grep -rn
'identity ladder\|medium confidence\|low to medium\|FingerprintResolver' --include=*.go --include=*.md
.` — is rewritten or deleted.

**Regression guard.** `middle rung` also names the roster, effort and mode ladders (`internal/config/config.go`, `internal/config/options.go`, `internal/provider/wire.go`, `internal/domain/config.go`, `internal/daemon/file.go`, `internal/tui/picker.go`, `internal/agent/resolution_test.go`, `docs/design/confinement-execution-contract.md`) and this plan file, so the rule and grep narrow to `'identity ladder\|medium confidence\|low to medium\|FingerprintResolver'` and the excluded dirs gain `docs/plans/` and `docs/design/`. Two announced surfaces join Files: `cmd/apogee/probemodel.go`'s `probe model` Long text ("recorded under the apogee home at medium confidence", "only the confidence rises, from low to medium", "Library observations keyed on it keep matching") and `internal/domain/config.go`'s `ConfigDir` doc ("the probe records the identity ladder reads live under it").

**Files:** `internal/probe/model.go`, `internal/probe/model_test.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/probemodel_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_settings.go`, `internal/domain/config.go`, `docs/manual/probe.md`, `CONTEXT.md`, `docs/adr/0021-*.md`, `docs/adr/0071-*.md`
**Read first:** internal/probe/model.go — effectLine, fingerprintLines, SaveOutcome; internal/probe/model_test.go — effect wording pins; cmd/apogee/probemodel.go — newProbeModelCmd Long text; cmd/apogee/probemodel_test.go — TestProbeModelRunsTheBatteryAndRecords, TestProbeModelNoSaveNamesTheSurvivingRecord; docs/manual/probe.md — "rises from low to medium"; CONTEXT.md — "Probing and model identity", retired-terms "Library"; docs/adr/0021-*.md §3; docs/adr/0071-*.md — "serve Validated sets and `probe model`"

**Tests.** `internal/probe/model_test.go` and `cmd/apogee/probemodel_test.go` effect/previous-record
wording assertions updated to the new sentences.

**Acceptance.**
- `go test ./internal/probe ./cmd/apogee -run 'Probe|Effect|Fingerprint'`
- the grep above → no match outside `docs/adr/`, `docs/design/`, `docs/plans/`, `docs/reviews/`, `CHANGELOG.md`.

**Commit:** `docs(probe): the report, manual, CONTEXT and ADR 0021 say what a probe record buys today`

## 8. `domain.Usage` — the five counters get a name

**What.** New `internal/domain/usage.go`: `Usage{Calls, PromptTokens, CachedPromptTokens,
CompletionTokens, TotalTokens int}` with `Sum(...Usage) Usage` and `Adopt(reading
Usage)` (latest wins when `reading.Calls > 0`). `internal/run/run.go`: `type Usage = domain.Usage`;
`SubAgentUsage` embeds it; `sessionUsage` and `delegateTotals` become struct conversions + `Sum`
(compile-checked order — never positional literals across types); the tap's `Calls>0 ⇒ latest
wins` clause becomes `Adopt`; the fill/Maintenance halves stay per Driver. `cmd/apogee/headless.go`'s
three re-packs become conversions. `session.Usage`, `eventjson.Usage`/`SubAgentUsage`/`usageData`
keep their shapes and keys (ADR 0075 D4/D10; byte-compared goldens).

**Regression guard.** `domain.Usage` is declared with `session.Usage`'s exact field NAMES and ORDER (so the run and TUI conversions to session.Usage are struct conversions that compile), and `eventjson`'s `usageFrame` STAYS a field-by-field mapping (its golden-pinned key order differs) — strike "declared in eventjson's field order"; every new or moved file in items 6 and 8 gets its `doc.go` line (each touched package carries a docmap_test.go). This supersedes `internal/run/run.go` `sessionUsage`'s "run.Usage and session.Usage carry the same five counters in a DIFFERENT field order, so the conversion is written out field by field on purpose" note, which the alias makes false; the `*Tokens` names keep `cmd/apogee/schedule.go`'s `firingSpend` and the `run.Usage{TotalTokens:}` literals in `wire_firing_test.go`/`e2e_usage_test.go` compiling untouched.

**Files:** `internal/domain/usage.go`, `internal/domain/usage_test.go`, `internal/domain/doc.go`, `internal/run/run.go`, `internal/run/run_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire_test.go`, `cmd/apogee/schedule_test.go`
**Read first:** internal/run/run.go — Usage, SubAgentUsage, sessionUsage, delegateTotals, eventTap.noteUsage, eventTap.totals; internal/session/store.go — Usage; internal/eventjson/writer.go — Usage, SubAgentUsage; cmd/apogee/headless.go — usageFrame, subAgentFrames, headlessUsageLines, headlessUsageLine; cmd/apogee/schedule.go — firingSpend; internal/run/run_test.go — TestEventTapCountsMaintenanceInTheTotalsOnly, usageWithTotals; internal/domain/doc.go — file map + internal/domain/docmap_test.go; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden

**Tests.** `TestEventTapCountsMaintenanceInTheTotalsOnly`, `TestE2EEventLinesGolden`
(`cmd/apogee/testdata/eventlines/run.jsonl` byte-identical), `eventjson/writer_test.go`,
`encode_test.go`, `wire_session_test.go`'s `Usage{}` literals (rewritten to keyed fields); new
`domain` tests for `Sum` and `Adopt` (a zero-Calls reading never overwrites).

**Acceptance.**
- `go test ./internal/domain ./internal/run ./internal/eventjson ./cmd/apogee -run 'Usage|Tap|EventLines|Headless|Session'`
- `git diff --exit-code -- cmd/apogee/testdata/eventlines/`

**Commit:** `refactor(domain): one Usage value with the latest-wins fold as a method`

## 9. The TUI counts through `domain.Usage`

**What.** Depends on item 8. `internal/tui`: `usageTotals` → `domain.Usage`; `usageSum` → `Sum`,
`usageReading`'s latest-wins clause → `Adopt` (the gauge + `usageBase` offset stay the TUI's
reading); `sessionsave.go` and `sessions.go` convert to `session.Usage` as they do today; the
delegate sum in `usage.go` uses `Sum`. `fold_test.go`'s latest-reading test stays (it asserts the
TUI's reading, not the fold rule).

**Regression guard.** Same field-name/order rule as 8: `domain.Usage` carries `session.Usage`'s names and order, so `session.Usage(m.usage)` (`sessionsave.go`) and `usageTotals(msg.rec.Meta.Usage)`'s successor in `sessions.go` stay struct conversions — `internal/tui/fold.go`'s "field set matches session.Usage exactly" note holds and the item yields to it. `internal/tui/model.go` (`Model.usage`/`usageBase`/`delegateUsage`, `replayResumed`) and `internal/tui/transcript.go` (entry `usage`, `applyUsage`) carry `usageTotals` and join Files, as do `transcript_test.go` and `transcriptbridge_test.go` (`usageTotals{...}` literals); `internal/tui/e2e_usage_test.go` does not exist — the usage e2e is `cmd/apogee/e2e_usage_test.go`.

**Files:** `internal/tui/fold.go`, `internal/tui/usage.go`, `internal/tui/model.go`, `internal/tui/transcript.go`, `internal/tui/sessionsave.go`, `internal/tui/sessions.go`, `internal/tui/commandrun.go`, `internal/tui/transcriptbridge.go`, `internal/tui/fold_test.go`, `internal/tui/usage_test.go`, `internal/tui/sessions_test.go`, `internal/tui/transcript_test.go`, `internal/tui/transcriptbridge_test.go`, `cmd/apogee/e2e_usage_test.go`
**Read first:** internal/tui/fold.go — usageTotals, usageReading, Model.foldStats; internal/tui/usage.go — usageSum, Model.delegateUsageTotal, usageRow; internal/tui/model.go — Model.usage, Model.usageBase, Model.delegateUsage, Model.replayResumed; internal/tui/transcript.go — entry.usage, transcript.applyUsage; internal/tui/sessionsave.go — sessionSnapshot usage/delegateUsage; internal/tui/sessions.go — reopen usageBase; internal/tui/transcriptbridge.go — wire→entry usage; internal/tui/fold_test.go — TestFoldStatsTracksTheMainAgentsCumulativeTotals

**Tests.** `TestFoldStatsTracksTheMainAgentsCumulativeTotals`, `cmd/apogee/e2e_usage_test.go`, the session
codec goldens (`TestTranscriptCodecGoldenV1`) unchanged.

**Acceptance.**
- `go test ./internal/tui ./cmd/apogee -run 'Usage|Fold|Session|Codec|E2E'`
- `grep -n 'usageTotals' internal/tui/*.go` → no match.

**Commit:** `refactor(tui): usage counters are domain.Usage`

## 10. `UsageEvent` carries one cumulative `domain.Usage`

**What.** Depends on items 8–9. `internal/domain/events.go` `UsageEvent`: the five cumulative ints
become one embedded `Cumulative domain.Usage` (the per-call half has no `Calls`, so it stays as
loose ints); `internal/agent/agent.go` fills it; `internal/eventjson/encode.go`, `internal/run/run.go`
and `internal/tui/fold.go` read it — JSON keys and the eventlines golden unchanged. Test literals
across the seven test files follow (keyed fields).

**Regression guard.** Three of the seven test files were unlisted: `internal/agent/minilang_test.go` and `internal/agent/restoresession_test.go` read `.CumulativeCalls`/`.CumulativeTotalTokens`, and `internal/tui/transcript_test.go` sets keyed `Cumulative*` literals — all three join Files so the `internal/agent` and `internal/tui` test packages keep compiling.

**Files:** `internal/domain/events.go`, `internal/agent/agent.go`, `internal/eventjson/encode.go`, `internal/run/run.go`, `internal/tui/fold.go`, `internal/agent/usagetally_test.go`, `internal/agent/minilang_test.go`, `internal/agent/restoresession_test.go`, `internal/eventjson/encode_test.go`, `internal/run/run_test.go`, `internal/tui/fold_test.go`, `internal/tui/transcript_test.go`
**Read first:** internal/domain/events.go — UsageEvent; internal/agent/agent.go — usageTally, usageTally.record; internal/eventjson/encode.go — usageData, encode UsageEvent case; internal/run/run.go — eventTap.noteUsage; internal/tui/fold.go — usageReading; internal/agent/usagetally_test.go + minilang_test.go + restoresession_test.go — usageEvents; internal/tui/transcript_test.go — Cumulative* literals; cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden

**Tests.** `TestE2EEventLinesGolden` byte-identical; `usagetally_test.go`, `encode_test.go`,
`run_test.go`, `fold_test.go` unchanged in outcome.

**Acceptance.**
- `go build ./... && go test ./internal/domain ./internal/agent ./internal/eventjson ./internal/run ./internal/tui ./cmd/apogee -run 'Usage|EventLines|Encode'`
- `git diff --exit-code -- cmd/apogee/testdata/eventlines/`

**Commit:** `refactor(domain): UsageEvent carries one cumulative Usage`

## 11. One upstream fault classifier

**What.** New `internal/provider/fault.go`: `classify(status int, errType, body, msg string,
effort bool) fault{overflow bool, code string, text string, retryable bool, hinted bool}` owning the
three rules the four copies keep by prose — 400-overflow detection + body sanitising (`maxErrorBodyBytes`
cap), the effort hint (never on overflow, never inside `StatusError.Body`), retryable. `client.go`
`statusError`/`inBandError` and `stream.go` `statusDelta`/`inBandErrorDelta` become renderers over
one `fault` — texts byte-identical (`*StatusError` wrapping, `upstreamStatusText`, the raw SSE
payload), `errors.As(*StatusError)` / `errors.Is(ErrContextOverflow)` chains intact. Bead
apogee-6fp's second dialect later adds one classifier arm.

**Regression guard.** `classify` is pure and key-less: it sniffs `body`/`msg` UNSANITISED (`sanitize` needs `c.apiKey` and truncates at `maxErrorLength` = 500, while the overflow marker is read off the raw 64 KiB body today); the `maxErrorBodyBytes` read cap and `c.sanitize`/`c.observeWire` stay in the four renderers. The classifier's `retryable` is read by `inBandErrorDelta` only — `statusDelta`'s Delta keeps `Retryable` false (`send` already retried that status; `stream.go`'s Retryable doc, "the class the client WOULD have retried had it arrived as an HTTP status", stands and the item yields to it) — and that case joins the new `fault_test` table.

**Files:** `internal/provider/fault.go`, `internal/provider/fault_test.go`, `internal/provider/client.go`, `internal/provider/stream.go`
**Read first:** internal/provider/client.go — statusError, inBandError, upstreamStatusText, sanitize, isContextOverflow, isRetryableStatus, thinkingEffortHint; internal/provider/stream.go — statusDelta, inBandErrorDelta, providerUnavailable, Delta.Retryable; internal/provider/wirejson.go — wireError.intCode, wireError.render; internal/provider/stream_test.go — TestStream_ThinkingEffortHint, TestStream_ErrorStatus, TestStream_MidStreamEOFIsRetryable; internal/provider/client_test.go — TestRespond_ThinkingEffortHint; internal/agent/loop.go — holdOffRestream, turnRun.retryable

**Tests.** `client_test.go` (11), `stream_test.go` (11), `reliability_test.go` (4) unchanged; new
table test over `classify`, including the case that a 5xx/429 arriving as a status renders with
`Retryable` false.

**Acceptance.**
- `go test ./internal/provider`

**Commit:** `refactor(provider): one classifier behind the four upstream-fault renderers`

## 12. stubllm plays in-process: the engine's pure fakes collapse onto one Script

**What.** `internal/stubllm/server.go` exports `Handler() http.Handler` and `Transport()
http.RoundTripper` (in-process, no listener; stubllm stays apogee-free). In `internal/agent` test
code: `scriptResponder(t, stubllm.Script) provider.Responder` = `provider.NewClient(…,
WithHTTPClient(&http.Client{Transport: s.Transport()}))` — parity with the HTTP path by
construction (one decoder). Migrate the pure fakes: `echoResponder`, `recordingResponder`,
`usageResponder`, `profileResponder`, `chunkedResponder`, `cappedSummaryResponder`,
`capturingResponder`, `scriptedResponder`, `captureAllResponder`, `overflowResponder` (HTTP 400 +
overflow body), `faultResponder` (in-band error) — delete each type when its last user moves.
Request-level assertions move to the Script's request log. The ten fakes in Out of scope stay.

**Regression guard.** Decided once here and inherited by 13–15 — `internal/stubllm` gains (a) a Turn that carries narration AND a tool call (framed head/tail as a real server does) and (b) an explicit `chunks:` list for hand-placed stream boundaries (the streamsuppress/chunked cases); a fake whose behaviour neither expresses stays a fake and is named in NOTES. Rule for the migration: every `internal/agent/*_test.go` naming one of the eleven fakes (`grep -l 'echoResponder\|scriptedResponder\|…'` — `echoResponder` and `scriptedResponder` each appear in ~40 files) is in scope; a type whose last user lies outside this item's Files survives it and is deleted by the item that moves that user, and the survivors are recorded in NOTES. The in-process constructor is listener-less (e.g. `stubllm.Load(t, script, opts...) *Server` — `New`/`Serve` listen from construction and `newServer` is unexported); `Transport()` is io.Pipe-based with a Flush-capable writer, recovering `kill()`'s `http.ErrAbortHandler` panic into a pipe `CloseWithError` (the client reads unexpected EOF); `scriptResponder` passes `WithMaxRetries(0)` so an unanticipated-request 500 or an `http: 500` turn is not retried 3× with backoff.

**Files:** `internal/stubllm/server.go`, `internal/stubllm/server_test.go`, `internal/stubllm/script.go`, `internal/stubllm/script_test.go`, `internal/agent/harness_test.go`, `internal/agent/statemachine_test.go`, `internal/agent/overflow_test.go`, `internal/agent/streamsuppress_test.go`, `internal/agent/usagetally_test.go`, `internal/agent/profile_test.go`, `internal/agent/compact_test.go`
**Read first:** internal/stubllm/server.go — New, Serve, newServer, handler, handleChat, take, reply, writeStream, kill, streamDeltas; internal/stubllm/script.go — Turn, Turn.validate, kindCount, Script.Validate, ToolCall.callID; internal/agent/harness_test.go — echoResponder, recordingResponder, streamReply, baseConfig, stepOnce; internal/agent/statemachine_test.go — scriptedResponder, toolCallScript, contentScript; internal/agent/retryexchange_test.go — captureAllResponder; internal/agent/profile_test.go — profileResponder, newProfileAgent, TestProfile_NativeCallWinsOverText; internal/agent/streamsuppress_test.go — chunkedResponder, TestStream_NativeIsByteIdentical; internal/provider/client.go — WithHTTPClient, WithMaxRetries, NewClient

**Tests.** Every migrated test keeps its assertion; new `stubllm` tests: one Script yields the same
bytes through `Handler()` and a listening server; a narration+tool-call Turn frames head/tail; a
`chunks:` list reaches the client at its hand-placed boundaries; the in-process transport surfaces
`kill()` as an unexpected EOF.

**Acceptance.**
- `go test ./internal/stubllm ./internal/agent`
- `grep -c 'Stream(.*provider.Request) iter.Seq\[provider.Delta\]' internal/agent/*_test.go` lower than at HEAD~1 (record before/after in NOTES).

**Commit:** `test(agent): the engine's pure fakes play a stubllm Script through an in-process transport`

## 13. The request log records sampling and effort; the compaction fakes migrate

**What.** Depends on item 12. `internal/stubllm/log.go` `Request` gains `Sampling` (max tokens,
temperature) and the effort/reasoning keys the dialects send, recorded verbatim from the body.
Migrate `summaryEffortResponder`, `scriptedCompactResponder`, `recoveryResponder`, `compactSpyResponder`
(selected by `when: system:`), and `maxTokRecordingResponder` now that the log carries
`Sampling.MaxTokens`; their EffortOff / kwargs-dialect assertions read the log.

**Regression guard.** Inherits item 12's decision (narration+tool-call Turn, `chunks:`). The body decode lives in `internal/stubllm/wire.go` (`chatRequest`) and the log entry is built in `server.go` `take` — both join Files so max_tokens/temperature/chat_template_kwargs/reasoning/reasoning_effort reach `Request`. The fake is `compactSpyResponder`, not `compactSpy`. `TestCompactSummarizerKeepsTheResolvedEffortOnAnUndialledServer`'s `EffortDialect == EffortDialectNone` assertion (effort "") has no wire observable — `applyEffort` emits nothing for that pair and for `EffortDialectOff` alike — so it is recast as "no effort key on the wire" (and NOTES says so), or `summaryEffortResponder` is kept for that one test.

**Files:** `internal/stubllm/log.go`, `internal/stubllm/log_test.go`, `internal/stubllm/wire.go`, `internal/stubllm/server.go`, `internal/agent/compact_test.go`, `internal/agent/autocompact_test.go`, `internal/agent/emergencyfold_test.go`, `internal/agent/effort_test.go`
**Read first:** internal/stubllm/wire.go — chatRequest, chatRequest.messages; internal/stubllm/server.go — take, record; internal/stubllm/log.go — Request, Requests, LastMessage; internal/provider/client.go — applyEffort, buildBody, Sampling; internal/agent/compact_test.go — summaryEffortResponder, foldOnce, maxTokRecordingResponder, TestCompactCappedSummaryFaultNamesTheAppliedCap; internal/agent/autocompact_test.go — compactSpyResponder, autoCompactConfig; internal/agent/overflowrecovery_test.go — recoveryResponder, isSummaryRequest; internal/agent/autocompact_guard_test.go — scriptedCompactResponder

**Tests.** The compaction suite unchanged in outcome; new `stubllm` log test for the two fields.

**Acceptance.**
- `go test ./internal/stubllm ./internal/agent -run 'Compact|Fold|Effort|Log'`

**Commit:** `test(agent): compaction fakes read effort and sampling from the stubllm request log`

## 14. `internal/agent`'s four listening upstreams script stubllm

**What.** Depends on items 12–13. `apikey_test.go`, `construct_test.go`, `harness_test.go`,
`routedspawn_test.go`: every `httptest.NewServer` + hand-rolled `text/event-stream` handler that
plays an LLM becomes `stubllm.New(t, script)` (`WithAPIKey` for the key tests). Fixtures that test
something other than an upstream (`authRecorder`, `wireUpstream` — see the guard) stay.

**Regression guard.** Inherits item 12's decision, and depends on item 13 too (order 13 before 14): `TestRoutedChildSummarizerSpeaksTheTargetsDialectOnTheWire` asserts `chat_template_kwargs.enable_thinking == false` and `reasoning` absent off the raw body, and those keys reach the log only through item 13. `harness_test.go` has no `writeFinal`/`writeToolCall` pair (those live in `internal/run/harness_test.go` and `internal/tui/e2e_test.go` — items 15 and 16); its only listener is `TestHarness_RealProviderWirePath`'s inline closure. Two fixtures test more than an upstream: `apikey_test.go`'s `authRecorder` asserts the Authorization header is ABSENT for an empty key (stubllm without `WithAPIKey` accepts any header and logs none), and `construct_test.go`'s `wireUpstream` compares the WireEvent payload byte-for-byte with the posted body (the log holds a decoded Request, not bytes) — wrap item 12's `Handler()` in a header/body-recording middleware under `httptest.NewServer` for those two fixtures (deleting their hand-rolled SSE), or add `Authorization`/`Body` to the stubllm `Request` log.

**Files:** `internal/agent/apikey_test.go`, `internal/agent/construct_test.go`, `internal/agent/harness_test.go`, `internal/agent/routedspawn_test.go`
**Read first:** internal/agent/apikey_test.go — authRecorder, newAuthRecorder, TestNewWithoutAPIKeySendsNoAuthHeader; internal/agent/construct_test.go — wireUpstream, newWireUpstream, TestInspectorArmsWireEventsThroughTheSink, wireEvents; internal/agent/harness_test.go — TestHarness_RealProviderWirePath, baseConfig, stepOnce; internal/agent/routedspawn_test.go — TestRoutedChildSummarizerSpeaksTheTargetsDialectOnTheWire, routingParent, routedTarget, seedFoldable; internal/stubllm/server.go — WithAPIKey, authorized, New

**Tests.** Unchanged in outcome.

**Acceptance.**
- `go test ./internal/agent`
- `grep -ln 'text/event-stream' internal/agent/*_test.go` → no match.

**Commit:** `test(agent): the package's listening upstreams script stubllm`

## 15. `internal/run`'s harness scripts stubllm

**What.** Depends on item 12. `internal/run/harness_test.go` `newUpstream` + its
`writeFinal`/`writeToolCall` pair go; the 34 `newUpstream` sites in `run_test.go` take a
`stubllm.Script`; `lastRoleIs(tool)` / `lastTextHas` predicates become `tool_result:` /
`last_message:` matchers. Split the migration across two dispatches (checkpoint at the midpoint)
if the diff passes ~400 lines — one commit.

**Regression guard.** Inherits item 12's decision: `writeToolCallWithText` (`run/harness_test.go`, used by the `run_test.go` site that narrates then calls) scripts as item 12's narration+tool-call Turn — until 12 lands it cannot be scripted (`Turn.validate`'s `kindCount` refuses text + tool_calls). Should that one site stay a hand-rolled reply instead, it is listed as a kept fixture and the `text/event-stream` acceptance grep exempts it by name.

**Files:** `internal/run/harness_test.go`, `internal/run/run_test.go`
**Read first:** internal/run/harness_test.go — newUpstream, upstream, request.lastRoleIs, request.lastTextHas, request.offers, decodeRequest, writeFinal, writeToolCall, writeToolCallWithText, writeUsage, alwaysFinal; internal/run/run_test.go — delegatingSession (already a stubllm.Script), planSpec, Once; internal/stubllm/script.go — Match.LastMessage, Match.ToolResult, Turn.Repeat, Turn.Usage; internal/stubllm/log.go — Requests, lastToolResultName

**Tests.** `internal/run` unchanged in outcome (strictness: a request the Script did not
anticipate is a 500 — fix the Script, never loosen it).

**Acceptance.**
- `go test ./internal/run`
- `grep -ln 'text/event-stream' internal/run/*_test.go` → no match.

**Commit:** `test(run): the runner harness scripts stubllm`

## 16. The TUI, confinement and schedule e2e files script stubllm

**What.** Depends on item 12. `internal/tui/e2e_test.go` (its `writeFinal`/`writeToolCall` pair
and one-fragment framing go), `cmd/apogee/confinement_e2e_test.go`, `cmd/apogee/schedule_test.go`:
hand-rolled SSE → `stubllm.New`. **Ratified:** should any `cmd/apogee` `TestE2E*` frame golden
differ, and only by the head/tail framing, it is regenerated after a reviewed diff and listed in
NOTES; any other diff is a migration defect.

**Regression guard.** No item-16 file takes a golden, so the golden sentence becomes conditional — "should any `cmd/apogee` TestE2E* frame golden differ, and only by the head/tail framing, it is regenerated after a reviewed diff and listed in NOTES; any other diff is a migration defect" — and every "-popup-design" reference in the plan goes (the gate no longer exists; pop-up frames are held goldens). Inherits item 12's decision. `TestE2EColdStartHeartbeat` needs a server that is DOWN (404 everything) then serves `/v1/models` with `context_length: 32768` and asserts `m.opts.ContextWindow == coldStartWindow`; stubllm's `modelEntry` carries only id/object and `New` listens from construction, so that fixture is exempt by name — keep its handler (its `writeFinal` chat branch included) and the `internal/tui` `text/event-stream` acceptance grep reads → 1, naming it — or it becomes a thin front (404 while down, `/v1/models` with context_length) that reverse-proxies `/v1/chat/completions` to a `stubllm.New` server and reads `wireModel` off `stub.Requests()[n].Model`. `internal/tui/testdata/` does not exist and none of the three files calls `tuitest.Golden`; the frame goldens live in `cmd/apogee/testdata/frames/` under tests that already script stubllm.

**Files:** `internal/tui/e2e_test.go`, `cmd/apogee/confinement_e2e_test.go`, `cmd/apogee/schedule_test.go`
**Read first:** internal/tui/e2e_test.go — scriptedModel, writeFinal, requestModel, TestE2EColdStartHeartbeat, newE2EEngine; cmd/apogee/confinement_e2e_test.go — scriptedTerminalModel, e2eWriteFinal, lastMessageIsToolResult, TestE2EAutoDegradationJourneyOnAnIncapableHost; cmd/apogee/schedule_test.go — firingUpstream, TestAFailedFiringStillCarriesWhatItSalvaged; cmd/apogee/e2e_stream_test.go — loadScript; internal/stubllm/script.go — Script, Turn, Match; internal/stubllm/log.go — Requests, lastToolResultName; internal/provider/discovery.go — modelsResponse

**Tests.** `TestE2E*` (tui), `TestE2EPopupFrames*` as at HEAD, the confinement and schedule e2e
suites unchanged in outcome.

**Acceptance.**
- `go test ./internal/tui ./cmd/apogee -run 'E2E|Confinement|Schedule'`
- `grep -ln 'text/event-stream' internal/tui/*_test.go cmd/apogee/confinement_e2e_test.go cmd/apogee/schedule_test.go` → no match outside `TestE2EColdStartHeartbeat`'s handler (`internal/tui/e2e_test.go` → 1 when that fixture is kept).
- `git diff --stat HEAD~1 -- cmd/apogee/testdata/frames/ | wc -l` → 0, or exactly the files a dated NOTES line lists.

**Commit:** `test(tui): the e2e upstreams script stubllm; framing goldens regenerated`

## 17. The bench-readiness test scripts stubllm

**What.** Depends on item 12. Root-package `benchreadiness_test.go`: its `writeFinal`/`writeToolCall`
pair and request-tail branching become a `stubllm.Script` with `when:` selectors. This closes ADR
0062 D2's debt: no file outside `internal/provider` and `internal/stubllm` writes
`text/event-stream`.

**Regression guard.** `benchModel`'s `RoleTool` branch replies `"completed: "+lastUser`, echoing the last USER message on a request whose last message is the tool result; a stubllm `Capture` reads only `system` or `last_message`, so a Script cannot reproduce that echo — and nothing asserts it (only the fork tokens are, and those ride a last USER message). Script the tool-result turn (`when: tool_result: list_dir`, repeat) with static text; carry the fork echo on `when: last_message: PLEASE_CLOSE` + a `from: last_message` capture of the token into `completed: {{token}}`; the ordered repeat turn stays the `list_dir` call. The file's header counts "the two internal imports that remain — internal/session and internal/tools"; importing `internal/stubllm` makes it three, so that sentence and the ADR 0010 remark beside it are recounted in the same commit (stubllm is not the root module path either).

**Files:** `benchreadiness_test.go`
**Read first:** benchreadiness_test.go — benchModel, requestTail, writeFinal, resumeFork, TestBenchReadinessContract, paddedRegistry; internal/stubllm/script.go — Turn, Match, Capture; internal/stubllm/match.go — matcher.next, Turn.expand; internal/stubllm/log.go — lastToolResultName

**Tests.** Unchanged in outcome.

**Acceptance.**
- `go test . -run 'Bench'`
- `grep -rln 'text/event-stream' --include=*_test.go . | grep -v 'internal/provider\|internal/stubllm'` → no match.

**Commit:** `test: the bench-readiness upstream scripts stubllm`

## 18. `writeTarget` — the write side's scope value; `write_file` and `edit_existing_file` adopt it

**What.** New `internal/tools/write_target.go`: `writeScope` (root, permit, journal) with
`target(args) (writeTarget, error)` — asked once per call for the argument the marker names — and
`writeTarget` methods `read()` (one fenced read), `stat()`, `perm()`, `refuseVirtual()`,
`notFound(prefix)` (siblings via `notFoundMessage` — the one renderer; a refusal never gains
suggestions), `note()` (" → resolves to", computed BEFORE the write), `write(data, perm)` and
`journaled(...)` — `readWriteTarget`, `statWriteTarget`, `currentPerm`, `escapeTargetPin` become
its methods (the permit pin stays on the disclosed Real — ADR 0049). Capture stays in
`path_safety.go` this item (`TestUndoCaptureHasExactlyTwoCallers` keeps passing — item 21 retires
it). Migrate `write_file.go` (keep the "target is a directory" pre-flight and the read-fails-⇒-empty
rule) and `file_edit.go`; `workspace_scoped.go`'s `pathArgWriteTarget` answers off the value.
Ride-along: `python_exec.go`'s private `resolveWorkdir` → `resolveWorkdirInRoot`.

**Regression guard.** The acceptance `-run` regex gains `ApprovedEscape` (`TestApprovedEscape*` drives every verb through a permit and the ADR 0049 pin moves in this item). `writeTarget` already exists — the Named/Real struct in `workspace_scoped.go`, the return type of every `workspaceWriteTarget` implementer and what `writeTargetOf`/`WorkspaceWriteTarget`/`ResolvedWriteTarget` read — and `docs/design/confinement-execution-contract.md` §3.2 fixes it as the marker's return value, so the item yields to the contract: the value IS that struct enlarged (Named/Real stay exported, `workspaceWriteTarget(call) (writeTarget, bool)` untouched) and `resolveTargetUnbounded` stays the marker's root-only path. `readWriteTarget`, `statWriteTarget`, `currentPerm`, `escapeTargetPin` stay as one-line shims over the methods until item 20 retires them (`find_replace.go`, `delete_file.go`, `file_ops.go` still call the free functions). The bite check greps what the migration removes (`resolveTargetUnbounded` is already 0 in both files at BASE). `doc.go`'s map names `write_target.go` in THIS item (`TestDocMapNamesEveryFile` reads every non-test file).

**Files:** `internal/tools/write_target.go`, `internal/tools/write_target_test.go`, `internal/tools/write_file.go`, `internal/tools/file_edit.go`, `internal/tools/path_safety.go`, `internal/tools/workspace_scoped.go`, `internal/tools/python_exec.go`, `internal/tools/doc.go`
**Read first:** internal/tools/workspace_scoped.go — writeTarget, workspaceScopedWriter, pathArgWriteTarget, resolveTargetUnbounded, resolvedTargetNote; internal/tools/path_safety.go — safeWriteFile, readWriteTarget, statWriteTarget, escapeTargetPin, capturePreImage, currentPerm, journalTarget; internal/tools/write_file.go — WriteFile.Execute; internal/tools/file_edit.go — EditExistingFile.Execute; internal/tools/path_suggest.go — notFoundOrRefusal, notFoundMessage; internal/tools/path_virtual.go — refuseVirtualWrite; internal/tools/undo_journal_test.go — TestUndoCaptureHasExactlyTwoCallers, undoCaptureSites; internal/tools/write_permit_test.go — escapeCases, TestApprovedEscapeLandsOnTheDisclosedTarget

**Tests.** `write_file_test.go` (13), `file_edit_test.go` (14), `write_permit_test.go` (9),
`workspace_scoped_test.go` (`TestWriteTargetProbesCoverEveryWriter`, `TestWriteTargetsAgreeOnPath`),
`undo_journal_test.go` (19), `path_suggest_test.go`, `python_exec_test.go` unchanged; TUI fixtures
quoting "wrote 31 bytes to main.go" unchanged; new `write_target_test.go` for each method.

**Acceptance.**
- `go test ./internal/tools -run 'WriteFile|Edit|Permit|WriteTarget|Undo|Suggest|Python|DocMap|ApprovedEscape'`
- `grep -c 'readWriteTarget(ctx\|statWriteTarget(ctx\|safeWriteFile(ctx' internal/tools/write_file.go internal/tools/file_edit.go` → 0.

**Commit:** `refactor(tools): a write-target value mirrors readScope; write_file and edit adopt it`

## 19. The find/replace pair and `delete_file` adopt `writeTarget`

**What.** Depends on item 18. `find_replace.go` (both tools) and `delete_file.go` (its
`checkDeletePath` folds into `target()`) take their target from the value; each Execute reads the
file ONCE through it. Wordings of every not-found prefix ("file not found: ", "directory not found: ",
"path not found: ") and refusal byte-identical.

**Regression guard.** The acceptance `-run` regex gains `ApprovedEscape` (`TestApprovedEscape*` drives every verb through a permit and the ADR 0049 pin moves in this item).

**Files:** `internal/tools/find_replace.go`, `internal/tools/delete_file.go`, `internal/tools/path_safety.go`, `internal/tools/find_replace_test.go`, `internal/tools/delete_file_test.go`
**Read first:** internal/tools/find_replace.go — SingleFindReplace.Execute, MultiFindReplace.Execute; internal/tools/delete_file.go — DeleteFile.Execute, checkDeletePath; internal/tools/path_safety.go — journaledMutation, mutationPath, statWriteTarget, readWriteTarget; internal/tools/path_suggest.go — notFoundOrRefusal; internal/tools/write_permit_test.go — escapeCases; internal/tools/undo_journal_test.go — TestWriteFunnelJournalsEveryContentVerb, TestDeleteFileJournalsThePreImage; internal/tools/delete_file_test.go — TestDeleteFile_RefusesADirectory, TestDeleteFile_DisclosesTheResolvedTarget

**Tests.** `find_replace_test.go` (23), `delete_file_test.go` (10), `undo_journal_test.go`,
`workspace_scoped_test.go` unchanged.

**Acceptance.**
- `go test ./internal/tools -run 'FindReplace|Replace|Delete|Undo|WriteTarget|ApprovedEscape'`

**Commit:** `refactor(tools): find/replace and delete_file take their target from writeTarget`

## 20. `copy_file` and `move_file` adopt `writeTarget` for the destination

**What.** Depends on item 18. `file_ops.go`: the destination(s) come from the value —
`copy_file`'s directory form yields N destination targets and `move_file` its two paths — through the
value's multi-path form, which is `journaledMutation` (`mutationPath` kept). `copy_file`'s SOURCE
stays on `readScope` (the one sanctioned crossing). `destinationArgWriteTarget` answers off the value.
Capture order for move (pre-image before the body), pre-image mode bits and `journalTarget`'s identity
rule (Named path; Real only under a permit) unchanged.

**Regression guard.** The acceptance `-run` regex gains `ApprovedEscape` (`TestApprovedEscape*` drives every verb through a permit and the ADR 0049 pin moves in this item). Roots, reworded: `copy_file`'s SOURCE is on `readScope` (`sourceRoot` from `readScope.locate` vs `t.root` — the two-root end); `move_file`'s two PATHS both sit under `t.root` with no `readScope` (`checkFileOpsPaths` equal roots, `SafeRemove(t.root, …)` unpermitted); the value's multi-path form takes `mutationPath{input, root, post}` unchanged, so `TestMoveSourceStaysInWorkspaceUnderAPermit` reads the same.

**Files:** `internal/tools/file_ops.go`, `internal/tools/path_safety.go`, `internal/tools/workspace_scoped.go`, `internal/tools/file_ops_test.go`, `internal/tools/write_permit_test.go`
**Read first:** internal/tools/file_ops.go — CopyFile.Execute, copyDirectory, copyFromMount, MoveFile.move, checkFileOpsPathsFrom, checkFileOpsDestination; internal/tools/workspace_scoped.go — destinationArgWriteTarget; internal/tools/path_safety.go — journaledMutation, journalTarget, statWriteTarget; internal/tools/path_read.go — readScope.locate; internal/tools/write_permit_test.go — TestMoveSourceStaysInWorkspaceUnderAPermit, TestApprovedEscapeThroughAWorkspaceLinkLandsOnTheDisclosedTarget; internal/tools/undo_journal_test.go — TestMoveFileJournalsBothEnds, TestCopyDirectoryJournalsEveryDestination; internal/agent/dispatch_test.go — TestToolResultEventCarriesToolAndWriteTarget

**Tests.** `file_ops_test.go` (28), `TestMoveSourceStaysInWorkspaceUnderAPermit`,
`TestApprovedEscapeThroughAWorkspaceLinkLandsOnTheDisclosedTarget`, `TestPermitWidensNoRead`,
`internal/agent/dispatch_test.go` `TestToolResultEventCarriesToolAndWriteTarget` unchanged.

**Acceptance.**
- `go test ./internal/tools ./internal/agent -run 'Copy|Move|FileOps|Permit|WriteTarget|Dispatch|ApprovedEscape'`

**Commit:** `refactor(tools): copy_file and move_file take their destinations from writeTarget`

## 21. Undo capture is structural

**What.** Depends on items 19–20. `capturePreImage`, `commit`, `commitReadBack` move behind the
value's `write`/`journaled` methods (unexported; no writer can reach a capture except through
them); `TestUndoCaptureHasExactlyTwoCallers` (source scan) is retired — `TestUndoJournalCoversEveryWriter`
stays as the coverage direction. ADR 0051 gains a dated amendment naming the value as what turns
decision 3 into a property; `internal/tools/doc.go` and `CONTEXT.md`'s undo-capture sentence follow;
`doc.go`'s section split is recounted (the "five-tool git.go" sentence says six).

**Regression guard.** The `doc.go` file-map sentence is dropped (item 18 owns it). "Unexported; no writer can reach a capture except through them" is not a property in Go — every writer lives in package `tools`, so one may still call `capturePreImage`/`commit` directly — and ADR 0051's 2026-08-24 amendment names the source scan as what "turns decision 3 from a claim into a property", so the item yields to it: `TestUndoCaptureHasExactlyTwoCallers` stays, re-pointed at the value's file (`undoFunnelFile = "write_target.go"`, the method spellings in `undoCaptureSites`), the `no match` acceptance grep goes, and the ADR amendment records the funnel's move onto the value, not a retirement of the scan (moving the capture pair into its own package, where the compiler enforces it, is the one alternative under which the amendment may call it a property).

**Files:** `internal/tools/path_safety.go`, `internal/tools/write_target.go`, `internal/tools/undo_journal_test.go`, `internal/tools/doc.go`, `docs/adr/0051-*.md`, `CONTEXT.md`
**Read first:** internal/tools/path_safety.go — safeWriteFile, journaledMutation, capturePreImage, preImage.commit, preImage.commitReadBack, mutationPath; internal/tools/undo_journal_test.go — TestUndoCaptureHasExactlyTwoCallers, undoFunnelFile, undoCaptureSites, TestUndoJournalCoversEveryWriter, undoProbes; internal/tools/doc.go — "The tool files, one line each" (five-tool git.go sentence), path_safety.go paragraph (the two funnels); docs/adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md — Amendment (2026-08-24); CONTEXT.md — Undo journal (shared write funnel sentence); internal/tools/docmap_test.go — TestDocMapNamesEveryFile

**Tests.** `TestUndoJournalCoversEveryWriter`, `TestUndoCaptureHasExactlyTwoCallers` (re-pointed at
`write_target.go`), `TestDocMapNamesEveryFile`, the undo suite unchanged.

**Acceptance.**
- `go test ./internal/tools -run 'Undo|DocMap'`

**Commit:** `refactor(tools): undo capture funnels through writeTarget; the source scan follows it`

## 22. One confinement handoff in `internal/subprocess`

**What.** `internal/subprocess/subprocess.go` exports `ConfinementHandoff(ctx, program string)
(prepare func(*exec.Cmd) error, confined bool, box *domain.ConfinementBox, err error)` — the one
rule for "a Confinement handle on ctx": absent ⇒ unconfined; nil Confiner ⇒ refuse with the one
sentence "confine %s: %w: the installed handle carries no Confiner"; `Confine` then `ScratchEnv`
seeded after `cmd.Environ()` (`TMPDIR=<scratch>/tmp` pinned). `run` calls it;
`internal/tools/console_open.go` `consolePrepare` becomes a three-line call that hands `prepare`
to `console.Spec.Prepare` and `confined` to `Spec.Confined` — the console package keeps its "does
not confine" boundary (`console/doc.go`) untouched. Denial-watch wiring stays per spawner.
Ride-alongs (comment-only): `internal/tools/web_fetch.go` and `internal/sanitize/sanitize.go` stop
naming the deleted `library.SanitizeContent` (`neuterInert` is now the only wholesale dropper).

**Files:** `internal/subprocess/subprocess.go`, `internal/subprocess/subprocess_test.go`, `internal/tools/console_open.go`, `internal/tools/console_open_test.go`, `internal/tools/web_fetch.go`, `internal/sanitize/sanitize.go`
**Read first:** internal/subprocess/subprocess.go — run (confine block after NewProcessTeardown, before the denial watch), SubprocessResult.Confined, SubprocessResult.Box; internal/subprocess/scratchenv.go — ScratchEnv; internal/tools/console_open.go — consolePrepare, ConsoleOpen.Execute; internal/console/process.go — Spec.Prepare, Spec.Confined, Start; internal/subprocess/subprocess_test.go — fakeConfiner, TestRunSubprocessNilConfinerFailsClosed, TestRunSubprocessConfinedRunSeedsTheScratchEnv; internal/tools/console_open_test.go — TestConsoleOpen_ConfinementUnavailablePropagates, TestConsoleOpen_ConfinedConsoleCarriesTheSeededScratchEnv; internal/tools/web_fetch.go — neuterInert; internal/sanitize/sanitize.go — the "drop wholesale" comment

**Tests.** `TestRunSubprocessNilConfinerFailsClosed`, `…RecordsConfined`, `…DenialWatchKillsConfinedRun`,
`…NeverWatchesUnconfined`, `…ConfinedRunSeedsTheScratchEnv`, `…UnconfinedRunKeepsTheHostEnv`;
`TestConsoleOpen_ConfinementUnavailablePropagates` (still a Go error from Execute so dispatch
demotes), `…ConfinesThroughTheHandleOnContext`, `…ConfinedConsoleCarriesTheSeededScratchEnv`,
`TestConsoleOpen_LiveConfinementDenialStopsTheConsole` (linux) unchanged.

**Acceptance.**
- `go test ./internal/subprocess ./internal/tools ./internal/console -run 'Subprocess|Confin|Console|Scratch'`
- `grep -rn 'carries no Confiner' --include=*.go internal | grep -v _test | wc -l` → 1.

**Commit:** `refactor(subprocess): one confinement handoff for the one-shot runner and the console`

## 23. `gitRead` — one read call under the six git tools

**What.** `internal/tools/git.go`: a `gitRef` type minted only by the ref guard (`validRef` +
`looksLikeOption`; `git_branch` keeps its own name guard — `validRef` would tighten it);
`gitRead(ctx, root, verb, refs []gitRef, pathspecs, flags, failWording, fallback string,
diffProducing bool)` owning per-verb hardening (`DiffHardeningArgs` ONLY when `diffProducing` —
`git status` takes none and would reject them), the one pathspec rule (`workspacePathspec`,
workspace-relative; `git_diff_range`'s absolute form changes to it), `Capture` + `gitResultText`
rendering. Migrate `git_status`, `git_log`, `git_diff_range`, `git_show`. `readonly_subprocess.go`'s
mint conditions read "spawns only through `gitRead` with a `gitRef`". Every fallback string
(`"git status failed"`, `"No commits found"`, `"git show failed"`), the `<ref>:./<rel>` object
spelling and `--porcelain=v2 --branch --ignore-submodules=dirty -z` unchanged. `lookGit`, `runGit`,
`runGitUnchecked`, `RunGitQuery` stay (plan `2026-09-15 - 01` item 16); every `gitexec.Program`/
`Resolve` call still passes `lookGit`.

**Regression guard.** Premise: `gitRead`/`gitWrite` return the raw `subprocess.SubprocessResult` (git_show's blob, git_status's porcelain, git_commit's ExitCode-keyed pre-check and summary, stageGitPaths' first-line note read it) alongside the rendered text — `gitResultText` stays each caller's rendering for log/diff and the failure branches, so git_show's `renderFile(res.CombinedOutput)` keeps its untrimmed blob (`TestGitShow_ReadsTheEarlierContentAtARef`, `TestGitShow_InheritsTheOpenEndedCap`) and its `<ref>:./<rel>` object is passed as an argument the refs/pathspecs slots need not spell. `gitRead` takes the timeout (or keys it per verb) so diff and show keep `gitDiffTimeout` (10s) and status and log keep `gitTimeout` (15s); the choice is recorded in NOTES.

**Files:** `internal/tools/git.go`, `internal/tools/readonly_subprocess.go`, `internal/tools/git_test.go`, `internal/tools/doc.go`
**Read first:** internal/tools/git.go — runGit, gitResultText, validRef, looksLikeOption, workspacePathspec, GitStatus.Execute, GitLog.Execute, GitDiffRange.Execute, GitShow.Execute, gitTimeout, gitDiffTimeout; internal/gitexec/gitexec.go — Capture, Program, DiffHardeningArgs; internal/tools/readonly_subprocess.go — readOnlySubprocess (mint conditions comment); internal/tools/git_test.go — TestGitReadTrio_ArgvCarriesTheHardening, recordGitArgv, hasHardeningPair, TestGitShow_ArgvIsHardenedAndCwdRelative, TestGitShow_ReadsTheEarlierContentAtARef, withFakeGit; docs/design/confinement-execution-contract.md — §4 RO-subproc mint ("spawns git through runGit")

**Tests.** `TestGitReadTrio_ArgvCarriesTheHardening`, `TestGitShow_ArgvIsHardenedAndCwdRelative`
become tests of `gitRead`; the 63 `git_test.go` tests (`withFakeGit`) and the RO-subproc classify
tests unchanged in outcome; `git_diff_range`'s recorded argv changes from absolute to relative
pathspecs (named in NOTES).

**Acceptance.**
- `go test ./internal/tools -run 'Git|Classify|ReadOnly'`
- `grep -c 'gitexec\.\(Program\|Resolve\)(' internal/tools/git.go` equals `grep -c ', lookGit)' internal/tools/git.go`.

**Commit:** `refactor(tools): gitRead — one validated read call under status, log, diff and show`

## 24. `gitWrite` — branch, commit and stage

**What.** Depends on item 23. `gitWrite(ctx, root, verb, args, failWording string)` for the
mutating verbs; migrate `git_branch`, `git_commit` (its `branch -r --contains HEAD` pre-check and
`log -1 --oneline` summary remain extra reads through `gitRead`; `--no-gpg-sign` kept; the `--`
pathspecs move to the relative rule) and `git_stage.go`'s two runs. `"commit created"` and every
other wording unchanged.

**Regression guard.** Premise: `gitRead`/`gitWrite` return the raw `subprocess.SubprocessResult` (git_show's blob, git_status's porcelain, git_commit's ExitCode-keyed pre-check and summary, stageGitPaths' first-line note read it) alongside the rendered text.

**Files:** `internal/tools/git.go`, `internal/tools/git_stage.go`, `internal/tools/git_test.go`, `internal/tools/git_stage_test.go`
**Read first:** internal/tools/git.go — GitCommit.Execute (amend pre-check, add, commit, log -1 summary), GitBranch.Execute, buildBranchArgs, remoteBranchesListed; internal/tools/git_stage.go — stageGitPaths (silent skips: absent git, refused program, probe non-zero), stagingSkipped, literalPathspec; internal/tools/git_stage_test.go — stagingConfiner, gitSubcommandOf, TestStageGitPaths_ConfinesTheGitChild, TestStageGitPaths_UnconfinableChildIsANote; internal/tools/git_test.go — TestGitCommit_PassesNoGpgSign, TestGitCommit_PathEscapeRejected, TestGitCommit_AmendRefusedWhenHEADIsBehindItsRemote, TestGitBranch_RunsUnderConfine

**Tests.** `TestGitCommit_PassesNoGpgSign`, `git_stage_test.go`, `git_windows_test.go`
(`GOOS=windows go vet ./internal/tools`) unchanged in outcome.

**Acceptance.**
- `go test ./internal/tools -run 'Git' && GOOS=windows go vet ./internal/tools`
- `grep -c 'runGit(' internal/tools/git.go internal/tools/git_stage.go` → the two calls inside `gitRead`/`gitWrite` only (record in NOTES).

**Commit:** `refactor(tools): gitWrite — branch, commit and stage over one write call`

## 25. One syntax engine for the Go half of `diagnostics`

**What.** Recast at the regression check (2026-09-16). `internal/tools/diagnostics.go`:
`goSyntaxDiagnostics`' own `go/parser` call is replaced by `syntaxcheck.Check`;
the diagnostics door keeps naming it (`abs:line:col: msg` — column preserved from the checker's
`Column`); the vet half still short-circuits on a syntax failure; `detectLanguage` stays Go-only
and the `ApprovalScope` truth table unchanged. **Ratified:** non-Go files keep
`noDiagnosticsMessage` ("no diagnostics available for X (no diagnostics provider for this file
type)") and the tool's announced description stays true. `internal/tools/doc.go` and `CONTEXT.md`'s
syntax-check sentence name the one engine.

**Regression guard.** diagnostics calls a Go-only entry `syntaxcheck.CheckGo` (exported over checkGoSyntax) WITHOUT the blank-content short-circuit, so an empty or whitespace-only .go file stays an error result exactly as today, and `Check`'s blank rule stays for the trailer; the diagnostics door keeps today's wording shape — the FIRST error as `abs:line:col: msg` plus "(and N more errors)" when more follow — built from `Result.Errors`, so no announced string changes; the shared piece is the parser, the trailer keeps its own renderer — `internal/tools/syntaxtrailer.go` is not edited: its `syntax check: N problem(s)\n  line N: msg` rendering (capped at `maxSyntaxTrailerErrors`) and the door's `abs:line:col: msg (and N more errors)` differ by design, and no renderer is extracted or shared between them (round 2).

**Files:** `internal/tools/diagnostics.go`, `internal/syntaxcheck/syntaxcheck.go`, `internal/syntaxcheck/syntaxcheck_test.go`, `internal/tools/diagnostics_test.go`, `internal/tools/syntaxtrailer_test.go`, `internal/tools/doc.go`, `CONTEXT.md`
**Read first:** internal/tools/diagnostics.go — diagnoseGo, goSyntaxDiagnostics, cleanGoMessage, noDiagnosticsMessage, detectLanguage, runGoVet; internal/syntaxcheck/syntaxcheck.go — Check, checkGoSyntax, Result, Error; internal/tools/syntaxtrailer.go — syntaxTrailer, maxSyntaxTrailerErrors;
internal/tools/diagnostics_test.go — TestDiagnostics_GoSyntaxErrorReportedInProcess, TestDiagnostics_UnsupportedLanguageDegradesGracefully, TestDiagnostics_RefusesEscapingSymlink, withFakeGo, writeGoFile; internal/tools/syntaxtrailer_test.go — TestSyntaxTrailerReportsGoProblemsWithTheirLines, TestSyntaxTrailerCapsTheProblemsItSpells;
internal/tui/toolpresent_test.go — the "syntax check: 1 problem(s)" literal; internal/tools/doc.go — diagnostics.go and syntaxtrailer.go lines; CONTEXT.md — the syntaxtrailer-over-syntaxcheck sentence

**Tests.** `TestDiagnostics_GoSyntaxErrorReportedInProcess` (names `broken.go`),
`TestDiagnostics_UnsupportedLanguageDegradesGracefully` (`main.rs` ⇒ "no diagnostics available"),
`TestDiagnostics_RefusesEscapingSymlink`, `syntaxtrailer_test.go`, the write/edit/find-replace
trailer wording tests ("syntax check: N problem(s)\n  line N: …") and the TUI `toolpresent_test.go`
literal unchanged; new: an empty and a whitespace-only `.go` file stay an error result through
`diagnostics` (`CheckGo` has no blank short-circuit) and the door's first-error-plus-"(and N more
errors)" wording is pinned; `syntaxcheck_test.go` gains the `CheckGo` case, `Check`'s blank rule
unchanged.

**Acceptance.**
- `go test ./internal/tools ./internal/syntaxcheck -run 'Diagnostics|Syntax|Trailer|DocMap'`
- `grep -c 'parser.ParseFile' internal/tools/diagnostics.go` → 0.

**Commit:** `refactor(tools): diagnostics' Go syntax verdict comes from syntaxcheck`
