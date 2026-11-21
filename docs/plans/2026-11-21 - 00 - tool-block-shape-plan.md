# Tool-block shape asked once — plan

**Goal:** one module in `internal/tui` answers "what shape is the tool block containing entry i" (kind, head, member position, end, group of one). Paint, fold (`blockFoldBy`), the panel reveal and mouse hit-testing ask it; no second walk restates the order or the group-of-one rule.
**Date:** 2026-11-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ae6879b2
**Closes:** tool-block-shape-twice

**Sources:**
- `.capcom/issues/tool-block-shape-twice.md`
- `docs/reviews/architecture-review-2026-11-21.html` §1
- ADR 0035 amendments 1118, 1120c (`capcom know show adr-0035-amend-1120c`)
- `docs/layout/tool-layout.md` (Rules, Vocabulary, Fold states)
- `docs/plans/archived/2026-11-20 - 00 - tool-group-of-one-plan.md` (NOTES on fold/paint drift)

**Ratified design calls (owner, 2026-11-21):**
- **Module home:** new `internal/tui/blockshape.go` holds the walk primitives (moved from `transcript.go`), the `blockShape` enum (moved from `paintcache.go`) and the shape answer.
- **Tests:** add a shape table and a paint/fold-agree table in `blockshape_test.go`; move only direct primitive asserts into rows; painted-line tests stay.

**Standing requirements:**
- skills: coding-standards
- Pure refactor: no painted line, fold, touch or config behaviour changes. Every existing test in `internal/tui` keeps passing unedited except where an item names it.
- Must keep ADR 0035 amendment 1120c (a)/(b): every call is a group of one; a lone delegation or skill fetch is `(1)`; task-list cards, answered `ask_user` records, targetless calls and workflow blocks stay out of every group.
- `blockFoldBy`'s rule table and the paint cache's resolved-fold key (`umbrellaFold`, `blockKey`) keep their shape.
- Bubble Tea v2 names; `Model` copied by value (ADR 0011).
- New file named in `doc.go`'s narration (`TestDocMapNamesEveryFile`).
- This box (AGENTS.local.md): every test run is `GOMEMLIMIT=2GiB go test -race -count=1 -run '<regex>' ./internal/tui/`, one package, one run at a time; never `-race` over the whole package or a pattern.

**Out of scope:**
- Fold gestures and setters (`fold-gesture-setters-leak`, review #6).
- Per-row grouping class in the tool registry (`tool-grouping-class-scattered`, review #7); `groupable`, `solo`, `out.Solo` stay as they are.
- Workflow stage framing (`subAgentSpan`, `subAgentFramed`, `retiredAttempts`) — paint concerns, not tool-block shape.
- Glossary or ADR changes; `docs/layout/` changes.

**Regression check (2026-11-21, ae6879b2):**
- 1: guard folded
- 2: guard folded
- 3: guard folded (writer's decision on the resumed-group head; the reviewer's "head stays the walk index" guard is replaced by it)
- 4: recast (writer's decision on the agreement asserts; remaining reviewer guards folded)
- 4 (re-check, second pass): guard folded — Goal rewritten to the guard's narrowed contract (the "replaces the Goal's" sentence deleted); Acceptance gains greps for the agreement test's presence and the retired direct `groupable`/`toolSuperGroup` asserts

## 1. Move the tool-block walk primitives into blockshape.go — ✅ DONE (2026-11-21)

**What:**
**Goal:** `internal/tui/blockshape.go` exists and defines `blockShape` and its constants, `toolRun`, `superGroup` (with `calls`), `toolSuperGroup`, `sameLabelRun`, `groupBlock`, `ownHeads`, `ownGroup`, `ownGroupAt` and `subAgentGroupAt`; none of them is defined in `transcript.go` or `paintcache.go`; `doc.go` names `blockshape.go`.
**Approach (assumed at the header base):** a pure move — cut each symbol with its doc comment from `transcript.go` (the symbols the Goal lists, plus `subAgentGroup` — not a range cut) and `blockShape` from `paintcache.go`, paste unchanged into `blockshape.go` with a file comment stating it is the one home of tool-block shape. No body, signature or name changes. Add `blockshape.go` to `doc.go`'s grouping narration.
**Regression guard.** The moved set is exactly the Goal's list plus `subAgentGroup` (`ownGroup`'s delegation form, which sits between `ownGroup` and `ownGroupAt`); in `transcript.go` the symbols between `ownHeads` and `ownGroup` — `backgroundSubAgentCall`, `subAgentHeads` and the entry run-head/workflow methods (`headsRun`, `opensRun`, `headsRunFor`, `headsWorkflow`, `headsWorkflowRuns`, `seatsItemHead`, `workflowItemHeads`, `carriesWorkflowSpend`, `workflowHeads`, `run`, `spawned`) — stay where they are; a range cut from `toolRun` to `subAgentGroupAt` would drag them into the one home of tool-block shape.
**Files:** internal/tui/blockshape.go; internal/tui/transcript.go; internal/tui/paintcache.go; internal/tui/doc.go
**Read first:** internal/tui/transcript.go — toolSuperGroup, ownGroup, subAgentGroup, ownGroupAt, subAgentGroupAt; internal/tui/paintcache.go — blockShape;
internal/tui/doc.go — grouping narration (toolSuperGroup / groupable); internal/tui/docmap_test.go — TestDocMapNamesEveryFile
**Tests:** none new; existing suites cover the moved code.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'DocMap|SuperGroup|Skill|SubAgentGroup|GroupOfOne|Render' ./internal/tui/`
- `grep -nE '^func (toolSuperGroup|sameLabelRun|ownGroup|ownGroupAt|subAgentGroupAt)\(' internal/tui/blockshape.go | wc -l` prints 5
**Commit:** `refactor(tui): move the tool-block walk primitives into blockshape.go`

## 2. Add the shape answer and make the fold side ask it

Depends on item 1.

**What:**
**Goal:** `blockshape.go` defines `toolBlockAt(entries []entry, i int) toolBlock`, answering from any entry index: `kind` (`shapeToolSuper`, `shapeSubAgentGroup`, `shapeSkillGroup`, else `shapeEntry`), `head`, `pos` (member index inside a Sub-agent/Skill group, else 0), `end` (exclusive, whole block), the umbrella's `sup` or the group's members, and methods `foldable()` (kind is one of the three groups) and `groupOfOne()` (umbrella of one call, or group of one member). `foldableHead`, `foldHeadOf` and `toolGroupAt` no longer exist; every fold-side caller asks `toolBlockAt`. `blockshape_test.go` holds a shape table.
**Approach (assumed at the header base):** `toolBlockAt` asks Sub-agent group → Skill group → umbrella, the order `foldableHead`/`resolveBlock` use today; for a member index it finds the head as `foldHeadOf` does (group → `group[0].at`; umbrella → walk back over `extendsGroupableStretch`, which moves too) and checks `i < end`. Rewire in `transcript.go`: `blockFoldBy` (the `foldByShared` arm fires only when `kind == shapeToolSuper`), `umbrellaIsLarge`, `closeTurn`, `markBlockTouched`, `openBlockAlone`, and `revealBlockMember` (`kind != shapeToolSuper || groupOfOne()` replaces `sup.calls() < 2`). Delete the "in render.go's resolveBlock order" comment. Rewrite the tests that call the deleted functions (`settledAt` in autocollapse_test.go, autocollapse_turnboundary_test.go, `TestUmbrellaHeadOfFindsTheHeadFromAnyMember`) against `toolBlockAt`, keeping each assertion's meaning.
**Regression guard.** `blockFoldBy`, `openBlockAlone` and `umbrellaIsLarge` answer `foldByNone`/false unless `toolBlockAt(i).head == i` — today's head-only contract: paint asks `blockFolded` at a group's resumed block (render.go `resolveGroup`, pos>0 after an open member), where `foldableHead` answers false, and a foldable()-only check would paint a second folded "✦ Skill (N)" header there. Every comment naming `foldableHead`, `foldHeadOf` or `toolGroupAt` (transcript.go's `[foldableHead]`/`[foldHeadOf]` doc links, render.go `resolveGroup`'s "(foldableHead)") is repointed to `toolBlockAt`.
**Files:** internal/tui/blockshape.go; internal/tui/blockshape_test.go; internal/tui/transcript.go; internal/tui/render.go; internal/tui/autocollapse_test.go; internal/tui/autocollapse_turnboundary_test.go; internal/tui/autocollapse_touch_test.go
**Read first:** internal/tui/transcript.go — blockFoldBy, foldableHead, foldHeadOf, toolGroupAt, revealBlockMember; internal/tui/render.go — resolveGroup;
internal/tui/autocollapse_test.go — settledAt; internal/tui/autocollapse_touch_test.go — TestUmbrellaHeadOfFindsTheHeadFromAnyMember
**Tests:** `TestToolBlockShapes` in `blockshape_test.go`, a table of `{name, build, at, want toolBlock-fields}` asked at the head AND at every member: lone groupable call (group of one); two runs (umbrella, not group of one); lone `sub_agent` and lone `load_skill` (`(1)` groups, group of one); a two-member Sub-agent group asked at pos 1; skill group between two umbrellas; narration and depth change as breakers; task-list card, answered `ask_user`, targetless call, workflow `finish`, `fan_out`, background sub-agent, workflow block — each `shapeEntry`, not foldable. A row asserts `blockFoldBy` at a pos-1 group member answers `foldByNone`.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ToolBlockShapes|AutoCollapse|Umbrella|GroupOfOne|Fold|Reveal|PanelJump|TurnBoundary' ./internal/tui/`
- `grep -nE '\b(foldableHead|foldHeadOf|toolGroupAt)\b' internal/tui/*.go` prints nothing
- `grep -c '^func TestToolBlockShapes(' internal/tui/blockshape_test.go` prints 1
**Commit:** `refactor(tui): answer a tool block's shape once for fold and reveal`

## 3. Make paint ask the shape answer

Depends on item 2.

**What:**
**Goal:** `resolveBlock` takes its tool-block kind, head, member position and group-of-one fact from `toolBlockAt`; `renderSuperGroup` paints the call's own block under the header from that group-of-one fact; neither `render.go` nor `toolblock.go` calls `subAgentGroupAt`, `ownGroupAt` or `toolSuperGroup` to decide a block's shape, and `render.go` does not name `loadSkillToolName`.
**Approach (assumed at the header base):** in `resolveBlock`, call `toolBlockAt(t.entries, head)` once and switch on `kind`: the two group kinds go to `resolveGroup` (passing the `toolBlock` instead of re-asking `subAgentGroupAt`/`ownGroupAt`); `shapeToolSuper` keeps `next = head + sup.calls()` and its `umbrellaFold`; `shapeEntry` keeps today's workflow run-framing (`subAgentSpan` + `subAgentFramed` → `shapeSubAgentRun`) and the `entryWorkflow` stage span — these are paint concerns and stay in render.go. Pass `groupOfOne()` into `renderSuperGroup` in place of the `len(runs) == 1 && len(runs[0].views) == 1` check. `resolveGroup`'s `pos>0` continuation, open-member `next`, and whole-group key cover are unchanged. mouse.go needs no edit: hit-testing reaches the shape through `blockFoldBy` and the targets paint emits.
**Regression guard.** resolveBlock's walk can land on a resumed Sub-agent/Skill group block (a member index, pos>0) — paint asks toolBlockAt at the walk index itself and takes head and pos from the answer, never assuming the walk index is the head; the pos>0 headerless continuation is unchanged. `toolBlockAt` allocates nothing beyond the group/superGroup slices today's calls already build (it now runs on every block, cache hit or miss). The no-call clause is scoped to `render.go` and `toolblock.go` (transcript.go's `inFlightFanOut`/`announcedMembers` keep calling `subAgentGroupAt`). Every `render.go`/`paintcache.go` comment naming `subAgentGroupAt`, `ownGroupAt`, `toolSuperGroup` or `foldableHead` as resolveBlock's question names `toolBlockAt`.
**Files:** internal/tui/render.go; internal/tui/toolblock.go; internal/tui/paintcache.go
**Read first:** internal/tui/render.go — transcript.resolveBlock, transcript.resolveGroup, transcript.renderView; internal/tui/toolblock.go — renderSuperGroup, superRunViews;
internal/tui/transcript.go — blockFolded, blockFoldBy; internal/tui/paintcache_test.go — TestAllHitRepaintAllocatesLittlePerBlock
**Tests:** none new; the painted-line suites and `TestAllHitRepaintAllocatesLittlePerBlock` are the guard.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Render|SuperGroup|Skill|SubAgent|GroupOfOne|BlockMarks|Mouse|PaintCache|Workflow|AllHitRepaint' ./internal/tui/`
- `grep -n 'loadSkillToolName' internal/tui/render.go` prints nothing
- `grep -nE '\b(subAgentGroupAt|ownGroupAt|toolSuperGroup)\(' internal/tui/render.go internal/tui/toolblock.go` prints nothing
- `grep -nE '\b(subAgentGroupAt|ownGroupAt|foldableHead)\b' internal/tui/render.go` prints nothing
**Commit:** `refactor(tui): paint a tool block from its one shape answer`

## 4. Pin paint and fold to the shape answer with an agreement table

Depends on item 3.

**What:**
Recast at the regression check (2026-11-21).
**Goal:** `blockshape_test.go` holds `TestPaintAndFoldAgreeWithTheBlockShape`: for every transcript in the shape table, at each walk index i `renderView` reaches, the painted shape equals `toolBlockAt(i).kind` (`shapeSubAgentRun` maps to `shapeEntry`); `toolBlockAt(i).head == i` at pos 0 and `head < i` at pos > 0 (a resumed group block); painted `next <= end` for foldable kinds only (for `shapeEntry`, `next` is `i+1` or `i+1+subAgentSpan(i)`); head agreement holds at each group member's `at` index and each umbrella call index; and at pos 0 `blockFoldBy(i) != foldByNone` exactly when `foldable()`, while a pos > 0 walk index answers `foldByNone`. The direct `groupable`/`toolSuperGroup` asserts in `TestSkillFetchesNeverJoinTheToolsSuperGroup` (toolshape_test.go) are rows of `TestToolBlockShapes`.
**Approach (assumed at the header base):** share the shape table's builders between both tests; reach heads with a test helper that steps `resolveBlock` exactly as `renderView` does (skipping `retiredAttempts`). Move, never drop, each direct-primitive assertion.
**Regression guard.** The agreement test asserts at each walk index i that the painted shape equals toolBlockAt(i).kind, that toolBlockAt(i).head == i when pos == 0 and head < i when pos > 0 (resumed group block), and asserts head agreement only at each group member's `at` index and each umbrella call index — never at entries inside a member's span, which item 2's toolBlockAt answers by its own rule. The painted `shapeSubAgentRun` maps to `shapeEntry` (a Workflow item head with a run); `next <= end` is asserted only for foldable kinds, and for `shapeEntry` `next` is `i+1` or `i+1+subAgentSpan(i)`. The fold clause holds at pos==0 heads; a pos>0 walk index answers `foldByNone`. The moved `TestSkillFetchesNeverJoinTheToolsSuperGroup` rows carry a field read straight from `toolSuperGroup(entries, at)`/`groupable` at `at` (the skill group is asked before the umbrella, so `kind` alone would not see a groupable fetch).
**Files:** internal/tui/blockshape_test.go; internal/tui/toolshape_test.go
**Read first:** internal/tui/render.go — transcript.renderView, transcript.resolveBlock, transcript.resolveGroup; internal/tui/transcript.go — blockFoldBy, toolSuperGroup;
internal/tui/subagentblock.go — subAgentSpan; internal/tui/workflowblock.go — retiredAttempts; internal/tui/toolshape_test.go — TestSkillFetchesNeverJoinTheToolsSuperGroup
**Tests:** as Goal.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ToolBlockShapes|PaintAndFoldAgree|SkillFetches|Render' ./internal/tui/`
- `grep -c '^func TestPaintAndFoldAgreeWithTheBlockShape(' internal/tui/blockshape_test.go` prints 1
- `grep -nE '\b(groupable|toolSuperGroup)\(' internal/tui/toolshape_test.go` prints nothing
**Commit:** `test(tui): pin paint and fold to the tool-block shape table`
