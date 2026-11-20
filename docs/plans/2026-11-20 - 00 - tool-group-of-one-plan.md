# Every tool call is a tool group — plan

**Goal:** every tool call renders as a tool group — a lone call is a group of one that folds, opens and re-folds exactly like a multi-call group — and the Group summary on a group header paints in the tools header colour. Lands the in-tree turn-boundary auto-collapse work first.
**Date:** 2026-11-20
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0b220a4eb
**Closes:** lone-call-no-refold

**Regression check (2026-11-20, 0b220a4eb):**
- 1: guard folded (CHANGELOG.md and the untracked issue file named in Files)
- 2: guard folded (star test amended; doc anchor reworded to the Group summary bullet; "Width fallback" cited as a bullet)
- 3: recast (absorbs the lone-call test ports item 4 held; floor file is transcript.go; ends `go test ./internal/tui/` green)
- 4: recast (deletes dead code and prose only; applies the owner's touched-group-grows rule, superseding `adr-0035-amend-1120b` (b))
- 5: guard folded (owner decision: ends green on its own package, ports every frame test reading a lone delegation or skill fetch)
- 6: guard folded (owner decision: owns every `cmd/apogee` assertion the group-of-one shape moved; ends green)
- 7: guard folded (sessions.md, lone-wording acceptance, supersession of `adr-0035-amend-1120b` (b), Width fallback cited as a bullet)
- Re-check round (2026-11-20, 0b220a4eb):
- 3: guard folded (owns the touched-group-grows test rewrites and `foldTouched` assertions; Files take every non-doc.go file the sweep hits; defect history moved from the Goal to the What)
- 4: guard folded (test work is rename-only; Acceptance adds the `lone` grep over the two autocollapse test files)
- 7: guard folded (sweep and Files also cover `internal/config/defaults/config.yaml`; Acceptance adds `go test ./internal/config/`)

**Sources:**
- `docs/layout/tool-layout.md` §"Fold states and interaction", §"Width fallback"
- `capcom know show adr-0035-amend-1118`, `adr-0035-amend-1120`, `adr-0035-amend-1120b` (in-tree, uncommitted at base)
- `capcom know show "Group summary"`; `capcom show lone-call-no-refold`
- `docs/design/test-drivers.md` (driving the TUI, stubllm scripts)

**Ratified design calls (owner, 2026-11-20):**
- **Scope:** every call is a group of one — groupable calls, a lone delegation (`Sub-agents (1)`) and a lone skill fetch (`Skill (1)`).
- **Open shape:** an open group of one paints its group header and, directly under it, the call's own block (header, target, body/diff/output as a standalone block shows them today) — no type row.
- **Auto-collapse off:** a lone call renders as an open group of one, folded and opened by click / ⏎ on its header.
- **Colour:** the Group summary phrase, its clipped remainder and the diffstat counts' base paint in `th.toolLabel` (bold + `tool-header` colour); `th.toolHeader` itself is not restyled.
- **Touched group grows:** a group the reader opened stays as the reader left it — open — when the next Turn's call grows it, and is never auto-folded again; the reader folds it by click.
- **Out of the group:** task-list cards, an answered `ask_user` record and a call with no target stay ungrouped (writer's call: no tool block to fold / no phrase source).

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); `Model` copied by value (ADR 0011).
- Tests needing a model script `internal/stubllm`; TUI claims go through `internal/tuitest` (`adr-0062-amend-1118`).
- `make test`, never a raw multi-package `go test`; per-item Acceptance runs one package at a time.

**Out of scope:**
- `graphics/demo/storyboards/hero.yaml` (in-tree, unrelated owner work — never staged or committed).
- Restyling `th.toolHeader`, the Sub-agent/Skill group header paint (already `toolLabel`), task-list fold.
- Any engine, config-key or session-record change.

## 1. Land the in-tree turn-boundary auto-collapse work — ✅ ALREADY-DONE (159d842c6)

**What:**
**Goal:** the working tree's uncommitted turn-boundary auto-collapse change (a settled block folds when the next Turn of its run starts, `adr-0035-amend-1120b`) is committed as one commit, and its targeted tests pass.
**Approach (assumed at the header base):** the change is already written: `entry.turnClosed`, `entry.heldForApproval`, `closeTurn`, `holdForApproval`, `waitsOnUser`, the sticky rule in `blockSettled` (`internal/tui/transcript.go`), its unit test `internal/tui/autocollapse_turnboundary_test.go`, the e2e in `cmd/apogee/e2e_toolsautocollapse_test.go` with `cmd/apogee/testdata/stubllm/toolsautocollapse.yaml`, and its docs. Verify, fix only what fails, and commit exactly the files below — including the in-tree `CHANGELOG.md` hunk (no sidecar entry for this item). Never stage `graphics/demo/storyboards/hero.yaml`.
**Regression guard.** The in-tree `CHANGELOG.md` hunk and the untracked `.capcom/issues/lone-call-no-refold.md` (its `plan:` line already set) are part of this commit, by rule: stage every file below plus the in-tree `CHANGELOG.md` hunk (kept off `**Files:**` by plan rule); `graphics/` stays out.
**Files:** internal/tui/transcript.go; internal/tui/model.go; internal/tui/doc.go; internal/tui/fold_test.go; internal/tui/autocollapse_turnboundary_test.go; cmd/apogee/e2e_toolsautocollapse_test.go; cmd/apogee/testdata/stubllm/toolsautocollapse.yaml; docs/layout/tool-layout.md; docs/manual/configuration.md; layout.md; .capcom/knowledge/decisions/adr-0035.md; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120b.md; .capcom/knowledge/glossary/glossary-context/group-summary.md; .capcom/issues/lone-call-no-refold.md
**Read first:** internal/tui/transcript.go — entry.turnClosed, heldForApproval, closeTurn, holdForApproval, waitsOnUser, blockSettled; internal/tui/autocollapse_turnboundary_test.go — TestAutoCollapseFoldsWhenTheNextTurnStarts; cmd/apogee/e2e_toolsautocollapse_test.go — TestE2EToolsAutoCollapse*; cmd/apogee/testdata/stubllm/toolsautocollapse.yaml; internal/tui/fold_test.go; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120b.md
**Tests:** existing — `TestAutoCollapseFoldsWhenTheNextTurnStarts`, `TestE2EToolsAutoCollapse*`.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -race -count=1 -run 'AutoCollapse|Fold|TurnBoundary' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EToolsAutoCollapse|TestE2EToolsFold' ./cmd/apogee/`
- `capcom check`
- `git show --stat HEAD` names no `graphics/` path and `git status --short` lists neither `CHANGELOG.md` nor `.capcom/issues/lone-call-no-refold.md`.
**Commit:** `feat(tui): fold a settled tool block when the next Turn of its run starts`

## 2. Paint the Group summary in the tools header colour

**What:**
**Goal:** on a `✦` group header the Group summary phrases, their clipped remainder and the diffstat counts' base foreground paint in `th.toolLabel` (bold, `tool-header` scheme role); the `✦`, dot leaders, `▶`/`▼` and failure outcome keep their current styles.
**Approach (assumed at the header base):** in `internal/tui/toolblock.go`, `paintGroupSegment` / `umbrellaHeaderPaint` use `th.toolHeader` (no foreground) for the phrase `default` arm and the remainder, and `diffAdded`/`diffRemoved` `.Inherit(th.toolHeader)`; switch those to `th.toolLabel`. Leave `th.toolHeader` (also `hangingWrap`'s wrap style and the session-log header) untouched. Add one sentence to the `docs/layout/tool-layout.md` "Group summary" bullet (and its "Width fallback" sub-bullet, a bullet inside that section, not a heading) saying the phrases, their remainder and the diffstat base paint in the tools header colour (bold); no existing "own tone" passage is there to reword.
**Regression guard.** `TestToolBlockStarPaintsInTheToolsHeaderColour` (`toolblock_test.go`, umbrella `after:` case) is amended: the `Read 2 files` phrase asserts `th.toolLabel`; the leading gap and marker stay `th.toolHeader` (`toolblock.go` ~310-312).
**Files:** internal/tui/toolblock.go; internal/tui/toolblock_test.go; docs/layout/tool-layout.md
**Read first:** internal/tui/toolblock.go — umbrellaHeaderPaint, paintGroupSegment, umbrellaCountPaint; internal/tui/toolblock_test.go — TestUmbrellaHeader_ShowsTheGroupSummary, TestToolBlockStarPaintsInTheToolsHeaderColour; internal/tui/theme.go — toolHeader, toolLabel; docs/layout/tool-layout.md — Group summary, Width fallback bullet; internal/tui/groupsummary_test.go
**Tests:** amend `TestUmbrellaHeader_ShowsTheGroupSummary` subtests ("paints the phrases in the header's tone…", "paints a failure count red…", "paints the diffstat's halves…") to assert `toolLabel`'s foreground and bold on the phrases; also amend `TestToolBlockStarPaintsInTheToolsHeaderColour`'s umbrella case; add a case that the clipped remainder paints `toolLabel`.
**Acceptance:**
- `go test -race -count=1 -run 'UmbrellaHeader|ToolBlockStar|GroupSummary' ./internal/tui/`
**Commit:** `feat(tui): paint the Group summary in the tools header colour`

## 3. A lone groupable call is a group of one that re-folds

**What:**
Recast at the regression check (2026-11-20). Fixes the defect from the lone auto-collapse change (`9d509e86d`-era, `adr-0035-amend-1120`): a lone call the reader opened could never be folded again.
**Goal:** a single groupable call (Read, Edit, Write, Terminal, Grep, a pending `ask_user`, …) renders as a `✦` group header with its Group summary (`Read 1 file`); open, the call's own block paints directly under that header with no type row; click and ⏎ on the header fold and open it in every state — settled, touched, live, auto-collapse on or off.
**Approach (assumed at the header base):** drop the `runs.calls() < 2` floor in `toolSuperGroup` (`internal/tui/transcript.go`) to 1, so `foldableHead` sees an umbrella; teach `renderSuperGroup` (`internal/tui/render.go`, `toolblock.go`) a one-call open shape that paints the member's standalone block (`renderToolBlock`) under the header in place of the type row, live verb on the header while the call runs; `blockFoldBy` and `Model.toggleBlockAt` (`mouse.go`) then treat it as any umbrella. Keep the paint-cache key and the block-cursor levels (`blocktarget.go`) consistent for the one-call shape: the header is one cursor stop, the call's own header (body toggle) the next. Update every doc comment that says a lone call has no fold but the settled one (rule: `rg -n 'LONE|lone call' internal/tui/*.go`).
**Regression guard.** Items 3 and 4 are not separable as written (owner, 2026-11-20): the floor change makes `loneFoldable`/`loneTouched` unreachable, so this item ports every lone-call test its floor change breaks (`autocollapse_test.go`, `autocollapse_touch_test.go`, `autocollapse_turnboundary_test.go` cases) to the group-of-one shape and ends with `go test ./internal/tui/` green; item 4 only deletes the dead code, comments and names. The touched-group-grows rule takes effect with the floor change, so this item owns the rewrites to it: `autocollapse_touch_test.go` (~351, `TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched`) and `autocollapse_turnboundary_test.go` (~305-330) assert the head's `foldTouched` and an open group after settle, and the `loneTouched` assertions (`autocollapse_touch_test.go` ~108, ~334, ~387; turnboundary ~312) move to `foldTouched`. This reverses `adr-0035-amend-1120b` (b) here, not at item 4.
**Files:** internal/tui/render.go; internal/tui/toolblock.go; internal/tui/mouse.go; internal/tui/blocktarget.go; internal/tui/transcript.go; plus every non-doc.go file `rg -n 'LONE|lone call' internal/tui/*.go` hits (e.g. paintcache.go, toolbranch.go, settingsapply.go, toolleader.go; doc.go stays item 7's); internal/tui/transcript_test.go; internal/tui/groupofone_test.go; internal/tui/autocollapse_test.go; internal/tui/autocollapse_touch_test.go; internal/tui/autocollapse_turnboundary_test.go
**Read first:** internal/tui/transcript.go — toolSuperGroup, foldableHead, blockFoldBy, blockSettled, foldHeadOf, openBlockAlone, revealBlockMember, closeTurn; internal/tui/render.go — resolveBlock, resolveFoldedCall; internal/tui/toolblock.go — renderSuperGroup, superRunViews, umbrellaHeader; internal/tui/mouse.go — toggleBlockAt (targetUmbrella and targetHeader arms); internal/tui/paintcache.go — blockKey, spanFlags; internal/tui/autocollapse_touch_test.go — autoCollapseLoneModel, assertLoneCallOpen, TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched; internal/tui/transcript_test.go — TestTranscriptSuperGroupFormsLiveAndGrows, toolSuperGroup table (~2883)
**Tests:** new `internal/tui/groupofone_test.go` via `internal/tuitest`: lone Read settles → folded header; click opens (call block visible, no type row); click folds again; same with ⏎; same with auto-collapse off (opens by default, folds on click); live lone call shows the live verb; a second call of the next Turn grows the group to two with type rows. Amend `TestTranscriptSuperGroupFormsLiveAndGrows` ("a lone call is no umbrella") and the `toolSuperGroup` table. Port every lone-call test the floor change breaks to the group-of-one shape, including the touched-group-grows rewrites named above; fix any other `internal/tui` test this breaks.
**Acceptance:**
- `go test -race -count=1 -run 'GroupOfOne|SuperGroup|AutoCollapse|Fold|Umbrella|Mouse' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/`
**Commit:** `fix(tui): make a lone tool call a group of one that folds again`

## 4. Retire the lone-call fold path

Depends on item 3.

**What:**
Recast at the regression check (2026-11-20).
**Goal:** no lone-call-specific fold state or shape remains: one touched mark and one settled rule serve every group, including a group of one and a group that grows from one to two.
**Approach (assumed at the header base):** delete `loneFoldable`, `entry.loneTouched`, the `touchedMark` split and `resolveFoldedCall` (`internal/tui/transcript.go`, `render.go`); the head's `foldTouched` carries over as a group grows. Delete the now-dead `!e.loneTouched` exception in `blockSettled` and its "opened closing member" handling rather than porting it. Item 3 already ported the lone-call tests; this item deletes only the dead code, comments and names.
**Regression guard.** Ratified rule (owner, 2026-11-20): a group the reader opened stays open when the next Turn's call grows it, and is never auto-folded again; the reader folds it by click. This supersedes `adr-0035-amend-1120b` (b)'s "The umbrella stays open until it settles by the earlier rule" and the archived settled-tool-blocks-fold plan's note that a touched lone call that grows heads an untouched umbrella; name it in tool-layout.md and `doc.go` prose (item 7). Rewrite `TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched` to assert it: a touched group of one grows to two and stays open after it settles. Retire `loneTouched`/`loneFoldable`/`resolveFoldedCall` from comments and tests too. Item 3 already rewrote that test and the `loneTouched` assertions, so this item's test work is rename-only; the `!e.loneTouched` deletion in `blockSettled` is safe because nothing sets the field after item 3.
**Files:** internal/tui/transcript.go; internal/tui/render.go; internal/tui/mouse.go; internal/tui/autocollapse_test.go; internal/tui/autocollapse_touch_test.go; internal/tui/autocollapse_turnboundary_test.go
**Read first:** internal/tui/transcript.go — blockFoldBy, blockSettled, touchedMark, markBlockTouched, openBlockAlone, loneFoldable, entry.loneTouched; internal/tui/render.go — resolveBlock, resolveFoldedCall; internal/tui/mouse.go — toggleBlockAt targetUmbrella arm; internal/tui/autocollapse_touch_test.go — TestAutoCollapseTouchWritersMoveTheGeneration, TestAutoCollapseTouchOpensALoneCall, TestAutoCollapsePanelJumpRevealsAFoldedLoneCall; internal/tui/autocollapse_test.go — TestAutoCollapseFoldsALoneCall, loneCallFoldedRow; internal/tui/autocollapse_turnboundary_test.go — "a lone call the reader opened stays open" subtest
**Tests:** rename-only (item 3 rewrote `TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched` to the touched-group-grows rule); rename the ported lone-call tests and helpers so no `lone…` name from the retired path remains.
**Acceptance:**
- `! rg -n 'loneTouched|loneFoldable|resolveFoldedCall' internal/` (comments and tests included)
- `! rg -n -i 'lone' internal/tui/autocollapse_test.go internal/tui/autocollapse_touch_test.go` (the turnboundary file's lone delegation / skill fetch cases belong to item 5)
- `go test -race -count=1 ./internal/tui/`
**Commit:** `refactor(tui): retire the lone-call fold path`

## 5. A lone delegation and a lone skill fetch are groups of one

Depends on item 4.

**What:**
**Goal:** a lone `sub_agent`/`fan_out` delegation renders as a `Sub-agents (1)` group and a lone `load_skill` fetch as a `Skill (1)` group; each folds, opens and re-folds like any group, auto-collapses when settled, and open shows its member row directly. The run-view click, wave hints, side-panel jump and run-view root header still reach the member.
**Approach (assumed at the header base):** drop `ownGroup`'s `len(group) < 2` floor (`internal/tui/transcript.go`, reached via `toolGroupAt` → `subAgentGroupAt` / `ownGroupAt`). Check each bare-row consumer: run-view click on the delegation row, `announceWave`, `revealBlockMember` (side panel opens a folded group to its member), the run-view root header. The turn boundary now folds a lone delegation (`closeTurn`): when the next Turn's delegation joins, the group stays folded and its header shows the grown count, per `adr-0035-amend-1120b` (b), unless a member waits on the user.
**Regression guard.** This item ends with `go test -race -count=1 ./internal/tui/` green on its own, porting every frame test that reads a lone delegation or skill-fetch row after settle (a lone settled delegation or skill fetch now folds under default config); find them with `rg -n '"✦ Sub-agent"|heads no group|floor of (two|2)' internal/tui/*_test.go`.
**Files:** internal/tui/transcript.go; internal/tui/subagentblock.go; internal/tui/mouse.go; internal/tui/subagentblock_test.go; internal/tui/toolshape_test.go; internal/tui/mouse_test.go; internal/tui/transcript_test.go; internal/tui/blocktarget_test.go; internal/tui/model_test.go; internal/tui/fanout_test.go
**Read first:** internal/tui/transcript.go — ownGroup, ownGroupAt, toolGroupAt, foldableHead, closeTurn, revealBlockMember, inFlightFanOut, announcedMembers; internal/tui/render.go — resolveBlock, resolveGroup; internal/tui/subagentblock.go — subAgentFramed, renderSubAgentGroup; internal/tui/subagentblock_test.go — TestLoneSubAgentRunWearsTheGroupMembersRow, loneDelegation; internal/tui/transcript_test.go — subAgentGroup/ownGroup tables; internal/tui/model_test.go — fanOutOf, TestFanOutCountIgnoresADelegatesGroup
**Tests:** amend `TestLoneSubAgentRunWearsTheGroupMembersRow` and the skill shapes in `toolshape_test.go`; add: lone delegation folds when settled, click opens, click folds; side-panel jump to a folded lone delegation opens it; next Turn's delegation grows a folded `Sub-agents (1)` to `(2)` folded.
**Acceptance:**
- `go test -race -count=1 -run 'SubAgent|Skill|Group|AutoCollapse|Panel' ./internal/tui/`
- `go test -race -count=1 ./internal/tui/`
**Commit:** `feat(tui): make a lone delegation and a lone skill fetch groups of one`

## 6. Drive the group of one end to end

Depends on items 2, 3 and 5.

**What:**
**Goal:** `cmd/apogee` e2e tests drive the shipped binary through a stubllm script and prove: a lone call folds when the next Turn starts, a click opens it, a second click folds it again; a lone delegation renders `Sub-agents (1)` and folds; with auto-collapse off a lone call shows an open group header that folds on click. Every existing `cmd/apogee` test passes against the group-of-one shape.
**Approach (assumed at the header base):** extend `cmd/apogee/testdata/stubllm/toolsautocollapse.yaml` and `cmd/apogee/e2e_toolsautocollapse_test.go` (`TestE2EToolsAutoCollapseFoldsALoneCall` is the model); amend `e2e_toolsfold_test.go` / `e2e_toolsfold_subagent_test.go` and any other `cmd/apogee` test whose frame assertion assumed a bare lone block.
**Regression guard.** This item owns every `cmd/apogee` frame/e2e assertion the group-of-one shape (items 3-5) moved, and ends with `go test -race -count=1 ./cmd/apogee/` green: the golden frames pinning a lone delegation (`t04-step-cap-block.txt`, `t15-cancelled-delegation.txt`, `workflow-stage-view.txt` to be checked) and every `e2e_*_test.go` found by `rg -ln 'sub_agent|load_skill' cmd/apogee/e2e_*_test.go` (e.g. `e2e_delegation_test.go`, `e2e_subagent_view_test.go`) whose assertion reads a lone block.
**Files:** cmd/apogee/e2e_toolsautocollapse_test.go; cmd/apogee/testdata/stubllm/toolsautocollapse.yaml; cmd/apogee/e2e_toolsfold_test.go; cmd/apogee/e2e_toolsfold_subagent_test.go; cmd/apogee/testdata/frames/*.txt; plus every other cmd/apogee/e2e_*_test.go the rule above finds
**Read first:** cmd/apogee/e2e_toolsautocollapse_test.go — TestE2EToolsAutoCollapseFoldsALoneCall, FoldsASubAgentGroup; cmd/apogee/testdata/stubllm/toolsautocollapse.yaml; cmd/apogee/e2e_toolsfold_test.go; cmd/apogee/e2e_toolsfold_subagent_test.go; cmd/apogee/e2e_subagent_view_test.go — openWorkingRun; cmd/apogee/testdata/frames/t04-step-cap-block.txt
**Tests:** new `TestE2EToolsAutoCollapseRefoldsALoneCall`, `TestE2EToolsAutoCollapseFoldsALoneDelegation`, `TestE2EToolsFoldLoneCallWithAutoCollapseOff`.
**Acceptance:**
- `go test -race -count=1 -run 'TestE2ETools' ./cmd/apogee/`
- `go test -race -count=1 ./cmd/apogee/`
**Commit:** `test(e2e): drive a lone call and a lone delegation as groups of one`

## 7. Document the group of one

Depends on items 2–5.

**What:**
**Goal:** the spec, manual, glossary and decision record describe every call as a tool group: a group of one's folded and open shapes, its re-fold, the auto-collapse-off shape, `Sub-agents (1)` / `Skill (1)`, and the Group summary colour; no public doc or Note still says a lone call, lone delegation or lone skill fetch is not a group or cannot be folded.
**Approach (assumed at the header base):** add `adr-0035-amend-1120c` (`capcom know add --id adr-0035-amend-1120c --parent adr-0035`) recording the four ratified calls and superseding `adr-0035-amend-1120` and `-1120b`'s lone-call / lone-delegation sentences; name it on `adr-0035`'s Status line and reading-order list. Rewrite `docs/layout/tool-layout.md` §"Single tool folded", §"Single tool collapsed/expanded", §"Grouped Sub-agents/Skills", §"Fold states and interaction"; `layout.md`; `docs/manual/configuration.md` (`ui.tools-auto-collapse`); the `Group summary` glossary Note; `internal/tui/doc.go`. Rule for the sweep: every passage naming a lone call/delegation/skill fetch or a group's two-call floor (`rg -n -i 'lone|two or more|at least two|≥ ?2' docs/layout layout.md docs/manual .capcom/knowledge/glossary internal/tui/doc.go internal/config/defaults/config.yaml`).
**Regression guard.** `adr-0035-amend-1120c`, `tool-layout.md` and `doc.go` state the supersession of `adr-0035-amend-1120b` (b)'s "The umbrella stays open until it settles by the earlier rule" by the ratified touched-group-grows rule (a group the reader opened stays open when the next Turn's call grows it; the reader folds it by click). The sweep also covers `docs/manual/sessions.md` (its "lone tool call" fold paragraph) and the shipped default config comments in `internal/config/defaults/config.yaml` (the `ui.tools-auto-collapse` text naming "a lone tool call" / "a lone call open"); `tool-layout.md`'s "Width fallback" is a bullet inside "Group summary", cited as such.
**Files:** .capcom/knowledge/decisions/adr-0035.md; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120c.md; docs/layout/tool-layout.md; layout.md; docs/manual/configuration.md; docs/manual/sessions.md; internal/config/defaults/config.yaml; .capcom/knowledge/glossary/glossary-context/group-summary.md; internal/tui/doc.go; plus every file the sweep rg hits
**Read first:** docs/layout/tool-layout.md — Fold states and interaction, Single tool folded, Grouped Sub-agents, Grouped Skills; layout.md — tool-block fold paragraphs; docs/manual/configuration.md — ui.tools-auto-collapse; docs/manual/sessions.md — fold paragraph; .capcom/knowledge/glossary/glossary-context/group-summary.md; internal/tui/doc.go; .capcom/knowledge/decisions/adr-0035.md — Status line, reading list
**Tests:** docs-only; existing doc drift tests.
**Acceptance:**
- `capcom check`
- `capcom know show adr-0035 | rg -q '1120c'`
- `! rg -n -i -w 'lone' docs/layout layout.md docs/manual/sessions.md docs/manual/configuration.md internal/config/defaults/config.yaml .capcom/knowledge/glossary/glossary-context/group-summary.md internal/tui/doc.go` (deliberate hits allowed, each justified)
- `go test -race -count=1 -run 'Doc|Drift' ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 ./internal/config/`
**Commit:** `docs(tools): specify every tool call as a tool group`
