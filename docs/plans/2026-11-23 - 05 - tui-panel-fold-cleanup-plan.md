# TUI panel, fold and grouping cleanup — plan

**Goal:** close the five TUI architecture-review cleanups: each tool's grouping class is one registry column, a reader's fold gesture is one act the fold module answers, the side panel's frame is one computed value, and navigation mode walks two region adapters. Behaviour on screen does not change; the only wire change is the ratified `task_list` solo verdict.

**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7d9589d1

**Regression check (2026-11-23, 7d9589d1):**
- 2: guard folded (Acceptance grep for `solo:`; the solo-count rule reaches `layout.md`).
- 3: guard folded (ask_user and targetless rows in the class test; `presenter()` spelling and gated `ownGroupAt`; checkable Goal grep).
- 4: guard folded (member act always flips; test scopes); also states that `revealBlockMember` survives (item 6's guard).
- 5: guard folded (`TestLoneDelegation` pattern; assertions from an open, untouched state).
- 6: guard folded (every Goal symbol grepped both ways; the moved run named end to end).
- 7: recast — input carries no `slotBottom`; width delegates never reach `frameSpans()` (writer decision).
- 8: guard folded — `newPanelFrame` takes `slotBottom` from its caller (writer decision), plus zero-geometry delegates and a section-paint counter.
- 9: guard folded — wheel and hover read only `pre`'s snapshot (writer decision), plus off-column paints nothing and a Goal grep that can fail.
- 10: guard folded — one row bound per key (writer decision, superseding the report's reach-only frame build), plus the `^x` swap moved here and the `navRegion` int type renamed.
- 11: guard folded (`sidepanel.go` in Files).
- 7 (re-check): guard folded — `cell(x, y, slotBottom)` with the zero-arg `panelCell` delegate reading `frameSpans().slotBottom` as today; `overlaid` gated on `overlayable` in the geometry, plus an above-the-gate row in `TestPanelGeometryGateAndClamp`.

**Sources:**
- `.capcom/issues/panel-resume-tui-fold.md`, `tool-grouping-class-scattered.md`, `fold-gesture-setters-leak.md`, `panel-geometry-scattered.md`, `nav-verbs-per-region.md`
- `docs/reviews/architecture-review-2026-11-10.html` (#1, #5, #6), `docs/reviews/architecture-review-2026-11-21.html` (#6, #7)
- ADR 0035 D2 + amendments 1118, 1120, 1120c, 1123; ADR 0083 §1; ADR 0052 D5; ADR 0110 D6 (+ amend-1108); ADR 0111 D4 (+ amend-1109b), D7, D12; ADR 0031
- `internal/tui/doc.go` (value-copied Model rule; navigation-mode paragraph)

**Ratified design calls:**
- **Scope:** the TUI panel/fold cluster only; the other ten review cleanups stay open (owner, 2026-11-23).
- **panel-resume-tui-fold:** only the pin test on `panelWriterTargetArgs`; the `FoldRecord` door and the structured task-list wire field are declined (owner triage, 2026-11-21).
- **task_list grouping:** its registry row declares `groupSolo` explicitly; new `task_list` records carry `"solo":true` on the transcript wire (owner, 2026-11-23).
- **ADR 0111 D4 names:** `panelTargetAt` and `followPanelTarget` stay as named entry points (delegates where needed), so D4 needs no amendment (writer, 2026-11-23).
- **Verb vs group phrase, `groupPathArg`:** not duplicates (they answer different sentences); left as they are (writer, 2026-11-23).

**Standing requirements:**
- skills: coding-standards
- Tests are narrowed to one package and a `-run` pattern; never race-run all of `./internal/tui/`.
- The Bubble Tea `Model` is copied by value: no pointer, memo, `sync.Once` or `strings.Builder` held on it; a frame value lives only on a per-Update copy (`pre`), never cached across Updates.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The other ten architecture-review issues (config, panes, paste, advise, Driver host, exchange holds, publish steps, session seat, setting source, run fill).
- Merging `shapeSubAgentGroup` and `shapeSkillGroup`.
- Any change to fold, grouping, panel or navigation behaviour a reader can see; engine, `domain` and session record shapes beyond the `task_list` solo verdict.

## 1. Pin the panel's writer-target map to the tools' writers — ✅ DONE (2026-11-23)

**What:**
**Goal:** a test in `internal/tui` fails when a workspace-writing tool is missing from `panelWriterTargetArgs`, when the map names a non-writer, or when the map names the wrong target argument.
**Approach (assumed at the header base):** test-only. Enumerate the built-in tools with `tools.DefaultToolsWithHost(t.TempDir(), tools.HostTools{Enabled: tools.KnownToolNames()})` (lifts default-off tools); probe each with `tools.WorkspaceWriteTarget(tool, call)` on a call carrying `{"path":"p","destination":"d","source":"s"}` — the writer set must equal the map's keys. For each writer, a call carrying only `{<mapped arg>: "only.txt"}` must resolve, with `filepath.Base` of the target equal to `only.txt`.
**Files:** internal/tui/sidepanel_test.go
**Read first:** internal/tui/sidepanel.go — panelWriterTargetArgs, replayedWriteKey; internal/tools/workspace_scoped.go — WorkspaceWriteTarget, writeTargetOf, pathArgWriteTarget, destinationArgWriteTarget; internal/tools/registry.go — DefaultToolsWithHost, KnownToolNames
**Tests:** new `TestPanelWriterTargetArgsCoverEveryWriter`.
**Acceptance:**
- `go test -race -count=1 -run 'TestPanelWriterTargetArgsCoverEveryWriter' ./internal/tui/`
- `go vet ./internal/tui/`
**Closes:** panel-resume-tui-fold
**Commit:** `test(tui): pin the panel's writer-target map to the tools' writers`

## 2. A registry row declares its grouping class — ✅ DONE (2026-11-23)

NOTES (2026-11-23): the item's three new tests live where their siblings do — `TestEveryRegistryRowDeclaresItsGroupingClass` (with its name→class table `groupingClassByTool`) in toolregistry_test.go, `TestPresentToolCallSoloFollowsTheGroupingClass` in toolpresent_test.go, `TestTranscriptCodecReDerivesSoloFromTheGroupingClass` in transcriptbridge_test.go (one subtest per registry row, plus a background sub-agent record and an add-only case); the existing `TestTranscriptCodecReDerivesSkillFetchSolo` comment now cites `toolPresenter.grouping` instead of the removed `toolPresenter.solo`.
NOTES (2026-11-23): layout.md's "What stays standalone" paragraph now names the grouping class and lists the own-kind pair (Sub-agent, Skill), the three solo calls (Fan-Out, Tasks, background Sub-agent) and the answered Ask User as the result-time verdict no class states; its later "kept the first two out by accident" became "kept the answered question and the delegation out", since the list's order changed.

**What:**
**Goal:** `toolPresenter` carries a `grouping groupClass` field (`groupUmbrella` zero value, `groupOwn`, `groupSolo`) and no `solo` field; `sub_agent` and `load_skill` rows are `groupOwn`, `fan_out`, `task_list` and the background sub-agent presenter are `groupSolo`; `grep -n 'solo:' internal/tui/toolregistry.go internal/tui/toolview.go` prints nothing. The persisted `Solo` verdict and `toolView.solo` stay (ADR 0083 §1).
**Approach (assumed at the header base):** in `internal/tui/toolregistry.go` add the type and column; `backgroundSubAgentPresenter` in `toolview.go` declares `groupSolo`. `presentToolCall` and `presentBackgroundSubAgentCall` set `tv.solo = p.grouping != groupUmbrella`. The decode re-derivation in `transcriptbridge.go` (`fromWireToolView`) reads the class and stays add-only: `w.Solo || class != groupUmbrella` plus the answered-ask_user footprint. The ask_user result-time rule (`askUserAnswerRecord` setting `out.Solo`, OR-ed in by the outcome hook) stays and its comment names it the one verdict a class cannot state. `groupable` (`toolbranch.go`) is unchanged — paint never reads the registry. Fix the stale "two/three solo rows" comments (rule: every comment counting solo rows; `grep -rn 'solo' internal/tui/*.go`).
**Regression guard.** The Goal's grep is an Acceptance check, and it covers the toolView literal `solo: p.solo` in `presentBackgroundSubAgentCall` (toolview.go:1014) as well as the presenter rows. The stale-count rule covers every prose that counts or lists solo calls — `grep -rn -i 'solo' internal/tui/*.go layout.md docs/layout/` — including layout.md's "Three calls say it" (≈:929-931), which `task_list` (and the already-solo `fan_out`) make wrong.
**Files:** internal/tui/toolregistry.go, internal/tui/toolview.go, internal/tui/transcriptbridge.go, internal/tui/toolbranch.go, internal/tui/doc.go, internal/tui/toolregistry_test.go, internal/tui/toolpresent_test.go, internal/tui/transcriptbridge_test.go, layout.md
**Read first:** internal/tui/toolregistry.go — toolPresenter.solo, toolRegistry (sub_agent, fan_out, task_list, load_skill rows), askUserAnswerRecord; internal/tui/toolview.go — backgroundSubAgentPresenter, presentToolCall, presentBackgroundSubAgentCall; internal/tui/transcriptbridge.go — fromWireToolView; layout.md — "What stays standalone"
**Tests:** new `TestEveryRegistryRowDeclaresItsGroupingClass` (explicit name→class table, walked both ways over `toolRegistry`), `TestPresentToolCallSoloFollowsTheGroupingClass`, `TestTranscriptCodecReDerivesSoloFromTheGroupingClass` (one case per row, including an old record of `task_list` without `solo`).
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestEveryRegistryRow|TestPresentToolCall|TestTranscriptCodec|TestAnsweredAskUser|TestWorkflowFinish|TestToolRegistryCovers|TestAskUser' ./internal/tui/`
- `go test -race -count=1 ./internal/session/`
- `! grep -n 'solo:' internal/tui/toolregistry.go internal/tui/toolview.go`
**Commit:** `refactor(tui): a tool's grouping class is one registry column`

## 3. Own groups key on the grouping class, not on tool names — ✅ DONE (2026-11-23)

NOTES (2026-11-23): `toolBlockAt` gates on a new `ownGroupedAt(entries, i)` (bounds + `entryToolCall` + `ownGrouped`) so an out-of-range index still answers shapeEntry as before; the Sub-agent vs Skill shape is picked by `entries[i].headsRun()`.
NOTES (2026-11-23): `TestEveryRowGroupsByItsClass` lives in blockshape_test.go beside `TestToolBlockShapes`; toolshape_test.go needed no change. It feeds every row one superset argument object (`rowGroupingArgs`) and fails setup if an umbrella row presents no target; own rows are pinned by name in `ownShapeByTool`, and a background sub-agent pair is a subtest.
NOTES (2026-11-23): consequential edit — internal/tui/toolregistry.go: made necessary by blockshape.go no longer keying on `loadSkillToolName` (its comment said the own-kind grouping keys on the constant).
NOTES (2026-11-23): consequential edit — internal/tui/toolbranch.go: made necessary by the same change (the `groupable` comment cited `subAgentToolName, loadSkillToolName` as the own-group key).
NOTES (2026-11-23): consequential edit — internal/tui/toolview.go: made necessary by the same change (the `presentToolCall` comment cited `loadSkillToolName` as what a skill fetch groups by).

**What:**
**Goal:** `toolBlockAt` (`internal/tui/blockshape.go`) forms an own group from the head's grouping class; `grep -n 'loadSkillToolName' internal/tui/blockshape.go` prints nothing and `toolBlockAt` names no tool constant (`subAgentGroup`/`subAgentGroupAt` keep `subAgentToolName` for delegation waves). Depends on item 2.
**Approach (assumed at the header base):** add `ownGrouped(tv)` reading `p, _ := tv.presenter(); return p.grouping == groupOwn` (through `presenter()`, so a background sub-agent call never joins a "Sub-agents (N)" group). `toolBlockAt` makes one `ownGroupAt(entries, i, entries[i].tool.name)` call and picks `shapeSubAgentGroup` when the head heads a run, else `shapeSkillGroup`. `subAgentGroup(At)` for delegation waves stays.
**Regression guard.** `toolBlockAt` calls `ownGroupAt` only when `entries[i].kind == entryToolCall && ownGrouped(entries[i].tool)` — `ownHeads` matches any non-background call against its own name, so an ungated call would group two adjacent `read_file` calls. In `TestEveryRowGroupsByItsClass` the `ask_user` row is finished with an `IsError` result (or left pending), since an answered one is made solo by `askUserAnswerRecord`; a targetless solo row (`task_list`) is given no target and asserts `shapeEntry`.
**Files:** internal/tui/blockshape.go, internal/tui/toolshape_test.go, internal/tui/blockshape_test.go
**Read first:** internal/tui/blockshape.go — toolBlockAt, ownGroupAt, ownHeads, subAgentGroupAt; internal/tui/toolview.go — toolView.presenter; internal/tui/blockshape_test.go — TestToolBlockShapes; internal/tui/toolregistry.go — askUserAnswerRecord; internal/tui/transcriptbridge_test.go — TestTranscriptCodecReDerivesAnsweredQuestionSolo (errored-question fixture)
**Tests:** new `TestEveryRowGroupsByItsClass` — per registry row, two adjacent finished calls (with a target where the row has an extractor; `ask_user` errored or pending): umbrella → `shapeToolSuper`, own → a group shape, solo → `shapeEntry`.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestEveryRowGroupsByItsClass|TestToolBlockShapes|TestPaintAndFold|TestSkill|TestRenderGroupBreakers|TestUnregisteredCalls|TestLoneDelegationAndSkillFetch' ./internal/tui/`
- `! grep -n 'loadSkillToolName' internal/tui/blockshape.go`
**Closes:** tool-grouping-class-scattered
**Commit:** `refactor(tui): own groups key on the grouping class`

## 4. One reader act moves a block's fold — ✅ DONE (2026-11-23)

NOTES (2026-11-23): `revealBlockMember` keeps its body and now reports whether a foldable block holds the index (the reveal act's `acted`); `applyFoldAct`'s reveal arm calls it rather than holding a copy of the body, so it survives as the transcript method item 6 moves.
NOTES (2026-11-23): `foldOutcome.folded` is the fold of the foldable block holding the act's index once the act has landed, the caller's shared flip included (false where no foldable block holds it); no gesture caller reads it today, the tests do.
NOTES (2026-11-23): the task-list branch of `toggleBlockAt` no longer runs the old trailing `markBlockTouched`: a task_list card is `groupSolo`, so it sits in no foldable block and that touch was always a no-op — behaviour unchanged.
NOTES (2026-11-23): the shared header arm now touches before the Model's `toggleToolsFold` rather than after it; neither write reads the other, so the end state is the same.

**What:**
**Goal:** gesture callers never touch the fold rule or its setters: `grep -nE 'markBlockTouched|openBlockAlone|setBlockFolded|blockFoldBy|foldBy(Settled|Shared|Head|None)|revealBlockMember|toggleTypeExpanded' internal/tui/mouse.go internal/tui/sidepanel.go | grep -v '//'` prints nothing; fold behaviour is unchanged.
**Approach (assumed at the header base):** new `internal/tui/blockfold.go` (not `fold.go` — that holds the Event fold): `foldActKind` (`foldActHeader`, `foldActTypeRow`, `foldActMember`, `foldActEnterRun`, `foldActReveal`), `foldAct{kind, index}`, `foldOutcome{acted, folded, flipToolsOpen bool}`, and `(*transcript).applyFoldAct(foldAct) foldOutcome` holding today's switch from `Model.toggleBlockAt` (`mouse.go`) and the reveal body of `revealBlockMember`. `Model.actOnFold(foldAct) (Model, foldOutcome)` is the only reader of `flipToolsOpen` and calls `toggleToolsFold` (`settingsapply.go`) — the transcript never writes `toolsOpen` or config. Rewire `toggleBlockAt` and `followPanelTarget` (`sidepanel.go`). Binding: the settled arm never asks for the shared flip, even on a large umbrella (1118 c); type-row and member acts touch only after a toggle that moved; a reveal on an open block only touches, never sets `foldOwn`; a reveal on a group of one or a Sub-agent/Skill group sets no type-row flag; the enter-run touch lands before `openRunAt`; `toggleTaskListFold` is not routed through an act; no new field reaches `toWireEntry`/`fromWireEntry`. Update the comments describing the gesture switch (rule: every comment naming the per-arm setters; grep the names above across `internal/tui/*.go`).
**Regression guard.** `foldActMember` always flips `expanded` (`toggleExpanded`, which accepts every block-state kind — a long prompt or a solo call included) and touches only when the index is in a foldable block. `TestFoldActOnNoBlockDoesNothing` covers only the header, type-row, reveal and enter-run acts; `TestFoldActMovesTheGenerationOnlyOnAMove` covers only the header, member and no-block acts (a reveal re-sets an open type row, and `setTypeExpanded` touches unconditionally). `revealBlockMember` survives as a `transcript` method (item 6 moves it).
**Files:** internal/tui/blockfold.go, internal/tui/blockfold_test.go, internal/tui/transcript.go, internal/tui/mouse.go, internal/tui/sidepanel.go, internal/tui/settingsapply.go, internal/tui/blocktarget.go, internal/tui/doc.go
**Read first:** internal/tui/mouse.go — Model.toggleBlockAt; internal/tui/transcript.go — blockFoldBy, openBlockAlone, markBlockTouched, revealBlockMember, setTypeExpanded, setExpanded; internal/tui/sidepanel.go — Model.followPanelTarget
**Tests:** new on a bare transcript, asserting `blockFolded` after a follower: `TestFoldActHeaderOpensAnAutoCollapsedBlockAlone`, `TestFoldActHeaderOnLargeAsksForTheSharedFlip`, `TestFoldActHeaderFoldsASmallOneOnItsOwn`, `TestFoldActOpeningAPartTouches`, `TestFoldActEnteringARunTouchesItsGroup`, `TestFoldActRevealOpensAloneAndItsTypeRow`, `TestFoldActTouchCarriesAsTheGroupGrows`, `TestFoldActMovesTheGenerationOnlyOnAMove`, `TestFoldActOnNoBlockDoesNothing`.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestFoldAct|TestAutoCollapse|TestLargeUmbrellasShareOneFold|TestSmallUmbrellaHeaderFoldsItselfOnly|TestGroupHeaderFolds|TestGroupOfOne|TestPanelJumpOpensAFoldedLoneDelegation|TestLoneDelegationAndSkillFetch' ./internal/tui/`
- the Goal's grep prints nothing.
**Commit:** `refactor(tui): one reader act moves a block's fold`

## 5. Fold gesture tests assert observable fold, not flags — ✅ DONE (2026-11-23)

NOTES (2026-11-23): no Model test was removed — the touch, group-of-one and Sub-agent families each already hold one mouse, one ⏎ and one panel-jump Model test, and `TestAutoCollapseTouchMarksEveryTargetKind` keeps its per-target-kind matrix (it is the only routing check from a row kind to its act's touch); the bare-transcript unit cases (`TestAutoCollapseTouchWritersMoveTheGeneration`, `TestAutoCollapseExemptsTouchedUmbrellas`) now drive `applyFoldAct`.
NOTES (2026-11-23): touch is read as "settled, still open" and a fold of its own as "still open once large under the shut shared preference"; the header case of `TestAutoCollapseTouchMarksEveryTargetKind` gestures twice, `TestGroupOfOneFoldsOpensAndRefolds` ends on an open group (4 gestures from open, 3 from folded) and lands the live call's result before settling, and `TestLoneDelegationMemberRowOpensItsRunView` opens the group untouched by turning auto-collapse off rather than by a header click.
NOTES (2026-11-23): setup calls `markBlockTouched` (`TestAutoCollapseFoldsSettledGroups` "touched stays open") and `openBlockAlone` (`TestAutoCollapseTurnBoundaryAndTouch`) stay as fixture setup with their return checked; only their flag reads were dropped. Mutation check: dropping the touch write in `markBlockTouched`, or the `foldOwn` write in `openBlockAlone`, fails the rewritten tests.

**What:**
**Goal:** `grep -nE '\.foldTouched|\.foldOwn' internal/tui/*_test.go` prints nothing; the touch, group-of-one and Sub-agent fold cases assert observable fold (settle it, is it still open?). Depends on item 4.
**Approach (assumed at the header base):** rewrite `TestAutoCollapseTouchWritersMoveTheGeneration` on `applyFoldAct`; drive `TestAutoCollapseExemptsTouchedUmbrellas` with acts instead of setting flags; replace each flag read in the touch and panel-jump tests with a settle-then-fold assertion. Unit cases move to bare transcripts; keep one mouse, one ⏎ and one panel-jump Model test each, plus both config write-log tests. `setBlockFolded` used as fixture setup elsewhere stays.
**Regression guard.** Each settle-then-fold assertion is made from an OPEN, otherwise-untouched state (an even gesture count, or a group opened without a touch) before the settle — a test that ends folded (`TestGroupOfOneFoldsOpensAndRefolds` "live"/"auto-collapse off" after 3 gestures) or was already touched by a header click (`TestLoneDelegationMemberRowOpensItsRunView`) cannot tell touched from untouched. The Acceptance pattern runs `TestLoneDelegation`, not `TestSubAgent.*Fold` (which matches three unrelated transcript_test.go tests).
**Files:** internal/tui/autocollapse_touch_test.go, internal/tui/autocollapse_test.go, internal/tui/autocollapse_turnboundary_test.go, internal/tui/groupofone_test.go, internal/tui/subagentblock_test.go, internal/tui/blockfold_test.go
**Read first:** internal/tui/autocollapse_touch_test.go — TestAutoCollapseTouchWritersMoveTheGeneration, TestAutoCollapseTouchMarksEveryTargetKind; internal/tui/autocollapse_test.go — TestAutoCollapseExemptsTouchedUmbrellas, autoCollapseUmbrella; internal/tui/groupofone_test.go — TestGroupOfOneFoldsOpensAndRefolds, foldGroupOfOne; internal/tui/subagentblock_test.go — TestLoneDelegationMemberRowOpensItsRunView, TestPanelJumpOpensAFoldedLoneDelegation
**Tests:** the rewritten tests above.
**Acceptance:**
- `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestFoldAct|TestAutoCollapse|TestGroupOfOne|TestLoneDelegation|TestPanelJump|TestLargeUmbrellasShareOneFold' ./internal/tui/`
- the Goal's grep prints nothing.
**Commit:** `test(tui): fold gesture tests assert the fold, not the flags`

## 6. The block-fold rule lives in its own file — ✅ DONE (2026-11-23)

NOTES (2026-11-23): blockfold.go already existed (item 4 created it for applyFoldAct/actOnFold); the moved run, setToolsOpen through toggleTypeExpanded (transcript.go 2223-2655 at HEAD), is appended verbatim after actOnFold, with `internal/domain` imported for blockSettled's `domain.SubAgentFinished`. No comment named the run's file, so no cross-reference changed.

**What:**
**Goal:** `blockFoldRule`, `blockFoldBy`, `blockFolded`, `blockSettled` and the fold writers (`setToolsOpen`, `setToolsFoldOver`, `umbrellaIsLarge`, `setToolsAutoCollapse`, `setTurnLive`, `setBlockFolded`, `markBlockTouched`, `openBlockAlone`, `revealBlockMember`, `setTypeExpanded`, `toggleTypeExpanded`) are defined in `internal/tui/blockfold.go`, none in `transcript.go`. Depends on items 4 and 5.
**Approach (assumed at the header base):** a pure move of that contiguous run of `transcript.go`, no edits beyond imports and comment cross-references; reviewable with `git diff --color-moved`.
**Regression guard.** The run is `setToolsOpen` through `toggleTypeExpanded` (transcript.go ≈:2230-2665 at base); `reported`, `waitsOnUser`, `passBlock`, `lastOwnEntry` and `holdForApproval` sit inside it and move with it, and `setTaskListOpen` (before) and `setTaskExpanded` (after) stay. Every Goal symbol, `type blockFoldRule` included, is grepped for absence from `transcript.go` and presence in `blockfold.go`; `revealBlockMember` still exists after item 4 (item 4's guard).
**Files:** internal/tui/transcript.go, internal/tui/blockfold.go
**Read first:** internal/tui/transcript.go — setToolsOpen, blockFoldRule, blockFoldBy, blockSettled, holdForApproval, toggleTypeExpanded; internal/tui/transcript_test.go — toggleTypeExpanded table test; internal/tui/paintcache_test.go — type-row toggle case
**Tests:** none new.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestFoldAct|TestAutoCollapse|TestGroupOfOne' ./internal/tui/`
- `grep -nE '^(type blockFoldRule|func \(t \*transcript\) (blockFoldBy|blockFolded|blockSettled|setToolsOpen|setToolsFoldOver|umbrellaIsLarge|setToolsAutoCollapse|setTurnLive|setBlockFolded|markBlockTouched|openBlockAlone|revealBlockMember|setTypeExpanded|toggleTypeExpanded)\()' internal/tui/transcript.go` prints nothing, and the same pattern over `internal/tui/blockfold.go` prints 15 lines.
**Closes:** fold-gesture-setters-leak
**Commit:** `refactor(tui): the block-fold rule lives in blockfold.go`

## 7. The side panel's horizontal geometry is one pure value — ✅ DONE (2026-11-23)

NOTES (2026-11-23): the input's overlay field is `isOverlaid` (not `overlaid`), per the house boolean-prefix rule; `panelGeometryAs(isOverlaid)` and `panelGeometry()` live in sidepanel.go beside the delegates, and the chrome's yield rule is `newPanelChrome` in panelframe.go (the `panelChrome` type stays in sidepanel.go).
NOTES (2026-11-23): added `panelGeometry.onColumn(x)` (the horizontal half of `cell`), so the zero-arg `Model.panelCell` delegate checks the column before passing `m.frameSpans().slotBottom` — the old body short-circuited before composing the frame, and an eager `frameSpans()` would render every overlay pane on each wheel notch off the column.
NOTES (2026-11-23): consequential edit — internal/tui/doc.go: made necessary by the new panelframe.go (TestDocMapNamesEveryFile requires every file in the package map).
NOTES (2026-11-23): TestDocMapNamesEveryFile fails at HEAD f3ce72dc because item 6's blockfold.go is not named in internal/tui/doc.go — not this item's file; reported as FOLLOW-UP.

**What:** Recast at the regression check (2026-11-23).
**Goal:** `newPanelGeometry(panelGeometryInput) panelGeometry` in new `internal/tui/panelframe.go` takes no `Model` and answers shown, overlayable, overlaid, docked width, paint width, chrome, overlay gap, content width, pin x, slot width, `cell(x, y, slotBottom)`, `gripAt(x)` and `clamp`; the gate and clamp rules are covered by table tests.
**Approach (assumed at the header base):** input `{window, show, width domain.SidePanelWidth, overlaid, fullRowWidth}`. `Model.panelGeometryAs(overlaid)` builds it; `Model.panelGeometry()` is the one place deriving `overlaid` from the cursor (today's `panelOverlaid` rule, including `blockCursorOwnsKeys`). The existing helpers in `sidepanel.go` (`panelWidth`, `panelPaintWidth`, `panelContentWidth`, `panelChrome`, `panelPinX`, `panelCell`, `panelGripAt`, `clampPanelWidth`, `panelOverlayGapHung`, …) and `model.go` (`panelShown`, `slotWidth`) become one-line delegates — tests call them widely. Binding (ADR 0110 D6, 0111 D7): `frameKey` and `slotWidth` keep reading the docked width (0 under the overlay); only paint reads the paint width.
**Regression guard.** `panelGeometryInput` carries no `slotBottom` and building a `panelGeometry` never reaches `frameSpans()` (which renders every overlay pane at `slotWidth()`); the width delegates `panelShown`, `panelWidth`, `panelPaintWidth`, `slotWidth` and `frameKey` build geometry from window, show, width setting and overlay state only; `cell(x, y, slotBottom)` takes the row bound as an argument from its caller (`pre.spans` / View's `belowSlot`); the zero-arg `Model.panelCell(x, y)` delegate passes `m.frameSpans().slotBottom` as today, so its mouse.go:2195 and `panelTargetAt` callers are unchanged; add `TestPanelWidthNeverComposesTheFrame` (or an equivalent pane-render-count assertion) proving `frameSpans` is not reached from `panelWidth`/`slotWidth`, and keep `TestOverlayPanesRenderOncePerUpdateAndView` and `TestSettingsClickBesideTheSidePanel` green.
The geometry gates the overlay itself: `overlaid = input.overlaid && overlayable` (today's `panelOverlaid`), so a docked column (window above the gate) with the nav cursor in the panel keeps the docked paint width, inset 0 and a live grip; `TestPanelGeometryGateAndClamp` carries that row.
**Files:** internal/tui/panelframe.go, internal/tui/panelframe_test.go, internal/tui/sidepanel.go, internal/tui/model.go
**Read first:** internal/tui/sidepanel.go — panelOverlaid, panelPaintWidth, panelChrome, panelCell, panelGripAt; internal/tui/model.go — frameSpans; internal/tui/panes.go — paneRenders; internal/tui/paintcache_test.go — TestOverlayPanesRenderOncePerUpdateAndView
**Tests:** new `TestPanelGeometryGateAndClamp` (including a window above the gate with `overlaid: true`: paint width == docked width, inset 0, grip live), `TestPanelGeometryChromeYields`, `TestPanelGeometryOverlayGapAndInset`, `TestPanelGeometryGripAndPin`, `TestPanelWidthNeverComposesTheFrame` (reads the process-wide `paneRenders` counter, so it never calls `t.Parallel()`); keep `TestOverlayPanesRenderOncePerUpdateAndView` and `TestSettingsClickBesideTheSidePanel` green.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestPanelGeometry|TestPanelWidthNeverComposesTheFrame|TestSidePanel|TestOverlay|TestSubAgentRowFacts|TestSlotPanesFitBesideTheSidePanel|TestSettingsClickBesideTheSidePanel|TestSettingsPaneReReadMovesTheSidePanelWidth' ./internal/tui/`
**Commit:** `refactor(tui): the side panel's geometry is one pure value`

## 8. One panel frame per View for paint and stops — ✅ DONE (2026-11-23)

NOTES (2026-11-23): the frame builder is `Model.newPanelFrame(geometry, rows)` (a Model method: painting the sections and the body stops reads the Model); the geometry-taking paint functions use an `In` suffix (`panelSectionsIn`, `panelSectionIn`, `panelBodyLeadIn`, `panelTextRoomIn`, `sidePanelContentIn`, `sidePanelColumnIn`, `panelCursorRowIn`), and the stop walk moved from `panelStops` into `layPanelStops(sections, layout)` in panelstops.go.
NOTES (2026-11-23): `panelSubAgentRow`, `panelFileRow` and `panelMCPRow` also keep zero-geometry delegates (over new `…In` versions), since sidepanel_test.go calls them; `panelRow`, `panelHeaderRow`, `panelTaskRow`, `panelUsageRows` and `panelFillRow` (no test callers) take the geometry as a first parameter instead. `panelTaskRow`'s three branches now pick glyph and style and share one `panelRow` call.
NOTES (2026-11-23): the section-paint counter is `panelSectionPaints` (sidepanel.go), counted once per `panelSectionsIn` call; `panelTargetAt` and `navPanelStops` still reach the frame through the `panelStops(rows)` delegate (items 9 and 10 own those paths).
NOTES (2026-11-23): consequential edit — internal/tui/doc.go: made necessary by the panel frame landing in panelframe.go (the file map's panelframe.go role named only the geometry).

**What:**
**Goal:** `View` builds one `panelFrame` (geometry, sections painted once at its content width, layout, rows, stops); `sidePanelColumn` and `panelCursorRow` read that frame, so the sections are painted once per frame. Depends on item 7.
**Approach (assumed at the header base):** `panelFrame{geom, sections, layout, rows, stops}` from `newPanelFrame` in `panelframe.go`: geometry → `panelSections` at its content width → `panelLayout` → stops (`panelHeaderStops`, `panelBodyStop`). Pass the geometry into `panelSections`, `panelBodyLead`, `panelTextRoom`, `panelHeaderRow` instead of each row calling `panelChrome`/`panelContentWidth`. `panelStops(rows)` becomes a delegate. Binding: a frame built "as overlaid" on a narrow window paints at the overlay's width; header stops stay pin before title (ADR 0111 amend-1109b); the stops' row count comes from the same `slotBottom` as `View`'s `belowSlot`; frame slices are never mutated after the build.
**Regression guard.** `newPanelFrame` takes `slotBottom` from its caller (View's `belowSlot`, or `pre.spans` on the click/hover path) and never calls `frameSpans()` itself; a frame is built at most once per View and once per key/click/motion, never from a width delegate. Zero-geometry delegates stay for `panelSections()`, `panelSection(s)`, `panelBodyLead`, `panelTextRoom`, `sidePanelContent(rows)`, `sidePanelColumn(rows)` and `panelCursorRow(rows)` (as item 7 does), so no test file outside Files changes. "Painted once per frame" is pinned by a package-level atomic section-paint counter shaped like `paneRenders`.
**Files:** internal/tui/panelframe.go, internal/tui/panelframe_test.go, internal/tui/sidepanel.go, internal/tui/panelstops.go, internal/tui/blockcursor.go, internal/tui/model.go
**Read first:** internal/tui/sidepanel.go — sidePanelColumn, sidePanelContent, panelSections, panelSection; internal/tui/panelstops.go — panelStops; internal/tui/blockcursor.go — panelCursorRow; internal/tui/model.go — View (belowSlot); internal/tui/sidepanel_test.go — TestPanelStopsMatchClickTargets
**Tests:** new `TestPanelFrameStopsLandOnPaintedRows`, `TestPanelFrameOverlaidPaintsAtOverlayWidth`, `TestPanelSectionsPaintOncePerView` (reads the section-paint counter); keep `TestPanelStopsMatchClickTargets`, `TestOverlayLeavesTranscriptLinesByteIdentical`, `TestOverlayPanesRenderOncePerUpdateAndView` green.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestPanel|TestSidePanel|TestOverlay|TestNavMode' ./internal/tui/`
**Commit:** `refactor(tui): one panel frame per view for paint and stops`

## 9. One panel hit-test, no speculative cursor copies

**What:**
**Goal:** `panelFrame.at(x, y)` returns a `panelHit{cell, grip, grab, isPin, target}`; click (`handleSidePanelClick`) and hover (`pointerShapeAt`) ask it on `pre`, and the wheel (`foldMouseWheel`) asks geometry only; `enterPanelRegion` and `openPanelOverlay` read `panelFrameAs(true).stops()` instead of mutating a copy's cursor; `grep -nE 'entered\.(panelOverlaid|navPanelStops)\(' internal/tui/` prints nothing. Depends on item 8.
**Approach (assumed at the header base):** a `Model.panelSnap` (with a composed flag) set only by `withFrameSpans`, as `frameSpans` carries vertical geometry; never returned to Bubble Tea. `panelTargetAt` stays as a named delegate (ADR 0111 D4). Binding: a wheel notch never paints the sections; no cursor is parked while a decision is pending or while the column docks; nothing reads a frame resolved for a key after `followPanelTarget` re-lays the column.
**Regression guard.** The wheel path and the hover path never build a frame through `frameSpans()` on `m` — only from the snapshot `withFrameSpans` already composed on `pre`; `enterPanelRegion`/`openPanelOverlay` pass the row bound they already hold. The snap holds geometry only: `at(x, y)` builds the sections and stops only once `geom.cell(x, y)` holds, so a hover or click off the column paints no section. The Goal's grep replaces `grep -c 'cursor.inPanel()'`, which already prints 1 at base.
**Files:** internal/tui/panelframe.go, internal/tui/panelframe_test.go, internal/tui/sidepanel.go, internal/tui/mouse.go, internal/tui/pointershape.go, internal/tui/blockcursor.go, internal/tui/model.go
**Read first:** internal/tui/sidepanel.go — panelCell, panelTargetAt, openPanelOverlay; internal/tui/blockcursor.go — enterPanelRegion, navPanelStops; internal/tui/pointershape.go — hoverPointerShape; internal/tui/mouse.go — foldMouseWheel; internal/tui/model.go — withFrameSpans
**Tests:** new `TestPanelFrameAtAnswersCellGripPinTarget`, `TestBarePanelParksNoCursorWhileADecisionIsPending`; keep `TestSidePanelClickHoverShapeMatches`, `TestSidePanelPinCellIsNoGrip`, `TestOverlayHasNoGrip`, `TestOverlayInsideClickActsAsDocked`, `TestMouseMotionNeverRepaints`, `TestPointerShapeAt*` green.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestPanelFrame|TestBarePanel|TestSidePanel|TestOverlay|TestMouseMotion|TestPointerShapeAt|TestNavModeRightIntoEmptyPanel' ./internal/tui/`
**Closes:** panel-geometry-scattered
**Commit:** `refactor(tui): one panel hit-test answers click, hover and wheel`

## 10. Navigation mode over two region adapters

**What:**
**Goal:** `blockCursorKey` resolves the cursor once per key through `m.navigation()` (reseat, build the panel frame once, return a `navigation`); `grep -nE 'func \(m Model\) (movePanelCursor|panelCursorRunHead|activatePanelCursor|moveBlockCursor|cursorRunHead)\(' internal/tui/` prints nothing. Depends on item 9.
**Approach (assumed at the header base):** new `internal/tui/navmode.go`: a `navRegion` interface (`stops`, `nearest(row)`, `activate(m)`, `runHead(m)`, `entry(dir)`), implemented by `transcriptRegion` (over `cursorStops`/`toggleBlockAt`) and `panelRegion` (over `panelFrame.stops`); `navigation.key(msg) (Model, tea.Cmd, bool)` and `standingOn()`. Exactly two adapters (ADR 0111 D12). Panel ⏎ still goes through `followPanelTarget` (D4). `seatBlockCursor` stays at `refreshViewport` and calls the adapters. `navigation()` is pure.
**Regression guard.** `m.navigation()` builds the panel frame from a row bound obtained once per key, never by calling `frameSpans()` more than once per key; `TestNavigationResolvesPanelOncePerKey` also asserts the overlay panes render no more often than at the header base (this supersedes the report's reach-only frame build). The `^x` swap at runview.go:472 (`m.reseatPanelCursor().cursorRunHead()`) moves into this item so its `go build` holds; item 11 keeps the esc swap. The existing `navRegion` int type (`blockCursor.region`) is renamed (e.g. `cursorRegion`) in this item, so the interface does not redeclare it.
**Files:** internal/tui/navmode.go, internal/tui/navmode_test.go, internal/tui/blockcursor.go, internal/tui/blockcursor_test.go, internal/tui/sidepanel.go, internal/tui/runview.go, internal/tui/runview_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/blockcursor.go — blockCursorKey, navRegion (type), reseatPanelCursor, cursorRunHead; internal/tui/model.go — transcriptClaimants; internal/tui/runview.go — runViewKey; internal/tui/mouse_test.go — TestGroupHeaderFoldsItsGroupAlone; internal/tui/paintcache_test.go — TestOverlayPanesRenderOncePerUpdateAndView
**Tests:** new `TestNavigationWalksAFakeRegion`, `TestNavigationResolvesPanelOncePerKey` (also counts `paneRenders` against the header base); keep all `TestNavMode*`, `TestBlockCursor*`, `TestGroupHeaderFoldsItsGroupAlone`, `TestRunViewCtrlXStopsTheCursorsRowInsteadOfTheView`, `TestOverlayPanesRenderOncePerUpdateAndView` green.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestNavigation|TestNavMode|TestBlockCursor|TestGroupHeaderFolds|TestRunView|TestOverlayPanesRenderOncePerUpdateAndView' ./internal/tui/`
**Commit:** `refactor(tui): navigation mode walks two region adapters`

## 11. The run view asks the resolved navigation

**What:**
**Goal:** `runViewKey` (`internal/tui/runview.go`) asks `m.navigation().standingOn()` and `m.navigation().runHead()`; `grep -n reseatPanelCursor internal/tui/runview.go` prints nothing; `doc.go`'s navigation-mode paragraph names the region adapters. Depends on item 10.
**Approach (assumed at the header base):** replace the `m.reseatPanelCursor().cursor.inPanel()` (esc) and `m.reseatPanelCursor().cursorRunHead()` (`^x`) reads. Update prose naming the deleted verb pairs (rule: every comment or doc line naming `movePanelCursor`, `activatePanelCursor`, `panelCursorRunHead`, `reseatPanelCursor`, `moveBlockCursor`, `cursorRunHead`; `grep -rn` them over `internal/tui/` and `docs/`, historical records excepted).
**Regression guard.** The prose rule reaches `followPanelTarget`'s doc comment (sidepanel.go ≈:1328 names `[Model.activatePanelCursor]`), so `internal/tui/sidepanel.go` is in Files.
**Files:** internal/tui/runview.go, internal/tui/doc.go, internal/tui/blockcursor.go, internal/tui/panelstops.go, internal/tui/sidepanel.go
**Read first:** internal/tui/runview.go — runViewKey; internal/tui/doc.go — navigation-mode paragraph (blockcursor.go is that same reach); internal/tui/sidepanel.go — followPanelTarget (doc comment); internal/tui/panelstops.go — package comment; internal/tui/runview_test.go — TestRunViewCtrlXStopsTheCursorsRowInsteadOfTheView, TestRunViewEscGoesOneLevelUp
**Tests:** keep `TestRunViewCtrlXStopsTheCursorsRowInsteadOfTheView`, `TestRunViewEscGoesOneLevelUp`, `TestNavModeEscLeavesFromEitherRegion`, `TestNavModeCtrlXStopsPanelSubAgent`, `TestNavModeCtrlXInWatchModeRefused` green.
**Acceptance:**
- `go build ./...` and `go vet ./internal/tui/`
- `go test -race -count=1 -run 'TestRunView|TestNavMode' ./internal/tui/`
- the Goal's grep prints nothing.
**Closes:** nav-verbs-per-region
**Commit:** `refactor(tui): the run view asks the resolved navigation`
