# Settled tool blocks fold — plan

**Goal:** Under `ui.tools-auto-collapse`, every settled, untouched tool surface folds to one line — the `✦ Tools` umbrella (shipped), a lone groupable tool call, the `✦ Sub-agents (N)` group and the `✦ Skill (N)` group — and every tool block's leading `✦` paints in the tools header colour.
**Date:** 2026-11-20
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7ff477903
**Closes:** lone-tools-never-fold; settled-group-collapse-scope

**Regression check (2026-11-20, 7ff477903):**
- 1: guard folded (the umbrella-header test's star; the `⟳` glyph override keeps its tone)
- 2: guard folded (writer decision: `blockSettled` walks the block's own members only; Files widened to every file naming the old identifiers)
- 3: guard folded (whole `cmd/apogee` e2e sweep; a lone head folds only on `foldBySettled`; a lone call's touch never carries into an umbrella)
- 4: guard folded (settled arm gated by shape until item 5; every group-header pin in Files; whole `internal/tui` run)
- 5: guard folded (writer decision: group member done-ness; e2e sweep over every adjacent `sub_agent` / `load_skill` pair)
- 5: guard folded (writer decision: opening a framed member's run view via `openRunAt` marks its group touched)
- 6: guard folded (scripts live in `cmd/apogee/testdata/stubllm/toolsautocollapse.yaml`)
- 7: guard folded (wider scope grep and sites; wording grep and settings-manual drift test in Acceptance)

**Sources:**
- `.capcom/issues/lone-tools-never-fold.md` (owner decisions, acceptance)
- `.capcom/knowledge/decisions/adr-0035/adr-0035-amend-1118.md` (the umbrella auto-collapse rule this extends)
- `docs/layout/tool-layout.md` §Fold states and interaction, §Grouped Sub-agents, §Grouped Skills, §Single tool collapsed
- `docs/plans/archived/2026-11-18 - 00 - settled-tools-and-watch-panel-width-plan.md` (items 6–7: the fold-resolution helper and touch writers)

**Ratified design calls (owner, 2026-11-20):**
- **Lone call folded line:** the Group summary an umbrella of one would paint (`✦ Read 1 file ⋯ ▶`, `✦ Ran 1 command ⋯ ▶`).
- **Lone call reopened:** opening it shows today's block exactly; it stays open (touched); no new header toggle on a lone block, every click on it keeps today's meaning.
- **Sub-agent / Skill groups fold too:** to their header line.
- **Group header is always a fold header:** it wears `▼`/`▶` and folds/opens on click or ⏎ on a session-only head flag, live or settled, auto-collapse on or off — a small umbrella's model.
- **Star colour:** every tool block's leading `✦` — umbrellas, lone tool calls (task-list cards and lone Skill/delegation blocks included), Sub-agent and Skill groups — paints in the tools header colour; the live blink unchanged.

**Standing requirements:**
- skills: coding-standards
- Machine rule (forward to every sub-agent): run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./<one package>/`; never a package pattern, never two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); driven tests use `internal/tuitest` + `internal/stubllm`, never an `httptest` upstream; durations through `internal/testtime`.
- Fold state is view state: never written to the session record (`toWireEntry`) or to config.

**Out of scope:**
- Persisting a fold or "touched" to the session record.
- Workflow blocks, task-list cards' own `ui.task-list-open` fold, lone delegation run blocks (`shapeSubAgentRun`) — none auto-collapse.
- Any engine or `domain.ToolSummary` change.

## 1. Paint every tool block's star in the tools header colour — ✅ DONE (2026-11-20)

NOTES (2026-11-20): the star is painted by one helper, `paintStar` (toolblock.go), rather than at each site: it colours only the `✦` glyph, so the live blink's bare cell stays an unstyled space and a `blockState.glyph` override (the Firing's `⟳`) passes through untouched; `paintHeaderMarker` splits the umbrella's measured marker into the coloured star plus the gap in `th.toolHeader`.
NOTES (2026-11-20): added one sentence to `layout.md` §The label stating the star's colour (a doc the change affects, per the implementer's documentation step); not in the item's Files list.
NOTES (2026-11-20): the built-in scheme files' `tool-header` role comments (`internal/scheme/schemes/dark.yaml`, `light.yaml`) still list "header label … and the sub-agent rail" without the star — left as written, since they are not made false and item 7 owns the wording pass.
NOTES (2026-11-20): workflow block headers (`workflowblock.go`) and the session log header (`render.go`, `renderSessionLog`) keep their star uncoloured — both are outside this item's named shapes (the plan's Out of scope names Workflow blocks).

**What:**
**Goal:** The leading `✦` of the umbrella header, a lone tool block header (`renderToolBlock`, every caller), and the Sub-agent / Skill group header renders with the tools header foreground (`ColorScheme.ToolHeader`) in every colour scheme; the live blink (a bare cell) is unchanged; no header text changes colour.
**Approach (assumed at the header base):** Add a `toolStar` style to `theme` (`internal/tui/theme.go`, `Foreground(toolHeaderFg)`, not bold) and paint the marker through it in `paintHeaderMarker` (umbrella, `toolblock.go`), `renderToolBlock`'s `hangingWrap` prefix, and `renderSubAgentGroup`'s header (`subagentblock.go`). `th.toolHeader` itself stays uncoloured — recolouring it would recolour `umbrellaHeaderPaint`'s phrase text. A star override (`blockState.glyph`, the scheduled Firing's `⟳`) keeps today's style.
**Regression guard.** Only the `✦` takes the colour: a glyph override (`blockState.glyph`, the Firing's `⟳` in `render.go`'s `entrySchedule` arm) keeps today's tone, as does the /sessions row's `⟳` (`sessionRowCells`). `TestUmbrellaHeader_ShowsTheGroupSummary` pins the star as `th.toolHeader.Render(defaultGlyphs.assistant+" ")` (`toolblock_test.go`): update that `want` to the new star style, and its run is in Acceptance (`UmbrellaHeader`).
**Files:** internal/tui/theme.go, internal/tui/toolblock.go, internal/tui/subagentblock.go, internal/tui/theme_test.go, internal/tui/toolblock_test.go
**Read first:** internal/tui/toolblock.go — renderToolBlock, paintHeaderMarker, umbrellaHeader; internal/tui/subagentblock.go — renderSubAgentGroup, renderSubAgentRun; internal/tui/theme.go — newTheme, themeStyles; internal/tui/blockstate.go — blockState.star;
internal/tui/wrap.go — hangingWrap, hangingPrefixes; internal/tui/render.go — renderEntryLines; internal/tui/toolblock_test.go — TestUmbrellaHeader_ShowsTheGroupSummary
**Tests:** a render test per shape (umbrella, lone call, Sub-agents group, Skill group) asserting the star's SGR carries the scheme's ToolHeader foreground and the label text after it is unchanged; a live-blink frame still paints a bare cell; a Firing's `⟳` header paints as today; `TestUmbrellaHeader_ShowsTheGroupSummary`'s expected star updated. Regenerate only goldens whose diff is the star's SGR alone (`cmd/apogee/testdata/frames/` are plain text — expect none).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Star|Theme|RenderSuperGroup|RenderToolBlock|SubAgentGroup|UmbrellaHeader' ./internal/tui/`
- `go build ./...`
**Commit:** `feat(tui): paint every tool block's star in the tools header colour`

## 2. Generalise the umbrella fold machinery to a tool-block fold — ✅ DONE (2026-11-20)

NOTES (2026-11-20): foldableHead is a free function `foldableHead(entries []entry, head int) (end int, ok bool)` rather than a transcript method, since the free foldHeadOf must dispatch on it too.
NOTES (2026-11-20): blockSettled now takes (head, end); the four autocollapse_test.go calls go through a new test helper settledAt(tr, head) that reads end off foldableHead — assertions unchanged.

**What:**
**Goal:** One fold-resolution helper answers for any foldable tool-block head (today: the umbrella only), and settledness is computed from a block's head and end rather than from `toolSuperGroup`; behaviour is byte-for-byte unchanged (pure refactor).
**Approach (assumed at the header base):** In `internal/tui/transcript.go`, rename `umbrellaFoldBy`→`blockFoldBy`, `umbrellaFolded`→`blockFolded`, `umbrellaSettled`→`blockSettled` (taking the block's end index, computed by the caller's shape), `umbrellaFoldRule`→`blockFoldRule`, `markUmbrellaTouched`/`openUmbrellaAlone`/`revealUmbrellaMember`/`umbrellaHeadOf`/`setUmbrellaFolded` → `markBlockTouched`/`openBlockAlone`/`revealBlockMember`/`foldHeadOf`/`setBlockFolded`; entry fields `umbrellaFolded`/`umbrellaTouched`/`umbrellaOwnFold` → `folded`/`foldTouched`/`foldOwn`. `foldHeadOf` and `blockFoldBy` dispatch on a single `foldableHead(head) (end int, ok bool)` that today recognises the umbrella only — items 3–4 add shapes there and nowhere else. `umbrellaIsLarge` and `umbrellaFold` (paint key) keep their names: size is umbrella-only. Use `gopls rename`; update comments that name the old identifiers.
**Regression guard.** blockSettled's "every entry done" check walks only the block's OWN members — entries at the head's depth in [head,end) — never the deeper span entries a member's run holds (their narration is never marked done); for the umbrella and lone call that is identical to today, so the refactor stays behaviour-neutral. Files rule: every file `grep -rln` finds for the old identifiers under `internal/` and `cmd/` (comments included — `model.go`, `settingsapply.go`, `toolblock.go`; callers of `setUmbrellaFolded` in `toolblock_test.go`, `paintcache_test.go`).
**Files:** internal/tui/transcript.go, internal/tui/render.go, internal/tui/mouse.go, internal/tui/sidepanel.go, internal/tui/paintcache.go, internal/tui/blocktarget.go, internal/tui/doc.go, internal/tui/model.go, internal/tui/settingsapply.go, internal/tui/toolblock.go, internal/tui/autocollapse_test.go, internal/tui/autocollapse_touch_test.go, internal/tui/transcript_test.go, internal/tui/mouse_test.go, internal/tui/toolblock_test.go, internal/tui/paintcache_test.go
**Read first:** internal/tui/transcript.go — umbrellaFoldBy, umbrellaFolded, umbrellaSettled, umbrellaHeadOf, markUmbrellaTouched, openUmbrellaAlone, revealUmbrellaMember, setUmbrellaFolded; internal/tui/render.go — resolveBlock;
internal/tui/mouse.go — toggleBlockAt; internal/tui/sidepanel.go — followPanelTarget; internal/tui/paintcache.go — blockKey, umbrellaFold; internal/tui/autocollapse_touch_test.go — TestAutoCollapseTouchWritersMoveTheGeneration
**Tests:** existing suites unchanged in assertions (renames only).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'AutoCollapse|Touch|Umbrella|Fold' ./internal/tui/`
- `grep -rn 'umbrellaFoldBy\|umbrellaSettled\|markUmbrellaTouched\|umbrellaTouched\|umbrellaOwnFold' internal/ cmd/` → empty
**Commit:** `refactor(tui): generalise the umbrella fold to a tool-block fold`

## 3. Fold a settled, untouched lone tool call to its Group summary — ✅ DONE (2026-11-20)

NOTES (2026-11-20): a lone call's touch is its own entry flag (`loneTouched`, read through `transcript.touchedMark`) rather than `foldTouched`, so a touched lone call that grows into an umbrella heads an untouched one and a Read a dissolved umbrella leaves alone reads none of the umbrella's flags; `blockFoldBy` answers `foldByNone` for a lone head that is not auto-collapsed.
NOTES (2026-11-20): `foldableHead` recognises the lone call through a new `loneFoldable` helper; the walk-back predicate `foldHeadOf` used is now `extendsGroupableStretch`, shared with `loneFoldable` (same rule, one statement).
NOTES (2026-11-20): the folded lone call is drawn by `renderSuperGroup` under a shut fold with a one-call run (render.go `resolveFoldedCall`, shape `shapeToolSuper`, fold in the paint key); its open state is today's `shapeEntry` block.
NOTES (2026-11-20): consequential edit — internal/tui/paintcache.go: made necessary by the folded lone call painting under shapeToolSuper (its shape comment named the umbrella only)
NOTES (2026-11-20): cmd/apogee/e2e_outcome_test.go (TestE2EOutcomeSlotsCarryTheToolsVerdict) launches with `ui.tools-auto-collapse: false`: its subject is the open row's slot and the edit bodies; the hostile and smoke tests follow the folded line instead.
NOTES (2026-11-20): TestMCPLoginFetch_ContextCancel_ReturnsJoinsInputAndFreesPort failed once in the first whole-package run on "bind: address already in use" for its callback port — an environmental port collision, unrelated to this item.

Depends on item 2.
**What:**
**Goal:** With auto-collapse on, a lone groupable tool call (an `entryToolCall` that heads no umbrella and is `groupable`) that is settled and untouched paints exactly the line `umbrellaHeader` paints for a one-call umbrella, folded (`▶`); a click or ⏎ on that line opens it alone (touched, no config write) and it paints today's block from then on; a live, touched, or auto-collapse-off lone call paints as today; a side-panel jump to a folded lone call opens it.
**Approach (assumed at the header base):** `foldableHead` recognises the lone groupable call (end = head+1). In `resolveBlock`'s single-entry tail (`render.go`), when `blockFolded(head)` for a lone call, resolve a block that draws `umbrellaHeader(th, []toolRunView{{views: {view}, done: {in.done}}}, …, open=false)` marked `targetUmbrella`, and carry the fold in the paint key (`resolvedBlock.fold`) so a settle repaints. `toggleBlockAt`'s `targetUmbrella` arm already routes `foldBySettled` → `openBlockAlone`. Excluded: `entrySchedule`, orphan `entryToolResult`, task-list cards (`collapsesToHeader`), solo views (Skill fetch, delegations). Update the lone-call assertion in `TestUmbrellaHeadOfFindsTheHeadFromAnyMember`; fix any existing `cmd/apogee` e2e assert that reads a settled lone call's `┕` row under default config by asserting the folded line (never by turning auto-collapse off unless the test's subject is the open row).
**Regression guard.** Every `cmd/apogee` test that waits on or asserts a lone groupable call's label, target or outcome after narration or the Turn's end follows the folded line (grep `WaitText`/`Contains` for outcome words — e.g. `TestE2EHostileToolResultsKeepOneRowPerEntry`, `TestE2ESmokeInProcess`, `TestE2ESmokePTY`); Acceptance runs the whole `./cmd/apogee/` package. A lone shape folds only on `foldBySettled` (`blockFolded` for a lone head = rule == `foldBySettled`), so a stale head flag left by a dissolved small umbrella (a Read beside a pending `ask_user` that turns solo on its answer) is never read. A lone call's touch never carries into an umbrella it later heads (its own flag, or cleared when the head grows into an umbrella): that umbrella auto-collapses as today.
**Files:** internal/tui/transcript.go, internal/tui/render.go, internal/tui/toolblock.go, internal/tui/mouse.go, internal/tui/autocollapse_test.go, internal/tui/autocollapse_touch_test.go, cmd/apogee/e2e_hostile_test.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/ (every other affected test file)
**Read first:** internal/tui/render.go — resolveBlock, renderEntryLines; internal/tui/transcript.go — umbrellaFoldBy, umbrellaSettled, umbrellaHeadOf, openUmbrellaAlone; internal/tui/toolblock.go — umbrellaHeader, toolRunView; internal/tui/mouse.go — toggleBlockAt;
internal/tui/toolbranch.go — groupable; cmd/apogee/e2e_hostile_test.go — TestE2EHostileToolResultsKeepOneRowPerEntry; cmd/apogee/e2e_smoke_test.go — TestE2ESmokeInProcess, TestE2ESmokePTY
**Tests:** `TestAutoCollapseFoldsALoneCall` (settles on narration / turn end; live stays open; off unchanged; solo and task-list excluded; a Read left alone when a folded small umbrella with a pending `ask_user` dissolves paints open with auto-collapse off); touch test: click and ⏎ on the folded line open it, no settings write, other folds unchanged; a live lone call touched, then joined by a second call, heads an umbrella that still auto-collapses on settle; panel-jump test reveals a folded lone call.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'AutoCollapse|Touch|LoneCall' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/apogee/`
**Commit:** `feat(tui): fold a settled, untouched lone tool call to its Group summary`

## 4. Give Sub-agent and Skill group headers a fold of their own — ✅ DONE (2026-11-20)

NOTES (2026-11-20): foldableHead now returns the block's shape as a third value (end, shape, ok) — the plan's "foldableHead returns the shape"; the settled arm of blockFoldBy is gated by autoCollapses(shape) (umbrella and lone call only), so no group auto-collapses yet.
NOTES (2026-11-20): consequential edit — internal/tui/autocollapse_test.go: made necessary by foldableHead's new three-value signature (settledAt helper's one call site).
NOTES (2026-11-20): consequential edit — internal/tui/blocktarget.go: made necessary by group headers now being marked targetUmbrella (targetKind doc comment).
NOTES (2026-11-20): consequential edit — internal/tui/paintcache.go: made necessary by resolveGroup now carrying its header fold in umbrellaFold (doc comment said every non-umbrella shape carries the zero value).
NOTES (2026-11-20): blockcursor.go needed no edit — the header became a cursor stop by being marked (cursorStops compares lineTarget whole, so {targetUmbrella, head} and the first member's {targetHeader, head} are separate stops), and cursorRunHead reads targetHeader only, so member rows answer ^x unchanged; both pinned in TestGroupHeaderFoldsItsGroupAlone.
NOTES (2026-11-20): foldHeadOf now maps any Sub-agent/Skill group member to its group head (new toolGroupAt), so a member-row click and revealBlockMember also mark the group touched; harmless here (touch only gates the settled arm, which groups do not take yet) and the shape item 5 builds on.
NOTES (2026-11-20): the group header is now a leader row (umbrella header shape: `✦ Sub-agents (2) ⋯ ▼`), replacing the hangingWrap header; test goldens use a new groupHeaderLine helper in subagentblock_test.go. Also ran the cmd/apogee sub-agent/skill e2e set (fan-out, delegation, naming, parallel, preempt, sub-agent view, tools-fold-subagent, fallback TUI): all pass.

Depends on item 2.
**What:**
**Goal:** The `✦ Sub-agents (N)` and `✦ Skill (N)` group header always wears `▼` (open) / `▶` (folded) at the block's right edge and is a fold target: click or ⏎ folds/opens that group alone on its head's session-only flag (no config write), live or settled; folded, the group paints its header line alone (live star while a member runs) and the block covers every member and its span; member rows and their expanded state are untouched under the fold. Auto-collapse not yet applied to groups.
**Approach (assumed at the header base):** `foldableHead` recognises a group head (`subAgentGroupAt` / `ownGroupAt(…, loadSkillToolName)` at pos 0), end = past the last member's span. `renderSubAgentGroup` paints the header via an `indicatorRow` like `umbrellaHeader`, marked `targetUmbrella`; `resolveGroup` (`render.go`), when folded, resolves ONE block for the whole group (no `openTail` split) with the fold in the paint key. `toggleBlockAt`'s `targetUmbrella` arm handles it through `blockFoldBy` (`foldByHead` → `setBlockFolded` + touched); the block cursor gains the header as a stop. `followPanelTarget`'s run-row jump to a member of a folded group unfolds it first (`revealBlockMember`).
**Regression guard.** Gate `blockFoldBy`'s settled arm by shape (`foldableHead` returns the shape; only umbrella and lone answer `foldBySettled`) until item 5, so no Skill or unspanned Sub-agent group auto-collapses here. Files rule: every test that pins a group header line by equality (`grep -nE '"✦ (Sub-agents|Skill) \([0-9]+\)"' internal/tui/*_test.go`) or clicks it (`TestRunViewOpensOnExpand`); Acceptance runs the whole `./internal/tui/` package.
**Files:** internal/tui/transcript.go, internal/tui/render.go, internal/tui/subagentblock.go, internal/tui/mouse.go, internal/tui/sidepanel.go, internal/tui/blockcursor.go, internal/tui/subagentblock_test.go, internal/tui/mouse_test.go, internal/tui/transcript_test.go, internal/tui/transcriptbridge_test.go, internal/tui/wrap_test.go, internal/tui/toolshape_test.go, internal/tui/runview_test.go
**Read first:** internal/tui/render.go — resolveGroup, resolveBlock; internal/tui/subagentblock.go — renderSubAgentGroup; internal/tui/transcript.go — ownGroupAt, ownGroup, subAgentGroupAt, umbrellaFoldBy; internal/tui/mouse.go — toggleBlockAt;
internal/tui/blockcursor.go — cursorStops, cursorRunHead; internal/tui/sidepanel.go — followPanelTarget; internal/tui/runview_test.go — TestRunViewOpensOnExpand
**Tests:** header paints `▼`/`▶`; click and ⏎ fold/unfold one group alone, settings untouched; folded group with an expanded member covers the whole group; live folded header blinks; panel jump into a folded group unfolds it; `cursorRunHead`/^x on member rows unchanged when open; a settled, untouched Skill group stays open with auto-collapse on; the header-equality pins and `TestRunViewOpensOnExpand`'s member-row count follow the new header.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tui/`
**Commit:** `feat(tui): fold Sub-agent and Skill groups from their header`

## 5. Auto-collapse settled, untouched Sub-agent and Skill groups — ✅ DONE (2026-11-20)

NOTES (2026-11-20): the settled arm of `blockFoldBy` is no longer gated by shape, so `autoCollapses` was deleted and `foldableHead` / `toolGroupAt` drop the shape value item 4 added only for that gate (now read nowhere); `settledAt` in autocollapse_test.go follows the two-value signature.
NOTES (2026-11-20): member done-ness is a new `entry.reported()` (done, or the FINISHED phase — subAgentReported's rule on the entry), used for every block's own members and for the run-head check `blockSettled` already made; deeper span entries are still never asked.
NOTES (2026-11-20): the run-view redirect in `toggleBlockAt` marks the group touched before `openRunAt` when `runOpenableAt` answers yes; a lone delegation heads no foldable block and marks nothing.
NOTES (2026-11-20): consequential edit — internal/tui/subagentblock_test.go: made necessary by groups now taking the settled fold (item 4's interim "auto-collapse never folds a settled, untouched group" subtest now pins the folded header).
NOTES (2026-11-20): consequential edit — internal/tui/blocktarget.go: made necessary by group headers now taking the settled fold (targetUmbrella doc comment).
NOTES (2026-11-20): consequential edit — internal/tui/render.go: made necessary by group headers now taking the settled fold (resolveGroup doc comment).
NOTES (2026-11-20): e2e: a shared `openSubAgentsGroup` helper (e2e_fanout_test.go) opens the folded group before tests whose subject is the member rows — livestate (read page by page, since the opened list holds the view on its header), both preempt journeys, the stream test, and the delegation step-cap continuation (two delegations from successive replies group by adjacency). e2e_parallel_test.go needed no change.

Depends on items 3, 4.
**What:**
**Goal:** With auto-collapse on, a Sub-agent or Skill group whose members have all finished and that something has moved past (a same-run follower, or its run/Turn no longer live — the umbrella's settled rule) and that the reader has not touched paints folded; touching any of its rows or its header exempts it; off, groups follow their head flag only.
**Approach (assumed at the header base):** `blockSettled` over the group's head..end: every member done (a delegation member done = its run head finished, `subAgentReported`'s rule), then the same follower / run-live walk the umbrella uses. `blockFoldBy` order unchanged (settled∧untouched → folded; touched∧own → head flag; else size rule for umbrellas, head flag for groups and lone calls). Every `targetHeader` hit on a member row marks the group touched (`markBlockTouched` resolves the group head via `foldHeadOf`). Fix existing default-config e2e asserts that read a settled group's member rows (`e2e_subagent_preempt_test.go`, `e2e_fanout_test.go`, `e2e_livestate_test.go`, `e2e_subagent_stop_test.go` — check each) by asserting the folded header or opening it, unless the subject is the open rows.
**Regression guard.** a group member counts as done when it is a delegation whose run head is finished (runHead.done || phase == SubAgentFinished, subAgentReported's rule) or, for a skill fetch, its entry.done; deeper span entries are never consulted for done-ness, and the follower walk skips deeper entries as umbrellaSettled already does. Also: sub_agent calls from successive tool-call-only replies group too (ownGroup is block adjacency), so item 5's e2e sweep covers every default-config e2e test that produces two adjacent sub_agent or load_skill calls, not only fan-out scripts. Opening a framed member's run view from a group row (`mouse.go` `toggleBlockAt` `targetHeader` → `openRunAt`) DOES mark that group touched, before `openRunAt` returns, so a group the reader entered never auto-folds behind them (the umbrella rule: any reader action inside marks it touched).
**Files:** internal/tui/transcript.go, internal/tui/mouse.go, internal/tui/autocollapse_test.go, internal/tui/autocollapse_touch_test.go, cmd/apogee/e2e_subagent_preempt_test.go, cmd/apogee/e2e_fanout_test.go, cmd/apogee/e2e_livestate_test.go, cmd/apogee/e2e_stream_test.go, cmd/apogee/e2e_delegation_test.go, cmd/apogee/e2e_parallel_test.go, cmd/apogee/ (any other default-config e2e file with two adjacent `sub_agent` or `load_skill` calls)
**Read first:** internal/tui/transcript.go — umbrellaSettled, umbrellaFoldBy, markUmbrellaTouched, ownGroup, ownGroupAt; internal/tui/mouse.go — toggleBlockAt; internal/tui/runview.go — openRunAt, runOpenableAt; internal/tui/subagentblock.go — subAgentReported, renderSubAgentMemberRows; internal/tui/render.go — resolveGroup;
internal/tui/autocollapse_test.go — autoCollapseUmbrella, TestAutoCollapseRunViewFoldsOnChildReport; cmd/apogee/e2e_subagent_preempt_test.go — TestE2EQueuedMessagePreemptsTheScheduledSubAgents; cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateLauncherMoveKeepsTheSessionWorking
**Tests:** table test: Sub-agents and Skill groups settle on narration / turn end, stay open while a member runs, touched exempt, off unchanged, replayed group folded, run-view nested group settles on child report; a spanned Sub-agent group (narration inside a member's span) settles once every run head is finished; touch test: a click and a ⏎ on a framed member row open its run view AND mark the group touched, so on return the settled group stays open.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'AutoCollapse|Touch' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'E2ESubAgent|E2EFanOut|E2ELiveState|E2EQueuedMessage|E2EDelegation|E2EParallel' ./cmd/apogee/` (one run each per the machine rule)
**Commit:** `feat(tui): auto-collapse settled Sub-agent and Skill groups`

## 6. Drive the widened auto-collapse end to end

Depends on item 5.
**What:**
**Goal:** Driven e2e tests prove, through a real TUI on a scripted upstream, that a settled lone call, a settled Sub-agent group and a settled Skill group fold under the default config, open on click, and stay open; and that the folded lines' stars are the tools colour.
**Approach (assumed at the header base):** Extend `cmd/apogee/e2e_toolsautocollapse_test.go` (`autoCollapseSurvey`) with stub scripts (`cmd/apogee/testdata/stubllm/toolsautocollapse.yaml`) for a lone `read_file` turn, a two-delegation fan-out and a two-skill-fetch turn; assert the folded lines (`✦ Read 1 file`, `✦ Sub-agents (2) … ▶`, `✦ Skill (2) … ▶`), click to open, re-settle and still open. A golden only where the frame is the subject.
**Regression guard.** The scripts live in `cmd/apogee/testdata/stubllm/*.yaml` (`loadScript`), not `internal/stubllm`: extend `toolsautocollapse.yaml` with the lone `read_file`, two-`sub_agent` and two-`load_skill` prompts plus their wrap-ups. No driven script emits `load_skill` today, so the Skill turn is new script text there.
**Files:** cmd/apogee/e2e_toolsautocollapse_test.go, cmd/apogee/testdata/stubllm/toolsautocollapse.yaml, cmd/apogee/testdata/frames/ (new golden only if added)
**Read first:** cmd/apogee/e2e_toolsautocollapse_test.go — autoCollapseSurvey, TestE2EToolsAutoCollapseSparesATouchedUmbrella, quitCleanly; cmd/apogee/e2e_toolsfold_test.go — umbrellaHeaderMatch (find, in, await); cmd/apogee/testdata/stubllm/toolsautocollapse.yaml;
cmd/apogee/e2e_delegation_test.go — assertNoErrorTone (SameColor/StyleRuns precedent for the star colour); internal/tools/load_skill.go — LoadSkill (read-only, a miss is still a result)
**Tests:** `TestE2EToolsAutoCollapseFoldsALoneCall`, `…FoldsASubAgentGroup`, `…FoldsASkillGroup`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'E2EToolsAutoCollapse' ./cmd/apogee/`
**Commit:** `test(e2e): drive auto-collapse of lone calls and Sub-agent and Skill groups`

## 7. State the widened rule in docs, ADR and settings text

Depends on item 5.
**What:**
**Goal:** Every doc, glossary Note and settings description that states the auto-collapse scope says it covers umbrellas, lone tool calls, Sub-agent and Skill groups; the group header's fold and the coloured star are specified; ADR 0035 carries a dated amendment (`adr-0035-amend-1120`) recording the owner calls and superseding `adr-0035-amend-1118`'s Scope paragraph, named on the record's Status line and reading list.
**Approach (assumed at the header base):** Rule: every sentence stating auto-collapse acts on "umbrellas"/"a settled umbrella" only, or that a lone call / group keeps its header open — find with `grep -rniE 'auto-?collapse|ToolsAutoCollapse' internal cmd docs layout.md .capcom/knowledge` minus historical paths. Sites known at write time: `docs/layout/tool-layout.md` (§Fold states, §Grouped Sub-agents/Skills sketches gain `▼`/`▶`, §Single tool collapsed), `docs/manual/configuration.md`, `docs/manual/sessions.md`, `layout.md`, `internal/config/registry.go` Desc, `internal/config/config.go`, `internal/config/defaults/config.yaml`, `internal/domain/uiprefs.go`, `internal/tui/tui.go`, `internal/tui/transcript.go` (`toolsAutoCollapse` field), glossary `group-summary`. Historical text (CHANGELOG, archived plans, ADR Decision bodies) stays.
**Regression guard.** The scope grep is `grep -rniE 'auto-?collapse|ToolsAutoCollapse' internal cmd docs layout.md .capcom/knowledge` minus historical paths — the narrower one missed `config.go`'s `ToolsAutoCollapse` comment, `tui.go`'s "settled, untouched umbrella", `transcript.go`'s `toolsAutoCollapse` field, `uiprefs.go` and `docs/manual/sessions.md`'s resumed-session paragraph. Acceptance checks the wording (an umbrella-only grep) and runs `TestManualDocumentsEverySettingsKey`, since `configuration.md` is edited.
**Files:** docs/layout/tool-layout.md, docs/manual/configuration.md, docs/manual/sessions.md, layout.md, internal/config/registry.go, internal/config/config.go, internal/config/defaults/config.yaml, internal/domain/uiprefs.go, internal/tui/doc.go, internal/tui/tui.go, internal/tui/transcript.go, .capcom/knowledge/decisions/adr-0035.md, .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120.md, .capcom/knowledge/glossary/glossary-context/group-summary.md
**Read first:** .capcom/knowledge/decisions/adr-0035.md — Status line, reading list; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1118.md — Scope paragraph; internal/config/registry.go — ui.tools-auto-collapse Desc; internal/config/defaults/config.yaml — tools-auto-collapse comments;
docs/layout/tool-layout.md — Fold states and interaction, Grouped tools folded (settled umbrella); docs/manual/configuration.md — ui.tools-auto-collapse; .capcom/knowledge/glossary/glossary-context/group-summary.md; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** drift tests unchanged and green (`go test -run Drift ./...` per package as they live); `capcom check` clean.
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0035"` names `adr-0035-amend-1120`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Drift|Registry' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ManualDocuments' ./cmd/apogee/`
- `grep -rniE 'settled,? (untouched )?(Tools )?umbrella' internal cmd docs layout.md .capcom/knowledge --exclude='*_test.go' | grep -vE 'decisions/|archived/|CHANGELOG'` → empty, or every hit also names lone calls and Sub-agent / Skill groups
**Commit:** `docs(tools): specify auto-collapse of lone calls and Sub-agent and Skill groups`
