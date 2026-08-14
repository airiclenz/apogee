# ISSUES residuals + render_test.go split — implementation plan

**Goal:** close the plan-ready open defects in `ISSUES.md` ("Run residuals — open (2026-08-14)"):
five doc/comment fixes, plus splitting the 5520-line `internal/tui/render_test.go` into
per-subject test files beside the sources they exercise.

**Date:** 2026-08-14 · **Status:** ready · **sized for:** ~200k-context host

**Authoritative sources:**
- `ISSUES.md`, section "Run residuals — open (2026-08-14, open-residuals sweep)" — the defect
  register each item discharges. Each item REMOVES its own bullet from that section on completion
  (the changelog is the closed trail; a done item never stays in `ISSUES.md`).
- For item 2: `internal/security/doc.go:103-104` and `internal/security/safeio.go:629` are the
  ground truth the ADR amendment must match. If item text and that code disagree, the code wins.
- For items 6–10: the current `internal/tui/render_test.go` itself. The group letters (B–R) used
  below refer to the file's own `// ----` banner sections, in order; each item names the test
  functions explicitly, and the function names are authoritative over the letters.

**Ratified design calls:**
- Scope selection (owner, via AskUserQuestion, 2026-08-14): the six doc/comment residuals AND the
  `render_test.go` split are in; the context-budget % config key stays parked; the Windows
  job-object breakaway test stays open in `ISSUES.md` (needs a Windows host — not this plan's).
- Test-file partition (plan author, 2026-08-14, mechanical — file-name and grouping choices with
  no user-visible consequence): new files `wrap_test.go`, `userblock_test.go`,
  `subagentblock_test.go`, `toolblock_test.go`, `toolleader_test.go`, `toolbranch_test.go`,
  `toolshape_test.go`, `blocktarget_test.go`, `startupbox_test.go`, `chromelayout_test.go`;
  `render_test.go` survives as the shared-helper + cross-cutting file. Ambiguous tests are pinned
  in the item texts below — those placements are binding.

**Standing requirements:**
- skills: coding-standards
- Items 6–10 are PURE MOVES: no test may be renamed, deleted, reordered internally, or edited
  beyond what the move itself requires (import blocks in the new files, removal from the old).
  All files stay in `package tui`, so cross-file helper use keeps compiling — moving a test away
  from a helper is never a reason to duplicate the helper.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The Windows job-object breakaway test (`ISSUES.md` bullet stays).
- The parked context-budget % config key.
- Any behavior change: items 1–5 touch prose/comments only; items 6–10 move test code verbatim.
- Version identifiers: no VERSION/CHANGELOG-release-heading change (see closing note).

---

## 1. show-scrollbar prose covers every popup pane — ✅ DONE (2026-08-14)

NOTES (2026-08-14): `internal/config/defaults/config.yaml` needed no edit — the `ui:` block intro (`:509–:511`, "whether a scroll bar is shown") and the Defaults line (`:516`, "the scroll bar shown") are already scope-neutral and never frame the bar as the transcript's alone, so the item's "if a line needs no change, leave it" clause applies; the file is not in this item's FILES.

NOTES (2026-08-14): this run reused the prior attempt's `config.go`/`tui.go` wording found in the working tree (per the dispatch DECISION) and rewrapped the `HideScrollbar` comment, whose first edit had left one 130-column line among ~100-column neighbours.

NOTES (2026-08-14): the working tree's `ISSUES.md` arrived with items 1–4's bullet removals already merged into one edit; per the DECISION that each removal lands in its own commit, `ISSUES.md` was restored to HEAD and only this item's two bullets (the config.yaml-intro bullet and the doc-comments bullet) removed. Items 2–4 re-apply their own removals in their own runs.

**What:** The `show-scrollbar` key now gates the scroll bar in the transcript AND every popup
pane (`internal/config/defaults/config.yaml:539`, `:568` state this); three summaries were not
brought along:

- `internal/config/defaults/config.yaml` — the `ui:` block intro (`:509–:510`) and the Defaults
  line (`:516`). Reword only if they frame the bar as the transcript's alone; match the scope the
  key's own prose states at `:539`/`:568`. The intro's current wording is looser than the
  `ISSUES.md` bullet claims ("whether a scroll bar is shown" — possibly already scope-neutral);
  if a line needs no change, leave it and record that in a dated NOTES line under this item.
- `internal/config/config.go:1533` — the `ShowScrollbar` doc comment ("gates the transcript's
  scroll bar and the column it hangs in"): widen to the transcript and every popup pane.
- `internal/tui/tui.go:242` — the `HideScrollbar` comment ("takes the transcript's scroll bar
  away"): same widening.

Remove the two corresponding bullets from `ISSUES.md` (the config.yaml-intro bullet and the
doc-comments bullet).

**Files:** `internal/config/defaults/config.yaml`, `internal/config/config.go`,
`internal/tui/tui.go`, `ISSUES.md`

**Tests:** none — prose only.

**Acceptance:** `go build ./internal/config/ ./internal/tui/` succeeds;
`grep -n "popup" internal/config/config.go internal/tui/tui.go` shows the widened wording at the
two comment sites.

**Commit:** `docs(config): show-scrollbar prose names the popup panes too`

---

## 2. ADR 0049 records the resolved-first permit routing — ✅ DONE (2026-08-14)

NOTES (2026-08-14): per the dispatch DECISION, this run reused the ADR amendment an earlier batch
attempt had already left in the working tree (verified line-by-line against `openMutationRoot`,
`openPermittedRoot`, `namesPermittedTarget`, `deepestExistingDir` and `rootRelative` before keeping
it) and re-applied only this item's `ISSUES.md` bullet removal, so the ADR edit and the bullet
removal land in this item's own commit. Items 3 and 4's dirty edits in `internal/tools/` were left
untouched.

**What:** `docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md`
never describes two properties its own fix introduced; `internal/security/doc.go:103-104` states
both, the ADR does not. Append a dated (2026-08-14) amendment note to the ADR (never rewrite the
original decision text) recording:

1. The permit match is **resolved-based** while the fallback branch stays **lexical**
   (`internal/security/safeio.go:629` is the code site).
2. The permit-plus-workspace-internal-symlink case, as `doc.go:103-104` states it.

Derive the amendment's wording from `doc.go` and `safeio.go` — the code and its package doc are
the ground truth; do not invent semantics beyond what they state. Remove the ADR 0049 bullet from
`ISSUES.md`.

**Files:** `docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md`,
`ISSUES.md`

**Tests:** none — doc only.

**Acceptance:** `grep -in "resolved" "docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md"`
hits the new amendment; the amendment's claims match `internal/security/doc.go:103-104` on read.

**Commit:** `docs(adr): 0049 amendment records resolved-first permit routing`

---

## 3. setProcessGroupTeardown overview admits the setsid escape — ✅ DONE (2026-08-14)

NOTES (2026-08-14): per the dispatch DECISION, this run kept the `internal/tools/exec_pgroup_unix.go`
comment edit an earlier batch attempt had already left in the working tree (verified against the
file's own setsid-escape paragraph at `:25-34` and the `killProcessGroup` note at `:63-68` before
keeping it) and applied only this item's `ISSUES.md` bullet removal, so both land in this item's own
commit. Item 4's dirty `internal/tools/path_safety_test.go` was left untouched.

NOTES (2026-08-14): removing this item's bullet stranded the neighbouring Windows bullet's "that
setsid escape" antecedent, so that bullet's opener was repointed to name the POSIX escape and its
code site directly (`internal/tools/exec_pgroup_unix.go:63`); the bullet itself stays open, per the
plan's Out-of-scope note.

**What:** `internal/tools/exec_pgroup_unix.go:19` — the overview bullet's absolute "never orphans
its children (or, when confined, an orphaned sandbox-exec wrapper)" contradicts the setsid escape
the same file documents at `:63` (a descendant that called setsid/setpgid(0,0) is outside the
group). Soften the overview so it no longer claims the absolute — e.g. scope it to the process
group and point at the escape note below — keeping the file's comment voice. Remove the
corresponding `ISSUES.md` bullet.

**Files:** `internal/tools/exec_pgroup_unix.go`, `ISSUES.md`

**Tests:** none — comment only.

**Acceptance:** `go build ./internal/tools/` succeeds; the overview at the top of
`exec_pgroup_unix.go` no longer states an unqualified "never orphans its children".

**Commit:** `docs(tools): teardown overview no longer claims an absolute against setsid`

---

## 4. tempRoot comment drops the absent fixture suite — ✅ DONE (2026-08-14)

NOTES (2026-08-14): per the dispatch DECISION this run kept the `path_safety_test.go` comment edit an earlier batch attempt had already left in the working tree (verified first that the file really declares no `Test` function — only `writeFixtureFile`, `realPath`, `tempRoot`) and re-applied only this item's `ISSUES.md` bullet removal, so both land in this item's own commit.

NOTES (2026-08-14): the kept edit had left the paragraph's tail ragged (one ~73-column line among ~100-column neighbours), so the rest of that comment paragraph was rewrapped to the file's ~100-column wrap; wording after the edited clause is unchanged.

**What:** `internal/tools/path_safety_test.go:44-45` — `tempRoot`'s package-rule comment ends
"plus this file's own fixtures", but the file now holds the shared path fixtures and no `Test`
function of its own. Rewrite the trailing clause to state what the file actually is (the shared
fixture home), dropping the reference to a suite that is not there. Remove the corresponding
`ISSUES.md` bullet.

**Files:** `internal/tools/path_safety_test.go`, `ISSUES.md`

**Tests:** none — comment only.

**Acceptance:** `go vet ./internal/tools/` succeeds;
`grep -n "this file's own" internal/tools/path_safety_test.go` returns nothing.

**Commit:** `docs(tools): tempRoot comment stops naming an absent suite`

---

## 5. upstream.go credits aliasFromEndpoint for the ephemeral row's label — ✅ DONE (2026-08-14)

NOTES (2026-08-14): the longer identifier pushed the edited line past the file's ~100-column comment
wrap, so the four lines of that sentence (`cmd/apogee/upstream.go:282-286`) were rewrapped; no
wording changed apart from the credited helper name.

**What:** `cmd/apogee/upstream.go:283` still credits `hostFromEndpoint` for the synthesized
ephemeral row's label, which `config.aliasFromEndpoint` (`:292` names it) now produces. Fix the
`:283` comment to credit `config.aliasFromEndpoint`, keeping the sentence's surrounding meaning
intact. Remove the corresponding `ISSUES.md` bullet.

**Files:** `cmd/apogee/upstream.go`, `ISSUES.md`

**Tests:** none — comment only.

**Acceptance:** `go build ./cmd/apogee/` succeeds;
`grep -n "hostFromEndpoint" cmd/apogee/upstream.go` no longer hits the ephemeral-row label
comment (other legitimate uses of the identifier may remain).

**Commit:** `docs(cmd): ephemeral row label credit names aliasFromEndpoint`

---

## 6. Carve wrap_test.go and userblock_test.go out of render_test.go — ✅ DONE (2026-08-14)

NOTES (2026-08-14): banner disposition where a `// ----` section splits across files — the
"Sub-agent framing reflow safety (P3.14)" banner stays in `render_test.go` with
`TestSubAgentReflowAtSmallWidths` (item 7 carries it away), so `TestRailedWidthFloors` opens
`wrap_test.go` under its own doc comment; conversely the "The row-capped clip (clipWrap)" banner
moved with the clipWrap suite, so `TestUserBlockRowsAreOneSquareLineEach` opens
`userblock_test.go` under its own doc comment. No banner text was invented or duplicated, and the
moved bodies are byte-identical to their `HEAD` originals (verified by reconstructing
`render_test.go` from the removed ranges).

**What:** Create `internal/tui/wrap_test.go` and `internal/tui/userblock_test.go`; move the
following out of `internal/tui/render_test.go` verbatim (pure moves — see Standing requirements):

To `wrap_test.go` (~610 lines): `TestRailedWidthFloors`; `TestWrapTextHoldsTheWidthCap` with
helper `withoutSpaces`; `TestWrapTextBreaksInThePaintersMeasure`; the clipWrap suite —
`TestClipWrapLeavesFittingTextAlone`, `TestClipWrapHoldsItsRowBudget`,
`TestClipWrapSurvivesNarrowWidths`, `TestWrappedSurfacesBreakInThePaintersMeasure`; and the rail
tests — `TestRenderSpacerRailsAtTheJoinDepth`, `TestRenderConsecutiveSubAgentRunsAreNotConnected`,
`TestRenderSpacerRailIsStyledAndUntrailed`, `TestSubRailPaintedInToolHeaderGold`,
`TestSubAgentFrameSplitsRailGoldFromBranchTone`, `TestSubAgentDoneMarkPaintedInTheSuccessRole`.

To `userblock_test.go` (~440 lines): `TestUserBlockRowsAreOneSquareLineEach` (binding placement —
it sits under the clipWrap banner but its subject is `renderUserBlock`); the prompt-paint suite
with helpers `promptRows`/`splitMarker` — `TestCollapsedPromptPaintsThreeRowsWithAnInlineMarker`,
`TestExpandedPromptPaintsItsWholeBodyAndTrailsSeeLess`,
`TestUnderThresholdPromptIgnoresItsExpandedState`, `TestPromptCollapseFollowsThePaintWidth`,
`TestPromptWithSkillsPaintsNoChipRow`; the skill-accent suite with helpers `spanOf`/`accentRuns`
(also called from `paint_test.go` — same package, keeps compiling) —
`TestSentBlockAccentsItsSkillTokens`, `TestSentBlockAccentsEveryOccurrence`,
`TestAccentedSkillTokenStraddlesASoftWrap`, `TestCollapsedBlockAccentsOnlyWhatItShows`,
`TestPromptMarkerCarriesTheHighlightStyle`.

Each new file gets the minimal import block it needs. `TestSubAgentReflowAtSmallWidths` stays in
`render_test.go` for now (item 7 takes it).

**Files:** `internal/tui/render_test.go`, `internal/tui/wrap_test.go`,
`internal/tui/userblock_test.go`

**Tests:** the moved tests themselves — no new tests.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `gofmt -l internal/tui` prints
nothing; every moved test name appears exactly once under `internal/tui/`
(`grep -rc "func TestClipWrapHoldsItsRowBudget" internal/tui/*_test.go` style spot checks).

**Commit:** `test(tui): carve wrap and userblock suites out of render_test.go`

---

## 7. Carve subagentblock_test.go out of render_test.go — ✅ DONE (2026-08-14)

NOTES (2026-08-14): banner disposition — the `// ----` banner "Sub-agent framing reflow safety (P3.14)" heads a section whose only test (`TestSubAgentReflowAtSmallWidths`) this item moves, so the banner travelled with it and now opens `subagentblock_test.go`; the sketch-state, delegation and spanless suites sit under the "The tool call block" and ask-user banners, which stay in `render_test.go` with the tests still filed there, so those suites arrive bannerless under their own doc comments (same disposition rule item 6 recorded). No banner text was invented or duplicated.
NOTES (2026-08-14): `render_test.go`'s import block lost `encoding/json`, which only the moved delegation helpers used; every other import still has callers in the file. The moved bodies are byte-identical to their `HEAD` originals and the residual `render_test.go` is `HEAD` minus exactly the moved ranges and that one import line (verified by reconstruction diff).

**What:** Create `internal/tui/subagentblock_test.go` (~890 lines); move verbatim from
`render_test.go`: `TestSubAgentReflowAtSmallWidths` (binding — exercises `railedWidth` via
sub-agent framing, lives with the sub-agent suite); the sketch-state suite with helpers
`targetedRender`/`rowWith` — `TestRenderSubAgentGroupSketchStates`,
`TestSubAgentMemberDoneOnItsOwnFinishedPhase`, `TestSubAgentScheduledUntilItStarts`; the
delegation suite with helpers `loneDelegation`/`delegateWithPrompt`/`delegateAsked` —
`TestSubAgentCloserOnlyWhenAnotherGroupedMemberFollows`,
`TestLoneSubAgentRunOpensInTheGroupMembersFrame`, `TestExpandedSubAgentKeepsItsTopLevelDetails`,
`TestExpandedSubAgentOpensWithItsPrompt`, `TestExpandedSubAgentPromptOpensOnOneBlankRailLine`;
and `TestSpanlessSubAgentHeadsGroupWithEachOther` (binding — filed under the ask-user banner, but
its subject is sub-agent grouping).

**Files:** `internal/tui/render_test.go`, `internal/tui/subagentblock_test.go`

**Tests:** the moved tests themselves — no new tests.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `gofmt -l internal/tui` prints
nothing; each moved test name appears exactly once under `internal/tui/`.

**Commit:** `test(tui): carve the sub-agent block suite out of render_test.go`

---

## 8. Carve toolleader_test.go and toolblock_test.go out of render_test.go — ✅ DONE (2026-08-14)

NOTES (2026-08-14): banner disposition, same rule items 6 and 7 recorded — the `// ----` banner "The tool header's label styling" heads a section whose only test (`TestToolHeaderLabelStyled`) this item moves, so it travelled with it and now opens `toolleader_test.go`; the grouping and group-member suites sit under the "Grouped same-label tool calls (tool-call layout item 4)" banner, which stays in `render_test.go` with `readCall` and the tests still filed there, so those suites arrive bannerless under their own doc comments. No banner text was invented or duplicated.
NOTES (2026-08-14): `render_test.go`'s import block is unchanged — every import still has callers in the residual file. The moved bodies are byte-identical to their `HEAD` originals and the residual is `HEAD` minus exactly the moved ranges (verified by reconstructing `HEAD`'s `render_test.go` from residual + moved chunks: exact match).

**What:** Create `internal/tui/toolleader_test.go` (~385 lines) and
`internal/tui/toolblock_test.go` (~500 lines); move verbatim from `render_test.go`:

To `toolleader_test.go`: `TestToolHeaderLabelStyled`; `TestLeaderRowSpendsItsRoomInOrder`,
`TestPromoteGuardHoldsFifteenCellsOfTarget`, `TestDemotedLineKeepsTheSpellingItWasWrittenWith`,
`TestGitCommitSlotIsTheShortHashAtEveryWidth`.

To `toolblock_test.go`: the same-label grouping suite — `TestRenderGroupsConsecutiveSameLabelCalls`,
`TestRenderGroupsInsideSubAgent`, `TestRenderGroupsDifferentToolsSharingALabel`,
`TestRenderSplitsEditFromReplace`, `TestRenderSuperGroupSketchStates`; the group-member suite with
helper `runGroup` — `TestRenderGroupsBodyCarryingCalls`, `TestGroupHeaderCountIsFaintAndInert`,
`TestGroupMemberKeepsItsSummaryAndClipsTheTarget`, `TestExpandedGroupMemberPaintsTheSketchShape`,
`TestSeeLessFooterClosesAnOpenBody`, `TestGroupMemberMarksNameTheirOwnCalls`,
`TestExpandedMemberGutterIsNotTheSubAgentRail`.

Binding: the widely shared helpers `readCall` and the golden-row builders
(`groupMemberLine`/`leaderEdgeRow`/`memberEdgeRow`/`seeLessFooterLine`) STAY in `render_test.go` —
they have callers in other test files already; only `runGroup` moves (its one outside caller in
the diff-detail suite keeps compiling cross-file).

**Files:** `internal/tui/render_test.go`, `internal/tui/toolleader_test.go`,
`internal/tui/toolblock_test.go`

**Tests:** the moved tests themselves — no new tests.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `gofmt -l internal/tui` prints
nothing; each moved test name appears exactly once under `internal/tui/`.

**Commit:** `test(tui): carve the tool leader and block suites out of render_test.go`

---

## 9. Carve toolbranch_test.go and toolshape_test.go out of render_test.go

**What:** Create `internal/tui/toolbranch_test.go` (~715 lines) and
`internal/tui/toolshape_test.go` (~485 lines); move verbatim from `render_test.go`:

To `toolbranch_test.go`: the detail/diff suite — `TestRenderGroupWithInFlightMember`,
`TestRenderSingleCallSharesTheGroupShape`, `TestRenderMultiDetailStandalone`,
`TestRenderDiffDetailStandalone`, `TestRenderDiffMatchesLayoutSketch`,
`TestRenderDiffStatSurvivesTheBodyCap`, `TestCollapsedPaintTruncatesRetainedBodies`,
`TestExpandedBlockPaintsItsWholeBody`, `TestExpandedBlockLiftsItsDetailTone`,
`TestDiffLinesKeepTheirColourInBothBlockStates`, `TestCollapsedBlockStandsAtMostTwoRows`,
`TestClippedTargetAloneIsNoToggleTarget` (binding — stays with this suite); the ask-user suite
with helper `askUserCall` — `TestAnsweredAskUserBlockPaintsTheRecord`,
`TestAnsweredAskUserBlockIsAToggleTarget`, `TestAnsweredAskUserBlocksNeverGroup`.

To `toolshape_test.go`: `TestRenderOneLineOutputRidesTheBranch`,
`TestRenderGroupsOneLineOutputCalls`, `TestRenderInFlightStandalone`,
`TestRenderNoTargetStandalone`, `TestRenderNoTargetKeepsItsSummary`,
`TestTargetlessBlocksCollapseToTheBudget`, `TestEveryToolShapeCollapsesInsideTheRowBudget`
(binding — cross-cutting over every shape, lives with the shape suite),
`TestUnregisteredCallLabelsItsArguments`, `TestRenderGroupBreakers`.

The helpers these suites call that live elsewhere (`readCall`, golden-row builders,
`blockMarks`, `firingBlock`) stay where they are — same package, cross-file calls compile.

**Files:** `internal/tui/render_test.go`, `internal/tui/toolbranch_test.go`,
`internal/tui/toolshape_test.go`

**Tests:** the moved tests themselves — no new tests.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `gofmt -l internal/tui` prints
nothing; each moved test name appears exactly once under `internal/tui/`.

**Commit:** `test(tui): carve the tool branch and shape suites out of render_test.go`

---

## 10. Carve blocktarget, startupbox and chromelayout suites; render_test.go becomes the shared core

**Depends on items 6, 7, 8, 9.**

**What:** Final carve; create three files and settle `render_test.go`'s residual shape:

To `internal/tui/blocktarget_test.go` (~720 lines): the block-target suite with type
`blockMark`, helpers `blockMarks`, `headerStar`, `branchIndicator` (their canonical home moves
here; callers left in other files compile cross-file) — `TestRenderMarksTheWholeBlock`,
`TestHeaderIndicatorFollowsTheBlockState`, `TestHeaderIndicatorIsStyledApartFromTheLabel`,
`TestRemainderCountRidesTheOutcomeSlot`, `TestPromptBlockIsOneClickSurface`,
`TestBlockMarksAgreeWithTheMouseMapping` (binding — stays with the target suite, not
`mouse_test.go`), `TestLiveBlockHeaderStarBlinks`.

To `internal/tui/startupbox_test.go` (~145 lines): helper `lineWithLogoAnd`;
`TestRenderStartupBox`, `TestRenderStartupBoxStackedFallback`.

To `internal/tui/chromelayout_test.go` (~205 lines): `TestInputContentRows`,
`TestInputContentRowsZeroWidth`, helper `widgetContentRows`,
`TestInputContentRowsMirrorsTheWidget`, `TestInputContentRowsMirrorsTheWidgetOnGeneratedDrafts`,
`TestPromptEditorRowsClampsTheWidgetCount` (binding — stays beside the `inputContentRows` suite
rather than moving into the existing `prompteditor_test.go`).

`render_test.go` afterwards holds ONLY: the shared helpers (`readCall`, the golden-row builders
`groupMemberLine`/`leaderEdgeRow`/`memberEdgeRow`/`seeLessFooterLine`, `firingBlock`); the firing
suite — `TestFiringBlockCollapsesToItsRemainderCount`, `TestFiringBlockHeaderNeverBlinks`,
`TestFiringBlockJoinsNoToolGrouping`; the whole-scrollback golden `TestTranscriptLayoutGolden`
(binding — deliberately cross-cutting, stays); and the preview suite with helpers
`streamingPreview`/`numberedLines` — `TestPreviewPaintsOnlyItsTail`,
`TestPreviewRowCountIsBounded`, `TestPreviewUnderTheBoundIsUnchanged`,
`TestPreviewOfAnEmptyBufferKeepsItsMarker`, `TestPreviewTailEdges` (~445 lines total). Trim its
import block to what remains. Remove the render_test.go-split bullet from `ISSUES.md`.

**Files:** `internal/tui/render_test.go`, `internal/tui/blocktarget_test.go`,
`internal/tui/startupbox_test.go`, `internal/tui/chromelayout_test.go`, `ISSUES.md`

**Tests:** the moved tests themselves — no new tests.

**Acceptance:** `go test ./internal/tui/ -count=1` passes; `gofmt -l internal/tui` prints
nothing; `wc -l internal/tui/render_test.go` is under ~500 lines; each moved test name appears
exactly once under `internal/tui/`.

**Commit:** `test(tui): render_test.go settles as the shared core after the split`

---

**Suggested version bump:** none required — doc/comment prose and test-file moves only, no
user-facing or behavioral change. If the owner wants the residual sweep visible in the release
trail, a micro bump is the most this warrants; the owner decides.
