# Open bug fixes

**Goal:** Fix the two open `type: bug` issues — a race-detector report from a test that shares one Model across parallel subtests, and a load-dependent flake in `TestE2ESubAgentView` — and the same readiness misuse in the shared `expandLastBlock` helper. All three are test-side defects; no shipped behaviour changes.
**Date:** 2026-11-10
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1179ec61

**Sources:**
- `.capcom/issues/prompt-rowcount-memo-race.md`, `.capcom/issues/subagent-view-t18-flake.md`
- `.capcom/issues/random-ci-failures/subagent-view-golden-early.md` and commit `07024cd5` (the t17 fix — the pattern items 2–3 copy)
- `docs/design/test-drivers.md` §Waiting; `internal/tui/doc.go`; ADR 0011 (`capcom know show "ADR 0011"`)
- Commit `f65794ae` (made a `rowCountMemo` hit write `last`, exposing the shared-Model test)

**Ratified design calls (owner, 2026-11-10):**
- **Race fix:** test-side only. Production renders on the single Update goroutine, so `rowCountMemo` stays lock-free and its hit path unchanged; each parallel subtest builds its own Model, and `internal/tui/doc.go` gains the rule that a parallel subtest never shares a parent-built Model.
- **Flake scope:** fix the issue's two scenes and also `expandLastBlock` (WaitQuiet straight after Enter); other `WaitQuiet`-after-content-wait sites in `cmd/apogee` are out of scope.

**Standing requirements:**
- skills: coding-standards
- This box is a Pi 5: never `go test -race` over a whole heavy package or a pattern (`./internal/tui/...`, `./cmd/apogee/...`); always one package with an anchored `-run`, prefixed `GOMEMLIMIT=2GiB`; never two test runs at once. Pass this to every sub-agent.
- Durations go through `internal/testtime`; `WaitQuiet` never stands in for readiness (`docs/design/test-drivers.md` §Waiting).

**Out of scope:**
- Any change to `rowCountMemo`, `paintCache`, `fileCache` or `thinkingRowCache` (no locks, no read-only-hit rework).
- The other `WaitQuiet` sites in `cmd/apogee` (`e2e_delegation_test.go`, `e2e_toolsfold_subagent_test.go`, `e2e_subagent_stop_test.go`, `e2e_parallel_test.go`).

**Regression check (2026-11-10, 1179ec61):**
- 1: guard folded (writer's decision + reviewer guards: four parent-Model readers added to Files, anchored -race run added to Acceptance)
- 2: guard folded (writer's decision + reviewer guards: t18 receipt constant, Goal narrowed to scenes 4 and 5)
- 3: guard folded (writer's decision + reviewer guards: indicator-glyph condition, esc tail kept, three call sites in two tests, Acceptance check for the Goal's no-WaitQuiet clause)

## 1. Parallel tui subtests stop sharing a parent-built Model — ✅ DONE (2026-11-10)

NOTES (2026-11-10): a go/types sweep of internal/tui (every t.Run FuncLit that calls t.Parallel, flagging any variable declared outside it whose type holds a tui.Model) found exactly the five plan-named sites before the fix and none after; the issue's other reported tests were collateral of the same race.
NOTES (2026-11-10): the workflows case table now carries `at func(paneTop int) (x, y int)` and a new `namingRowModel` helper builds the Model and paneTop inside each subtest; TestPanelWriteKeyUnderSymlinkedRoot's cases carry the workspace string instead of a Model.

**What:** Fixes bug `prompt-rowcount-memo-race` (exposed by `f65794ae`): `TestWorkflowsViewClickIsSwallowedByTheNameRow` builds `base` in the parent and two `t.Parallel()` subtests each run `stepCmd(t, base, …)`; the Model's pointer-held memos (`promptEditor.rowCount`, `transcript.paints`) are then written from two goroutines.

**Regression guard.** A static sweep of internal/tui finds this test the only parallel subtest stepping a shared Model; the issue's other reported tests were collateral of the same race. The close reason for prompt-rowcount-memo-race records that, and the Acceptance's second and third commands stand in for the whole-package -race run this box forbids.
The Goal also binds four race-free parallel readers of a parent Model — `TestListSurfaceArrowsAnswerTheEndsByTheWrapFlag`, `TestListSurfaceArrowsAtTheBottomOfAFilteredList`, `TestListSurfaceVerdictsNameWhatThePaneMustDo` (`m` via `pressList` / `typedFilter`) and `TestPanelWriteKeyUnderSymlinkedRoot` (`tc.m.panelWriteKey`): give each subtest its own Model.

**Goal:** No parallel subtest in `internal/tui` reads or steps a Model value built outside it, and `internal/tui/doc.go` states that rule beside the ADR 0011 value-copy rule.

**Approach (assumed at the header base):** In `internal/tui/workflows_test.go`, move the construction of `base` into a small helper called inside each subtest (keep `t.Parallel()`). Read the race report's second stack once to confirm both stacks are the memo; any other test whose race stack shows a shared parent-built Model gets the same fix in this item. Add one paragraph to `internal/tui/doc.go`: the Model's caches are pointer-held and single-goroutine by design (no locks), so a parallel subtest builds its own Model and never closes over a parent's. Production code is untouched.

**Files:** internal/tui/workflows_test.go; internal/tui/listsurface_test.go; internal/tui/sidepanel_test.go; internal/tui/doc.go
**Read first:** internal/tui/workflows_test.go — TestWorkflowsViewClickIsSwallowedByTheNameRow, savePaneModel (paneTop for the "over the pane" row is computed from base today, so the per-subtest helper must yield it too); internal/tui/model_test.go — stepCmd;
internal/tui/prompteditor.go — rowCountMemo.rows; internal/tui/listsurface_test.go — pressList; internal/tui/sidepanel_test.go — TestPanelWriteKeyUnderSymlinkedRoot; internal/tui/doc.go — the "Invariant — the value-copied Model holds no self-referential no-copy type by value" paragraph (TestModelNoBuilderByValue)

**Tests:** The existing test is the bite: before the fix `-race -count=10` reports the race in roughly one run in three. The three `TestListSurface*` tests and `TestPanelWriteKeyUnderSymlinkedRoot` keep passing with a Model per subtest.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=10 -run '^TestWorkflowsViewClickIsSwallowedByTheNameRow$' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=3 -run '^TestWorkflows' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=3 -run '^(TestListSurface|TestPanelWriteKeyUnderSymlinkedRoot)' ./internal/tui/`
- `go vet ./internal/tui/`

**Closes:** prompt-rowcount-memo-race

**Commit:** `fix(tui): build a Model per parallel subtest so pointer-held memos are not shared`

## 2. TestE2ESubAgentView scenes wait on the rows they assert — ✅ DONE (2026-11-10)

NOTES (2026-11-10): re-derived from the assumption that the t18 flake was a frame captured before its last row painted. A 20x one-core -race run with only the scene 4/5 waits failed 1 in 20: the frame showed `scout ✓ … 1 tool call · done · steered by 1 message`. The human's message had reached the child's second request itself, because it was typed after the first read's receipt painted but before the child asked again. The child then answered without the second read. That one race explains both of the issue's failures (the collapsed-row check at ~L193 and the t18 golden's `Read (2)` / `4 lines`). Scene 3 now waits on the stub's request log (new `childSentToolResult`) for the child's tool-result request before `submit(drv, childMessage)`; the stub logs a request when it matches it, before the 3 s reply streams.
NOTES (2026-11-10): the non-failing row predicate is `firstRow(f, want) string` ("" when absent). It sits beside `holds` and `frameWhen` in e2e_subagent_view_test.go, not beside `rowContaining` in e2e_support_test.go, which is outside the item's Files. It mirrors rowContaining's first-row semantics, so each wait condition reads exactly the row its assertion reads.
NOTES (2026-11-10): scene 4 drops its WaitQuiet, since no golden or whole-frame read follows. Scene 5 waits on the condition, then runs WaitQuiet(settled), then takes the goldened frame through frameWhen with the same condition, so the golden is a frame that condition accepted.
NOTES (2026-11-10): the Acceptance command as written cannot finish on the Pi 5. One iteration takes ~36 s (unchanged from base 2cfebcaf), so 20 iterations exceed go test's default 10 m `-timeout` and panic. It was run with `-timeout 30m` added.

**What:** Fixes bug `subagent-view-t18-flake`: scene 4 (esc back to the parent, then the collapsed-row checks) and scene 5 (`openLastRun`, then the `t18-run-view-finished` golden) wait on `WaitText` + `WaitQuiet(settled)`, and those content waits do not cover the rows the assertions read, so under load the frame can be captured before the last row paints.

**Regression guard.** A WaitQuiet may follow the content wait only before a golden or a whole-frame read (docs/design/test-drivers.md §Waiting); any new non-failing row predicate reuses an existing package-main helper (rowsContain / rowsContaining) where one fits, and a new helper name must not collide with them.
Scene 5's receipt is t18's `4 lines` (`t18-run-view-finished.txt:10`), not `readReceipt` (`2 lines`, absent from t18): name it a new constant with a comment like `readReceipt`'s, and require ONE row holding both `secondRead` and it.

**Goal:** Every frame scenes 4 and 5 of `TestE2ESubAgentView` assert on or golden (`top`, `finished`) is the frame returned by a content wait whose condition includes each fact that frame's assertions read; the test passes 20 times under `-race` pinned to one core.

**Approach (assumed at the header base):** Copy the t17 fix (`07024cd5`): `frameWhen(t, drv, what, want)` returns the frame meeting the condition; `holds(f, text)` checks without failing. Scene 4: the condition requires a row holding both `longDelegate` and `steeredOutcome`, a row holding both `shortDelegate` and `siblingAnswer`, `holds(parentEcho)` and `childAnswer` absent — add a non-failing row predicate beside `rowContaining` (which fails the test and cannot sit inside a wait). Scene 5: the condition requires the crumb row ending `runViewHint` and not `runViewStopHint`, plus one row holding `secondRead` and the new `4 lines` receipt constant, `childMessage`, `childAnswer` and `childFinishedBox` — name any last-painting fact as a constant with a comment, as `readReceipt` is. Keep the existing `rowContaining` assertions and the golden on the returned frame. A `WaitQuiet` may remain only after the content wait, before the golden or a whole-frame read.

**Files:** cmd/apogee/e2e_subagent_view_test.go
**Read first:** cmd/apogee/e2e_subagent_view_test.go — frameWhen, holds, readReceipt; cmd/apogee/e2e_support_test.go — rowContaining, openLastRun; cmd/apogee/testdata/frames/t18-run-view-finished.txt;
cmd/apogee/e2e_hostile_test.go — rowsContain; cmd/apogee/e2e_livestate_test.go — rowsContaining

**Tests:** The test itself; no golden change expected (`cmd/apogee/testdata/frames/t18-run-view-finished.txt` unchanged).

**Acceptance:**
- `cd cmd/apogee && GOMEMLIMIT=2GiB APOGEE_TEST_TIME_SCALE=2 taskset -c 0 go test -race -count=20 -run '^TestE2ESubAgentView$' .`
- `go vet ./cmd/apogee/`
- `git diff --exit-code cmd/apogee/testdata/frames/`

**Closes:** subagent-view-t18-flake

**Commit:** `fix(e2e): wait on the asserted rows in TestE2ESubAgentView scenes 4 and 5`

## 3. expandLastBlock waits on the expansion, not on quiet — ✅ DONE (2026-11-10)

NOTES (2026-11-10): the indicator glyphs are package-main constants `glyphCollapsed` ("▶") and `glyphExpanded` ("▼") in e2e_support_test.go. They mirror the default glyph set in internal/tui/theme.go. The e2e tests run on the default set, so the safe set's ► is not counted. Neither name collides with an existing package-main identifier.
NOTES (2026-11-10): the pre-⏎ baseline is taken from the frame after the ⌥↑ repaint has landed, not before the ⌥↑. The block-cursor highlight changes styles only, never indicator glyphs. The esc + awaitRepaint(dropped) + WaitQuiet(settled) tail is unchanged.
NOTES (2026-11-10): the Acceptance -race -count=3 runs were given `-timeout 30m` as a precaution on the Pi 5. Both passed: Outcome in 98.8 s, Firing in 7.0 s.

**What:** Same defect class as item 2: `expandLastBlock` (`cmd/apogee/e2e_support_test.go`) presses Enter and then calls `WaitQuiet` as its only readiness signal — the form `docs/design/test-drivers.md` §Waiting forbids outright. Its three call sites — `TestE2EOutcomeSlotsCarryTheToolsVerdict` (two calls) and `TestE2EFiringMarksAnAbandonedFinalTurn` (not `t.Parallel`, fake schedule clock) — then read the frame.

**Regression guard.** A WaitQuiet may follow the content condition only where a caller goldens or reads the whole frame (docs/design/test-drivers.md §Waiting); any new helper name must not collide with existing package-main helpers (rowsContain / rowsContaining).
Key the condition to the indicator glyphs alone — the frame's ▶ count drops below, or its ▼ count rises above, the pre-Enter frame's (the collapsed row's `+N more lines` tail vanishes on expansion, so a ▶→▼ swap of that row never appears). Keep the esc + `awaitRepaint(dropped)` + `WaitQuiet(settled)` tail unchanged: every caller reads the whole frame.

**Goal:** `expandLastBlock` returns only after a content or repaint condition shows the block expanded; no `WaitQuiet` precedes that condition, and every caller still passes.

**Approach (assumed at the header base):** Wait on what the expansion changes — the block's fold indicator turning open and/or a repaint observed through `Screen().BytesWritten()` past a mark taken before Enter (as the house `awaitRepaint` helper does) — with `Driver.WaitFor` under `DefaultTimeout`. If callers pass nothing that identifies the block, take the condition from the frame before Enter (its ▶ count drops below, or its ▼ count rises above, the pre-Enter frame's) rather than adding parameters to every caller. The esc + `awaitRepaint(dropped)` + `WaitQuiet(settled)` tail stays: every caller reads the whole frame.

**Files:** cmd/apogee/e2e_support_test.go
**Read first:** cmd/apogee/e2e_support_test.go — expandLastBlock, awaitRepaint, scrollTranscript; cmd/apogee/e2e_outcome_test.go — TestE2EOutcomeSlotsCarryTheToolsVerdict;
cmd/apogee/e2e_schedule_test.go — TestE2EFiringMarksAnAbandonedFinalTurn; internal/tui/toolleader.go — leaderRow; internal/tui/theme.go — glyphExpanded, glyphs.collapsed

**Tests:** The two tests that call `expandLastBlock` (three call sites; `grep -n expandLastBlock cmd/apogee/*_test.go`).

**Acceptance:**
- `grep -n expandLastBlock cmd/apogee/*_test.go` — then for each calling test `T`: `GOMEMLIMIT=2GiB go test -race -count=3 -run '^T$' ./cmd/apogee/`
- `awk '/^func expandLastBlock/,/^}/' cmd/apogee/e2e_support_test.go | sed -n '/tuitest.Enter)/,/WaitFor/p' | grep WaitQuiet` prints nothing
- `go vet ./cmd/apogee/`

**Commit:** `fix(e2e): wait on the expanded block in expandLastBlock instead of quiet`
