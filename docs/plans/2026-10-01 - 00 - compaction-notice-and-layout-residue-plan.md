# Compaction-notice budget and TUI layout-call residue — plan

**Goal:** The compaction transcript's elision notice counts against its char budget, and every
Update arm outside `internal/tui/model.go` leaves layout to Update's tail, with a structural test
that keeps it so.
**Date:** 2026-10-01
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 81cde6a2
**Sources:**
- beads `apogee-transcript-notice-unbudgeted`, `apogee-arm-layout-calls-residue`
- `docs/reviews/2026-09-30 - audit-context-all.md` (finding C-01/M-01)
- `internal/tui/doc.go` — invariant "an arm mutates; Update's tail lays out and repaints"

**Ratified design calls:**
- **Scope:** the two P4 hygiene beads only (owner, 2026-10-01).

**Standing requirements:**
- skills: coding-standards
- `internal/tui` tests run only narrowed with `-run`; never a whole-package `-race` or `-cover` run.

**Out of scope:**
- Layout calls inside `internal/tui/model.go` (the doc.go invariant already names its kept ones).
- Any change to `Model.settle`, `settleFrame`, `frameKey` or `layout()` itself.
- Every other open bead.

**Regression check (2026-10-01, 81cde6a2):**
- 1: guard folded (fixture sizing pinned; exception case added).
- 2: guard folded (writer's decision: extended keep criterion + direct-caller tests; settled
  helper compares layout-owned state, not View(); `// geometry:` grep added to Acceptance).
- 3: guard folded (writer's decision; skills.go/workflow.go test files named); supersedes
  undo.go `noteRevert`'s doc comment ("… and re-lays the frame").
- 4: guard folded (writer's decision; direct `dismissReport` callers settle their copy).
- 5: guard folded (writer's decision; "outside an Update pass" premise dropped; stale prose
  rule); supersedes transcript.go `hasLiveStar`'s doc where it names `foldSpinnerTick` as a repaint.
- 6: guard folded (Bites fixture test; source-scan precedent is seams_guard_test.go).

## 1. Count the elision notice against the compaction budget — ✅ DONE (2026-10-01)

**What:**
**Goal:** when `renderBudgetedTranscript` elides middle messages, its output length is
≤ `maxChars` unless the protected prefix plus the most recent message plus the notice alone
exceed it; with nothing elided, output is byte-identical to before.
**Approach (assumed at the header base):** in `internal/context/compact.go`
`renderBudgetedTranscript`, after the backwards tail fill, when `keepFrom > prefixEnd` compute
the notice text for the elided count and, while `used + len(notice) > maxChars` and more than
the most recent message is kept, drop the oldest kept tail message (`keepFrom++`), recomputing
the notice (its digit count can change). The most recent message stays unconditional.
Fix for the stated contract (audit C-01/M-01): the notice was written on top of a full budget.
**Regression guard.** The new test's fixture is sized so the fix can pass it: each tail message
the fix drops renders ≥ the notice's length, and prefix + most recent message + notice ≤ budget.
A second case pins the stated exception: when prefix + most recent message + notice > budget,
only the most recent tail message is kept and the output exceeds the budget.
**Files:** internal/context/compact.go; internal/context/compact_test.go
**Read first:** internal/context/compact.go — renderBudgetedTranscript, renderMessage, Summarize;
internal/context/compact_test.go — TestRenderBudgetedTranscriptElidesMiddleKeepingPrefixAndTail,
TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage, TestCompactAppliesTranscriptBudget
**Tests:** new test in `internal/context/compact_test.go`: a budget the tail fills exactly, with
middle messages elided, sized per the guard — asserts `len(got) <= budget` and the notice present
(fails before the fix); a second case for the exception (most recent message kept, output over
budget); existing `renderBudgetedTranscript` tests stay green unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/context/`
- `go test -race -count=1 ./internal/context/`
**Closes:** apogee-transcript-notice-unbudgeted
**Commit:** `fix(context): count the elision notice against the compaction transcript budget`

## 2. Input-box arms leave layout to the tail — ✅ DONE (2026-10-01)

NOTES (2026-10-01): 12 belt calls dropped (showRecall, recallPastNewest, foldPaste, queueCommand, runDeferredCommands, confirmBoundary, answerBoundary, runCompact, stageInterjection, popDeferredCommand, popInterjection, foldSkillHintTick); stageChildMessage's call is the one kept, annotated `// geometry:` naming the detached flag, per the plan's folded keep criterion. foldSkillHintTick's doc comment is reworded because it claimed the fold re-lays the frame.
NOTES (2026-10-01): settled_test.go holds only the assertSettled helper. Each per-file test sits in its source's own _test.go (Go convention {source}_test.go): TestRecallWalkIsSettledByTheTail, TestPasteIsSettledByTheTail, TestBandRowIsSettledByTheTail, TestQueueArmsAreSettledByTheTail, TestCommandArmsAreSettledByTheTail. The Acceptance -run glob already picks all of them up.
NOTES (2026-10-01): the only direct test caller of a function that lost a call is schedule_test.go TestReportActivityHoldsWhileACommandIsQueued (queueCommand, runDeferredCommands). It reads no geometry, so it is unchanged and was added to the Acceptance -run alternation. It passes.

**What:**
**Goal:** no `.layout()` / `.refreshViewport()` call remains in `internal/tui/interject.go`,
`commandrun.go`, `recall.go`, `prompteditor.go`, `suggestband.go` except one that reads
geometry afterwards in the same arm or runs outside an Update pass; each kept call carries a
same-line comment beginning `// geometry:` naming what it reads or why the tail cannot see it.
**Approach (assumed at the header base):** per call site: delete it when the enclosing arm is
reached from `Model.Update` (whose deferred `settleFrame` lays out) and nothing after the call in
that arm reads offset, `transcriptRows`, a widget's bottom or a placed pane's rows. Otherwise keep
it and rewrite its trailing comment to the `// geometry:` form. Add a test helper (in a new
`internal/tui/settled_test.go`) that, given a model returned by `Update`, asserts its `View()`
equals the `View()` of a copy after an explicit `layout()` — "the tail already settled it".
Binding: behaviour does not change; a site whose removal breaks the settled assertion is kept.
**Regression guard.** Keep criterion extended (binds items 2–5): a call is also kept when the arm
writes state that layout()/refreshViewport() consumes but settleFrame does not observe through
frameKey or a stale height — e.g. the follow/detached flag or a scroll offset; its `// geometry:`
comment names that state. Also: Tests must cover every test that calls a function losing a call
directly (bypassing Update) — grep `\.<fn>(` in internal/tui/*_test.go for each such function and
add those tests to the item's Acceptance -run pattern.
Further (folded from the check): `stageChildMessage`'s call (interject.go, `detached = false`) is
kept under this criterion. The settled helper compares what `layout()` owns —
`viewport.Height()`/`YOffset()`, `input.Height()`, `m.painted`, `len(m.lines)` — not two `View()`
calls, which differ across a second boundary in stateRunning (the status clock reads `time.Now()`).
**Files:** internal/tui/interject.go; internal/tui/commandrun.go; internal/tui/recall.go;
internal/tui/prompteditor.go; internal/tui/suggestband.go; internal/tui/settled_test.go; any
`internal/tui/*_test.go` holding a direct caller of a function losing a call
**Read first:** internal/tui/model.go — Model.settle, Model.refreshViewport; internal/tui/interject.go
— stageChildMessage, bandShape; internal/tui/commandrun.go — queueCommand, runCompact;
internal/tui/model_test.go — newTestModel, step
**Tests:** for each file with a removed call, one test driving that arm through `Update` and
calling the settled helper; the files' existing tests stay green, and so does every test found by
`grep -n '\.<fn>(' internal/tui/*_test.go` for each function losing a call (a direct caller that
read geometry the dropped call laid settles its returned copy with `.layout()` first).
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -count=1 -run "^($(grep -ho '^func Test[A-Za-z0-9_]*' internal/tui/interject_test.go internal/tui/commandrun_test.go internal/tui/recall_test.go internal/tui/prompteditor_test.go internal/tui/suggestband_test.go internal/tui/settled_test.go | sed 's/^func //' | paste -sd'|')|TestTranscriptLayoutGolden)$" ./internal/tui/`
  — its alternation extended with the direct-caller tests the grep above finds.
- `grep -n '\.layout()\|\.refreshViewport()' internal/tui/{interject,commandrun,recall,prompteditor,suggestband}.go | grep -v '// geometry:'` prints nothing.
**Commit:** `refactor(tui): input-box arms leave layout to Update's tail`

## 3. Command arms leave layout to the tail — ✅ DONE (2026-10-01)

NOTES (2026-10-01): all 13 calls dropped, none kept (no arm writes follow/detached or scroll state, and none reads geometry after its call): schedule.go runSchedule, createSchedule, runScheduleStop, stopSchedule, acceptCycle; fork.go runFork, acceptFork; workflow.go closeWorkflowPrompt; undo.go noteRevert; skills.go noteSkillCatalog; confine.go runConfine; effort.go runEffortCommand; usage.go runUsageCommand. noteRevert's doc comment no longer claims it re-lays the frame; it names Update's tail ([Model.settle]).
NOTES (2026-10-01): one test per source file in its own _test.go: TestScheduleArmsAreSettledByTheTail, TestBackgroundPromptCloseIsSettledByTheTail (workflow_test.go), TestForkArmsAreSettledByTheTail, TestUndoNotesAreSettledByTheTail, TestSkillsListingIsSettledByTheTail (skillscmd_test.go), TestConfineNoteIsSettledByTheTail, TestEffortPickerIsSettledByTheTail, TestUsagePaneIsSettledByTheTail. workflows_test.go is unchanged.
NOTES (2026-10-01): the guard's direct-caller grep found one test: engineholds_test.go TestReleaseEngine_CloseWorkflowPrompt calls dismissWorkflowPrompt, which reaches closeWorkflowPrompt. It reads only the rebind, no geometry, so it is unchanged; it was added to the Acceptance -run alternation and passes. dismissReport, the guard's example, has no layout call in these files (it is not in usage.go), so it is outside this item.

**What:**
**Goal:** the item-2 rule holds for `internal/tui/schedule.go`, `workflow.go`, `fork.go`,
`undo.go`, `skills.go`, `confine.go`, `effort.go`, `usage.go`.
**Approach (assumed at the header base):** item 2's per-site rule and settled helper, applied to
these files. Depends on item 2 (the helper).
**Regression guard.** Item 2's extended keep criterion applies (arms writing follow/detached or
scroll state the tail does not observe keep their call, annotated). For every function losing a
call, grep `\.<fn>(` in internal/tui/*_test.go (e.g. dismissReport in mouse_test.go/usage_test.go)
and include those tests in the Acceptance -run pattern.
Further: if undo.go `noteRevert` loses its `m.layout()`, its doc comment ("… and re-lays the
frame") is superseded and rewritten in the same edit.
**Files:** internal/tui/schedule.go; internal/tui/workflow.go; internal/tui/fork.go;
internal/tui/undo.go; internal/tui/skills.go; internal/tui/confine.go; internal/tui/effort.go;
internal/tui/usage.go; any `internal/tui/*_test.go` holding a direct caller of a function losing a call
**Read first:** internal/tui/model.go — settleFrame, Model.settle; internal/tui/paintcache.go —
Model.frameKey; internal/tui/undo.go — noteRevert; internal/tui/schedule.go — runSchedule;
internal/tui/workflow.go — foldBackgroundPhase; internal/tui/skills.go — noteSkillCatalog; model_test.go — step
**Tests:** for each file with a removed call, one test (in that file's existing `_test.go` —
`skillscmd_test.go` for skills.go, `workflow_test.go` or `workflows_test.go` for workflow.go)
driving the arm through `Update` and calling the settled helper; every direct-caller test the
guard's grep finds stays green.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -count=1 -run "^($(grep -ho '^func Test[A-Za-z0-9_]*' internal/tui/schedule_test.go internal/tui/workflow_test.go internal/tui/workflows_test.go internal/tui/fork_test.go internal/tui/undo_test.go internal/tui/skillscmd_test.go internal/tui/confine_test.go internal/tui/effort_test.go internal/tui/usage_test.go | sed 's/^func //' | paste -sd'|')|TestTranscriptLayoutGolden)$" ./internal/tui/`
  — its alternation extended with the direct-caller tests the guard's grep finds.
**Commit:** `refactor(tui): command arms leave layout to Update's tail`

## 4. Pane arms leave layout to the tail

**What:**
**Goal:** the item-2 rule holds for `internal/tui/advicepane.go`, `thinkingpane.go`,
`reportpane.go`, `inspector.go`, `keymigration.go`, `actuation.go`.
**Approach (assumed at the header base):** item 2's per-site rule and settled helper. A pane
open/close moves a height the tail's `freshenTranscriptClamp` already re-lays. Depends on item 2.
**Regression guard.** Same as item 3 — extended keep criterion, plus direct-caller tests grepped
and added to Acceptance.
Further: if `dismissReport` loses its call, its direct callers that then read geometry settle the
dismissed copy first — `TestClickOnAVacatedRowSelectsNoTranscriptLine` (mouse_test.go:
`d := m.dismissReport(..).dismissReport(..); d.layout()` before `pointTranscriptRow`) and
`TestUsageKeysLeaveTheRestOfTheFrameAlone` (usage_test.go: `c := m.dismissReport(usageReport);
c.layout()` before stepping PgUp).
**Files:** internal/tui/advicepane.go; internal/tui/thinkingpane.go; internal/tui/reportpane.go;
internal/tui/inspector.go; internal/tui/keymigration.go; internal/tui/actuation.go;
internal/tui/mouse_test.go; internal/tui/usage_test.go
**Read first:** internal/tui/reportpane.go — dismissReport; internal/tui/mouse.go —
pointTranscriptRow; internal/tui/mouse_test.go — TestClickOnAVacatedRowSelectsNoTranscriptLine;
internal/tui/usage_test.go — TestUsageKeysLeaveTheRestOfTheFrameAlone; internal/tui/model.go —
Model.freshenTranscriptClamp, Model.refreshViewport; internal/tui/actuation.go — startProfileLoad;
internal/tui/inspector.go — runInspectCommand
**Tests:** for each file with a removed call, one test driving the arm through `Update` and
calling the settled helper; the two direct-caller tests above settle their dismissed copy, and
every direct-caller test the guard's grep finds (`dismissReport` also in advicepane_test.go,
reportpane_test.go, thinkingpane_test.go) stays green.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -count=1 -run "^($(grep -ho '^func Test[A-Za-z0-9_]*' internal/tui/advicepane_test.go internal/tui/thinkingpane_test.go internal/tui/reportpane_test.go internal/tui/inspector_test.go internal/tui/keymigration_test.go internal/tui/actuation_test.go | sed 's/^func //' | paste -sd'|')|TestTranscriptLayoutGolden)$" ./internal/tui/`
  — its alternation extended with the direct-caller tests the guard's grep finds (at least
  `TestClickOnAVacatedRowSelectsNoTranscriptLine|TestUsageKeysLeaveTheRestOfTheFrameAlone`).
**Commit:** `refactor(tui): pane arms leave layout to Update's tail`

## 5. Frame-driver sites: keep or drop, each annotated

**What:**
**Goal:** the item-2 rule holds for `internal/tui/spinner.go`, `runview.go`, `heartbeat.go`,
`width.go`, `prebound.go`.
**Approach (assumed at the header base):** item 2's per-site rule. doc.go names the run view's
`openRun`/`upRun` repaints as positioning calls to keep; `prebound.go`'s `m.layout()` /
`bound.layout()` and `heartbeat.go`'s `next.layout()` take item 2's rule like every other site.
Depends on item 2.
**Regression guard.** Same as item 3 — extended keep criterion (runview.go openRun/upRun
explicitly checked against it), plus direct-caller tests grepped and added to Acceptance.
Further: `bindToServer` (only via `switchToServer`) and `foldBeatMsg` (only via Update's `beatMsg`
arm) run inside an Update pass, so no call here is kept for running outside one. Every comment
naming a dropped call's function as a repaint site is rewritten (`grep -n 'foldSpinnerTick\|foldModeReport\|foldBeatMsg\|bindToServer' internal/tui/*.go`),
superseding e.g. transcript.go `hasLiveStar`'s doc, model.go `settle`'s doc, paintcache.go `blink`.
**Files:** internal/tui/spinner.go; internal/tui/runview.go; internal/tui/heartbeat.go;
internal/tui/width.go; internal/tui/prebound.go; comments only, as the guard's grep finds them:
internal/tui/transcript.go; internal/tui/model.go; internal/tui/paintcache.go
**Read first:** internal/tui/spinner.go — foldSpinnerTick; internal/tui/runview.go — pushView, upRun;
internal/tui/heartbeat.go — foldBeatMsg; internal/tui/prebound.go — bindToServer, preboundRefusal;
internal/tui/width.go — foldModeReport; internal/tui/transcript.go — transcript.hasLiveStar
**Tests:** for each file with a removed call, one test driving the arm through `Update` and
calling the settled helper; every direct-caller test the guard's grep finds stays green.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -count=1 -run "^($(grep -ho '^func Test[A-Za-z0-9_]*' internal/tui/spinner_test.go internal/tui/runview_test.go internal/tui/heartbeat_test.go internal/tui/width_test.go internal/tui/prebound_test.go | sed 's/^func //' | paste -sd'|')|TestTranscriptLayoutGolden)$" ./internal/tui/`
  — its alternation extended with the direct-caller tests the guard's grep finds.
**Commit:** `refactor(tui): annotate or drop the frame-driver layout calls`

## 6. Guard the invariant structurally and retire the residue note

**What:**
**Goal:** a test fails when any non-test `.go` file in `internal/tui` other than `model.go` calls
`.layout()` or `.refreshViewport()` without a same-line `// geometry:` comment, and
`internal/tui/doc.go`'s arm invariant no longer describes residue or names the bead.
**Approach (assumed at the header base):** add `TestArmsLeaveLayoutToTail` (in
`internal/tui/settled_test.go`), a source scan in the style of
`TestNoParallelTestSwapsAPackageSeam` (seams_guard_test.go, `packageGoFiles`). Edit
the doc.go paragraph "Invariant — an arm mutates…": drop the "is not yet everywhere … tracked by
apogee-arm-layout-calls-residue" sentence; state the `// geometry:` comment rule and name the test.
Prose rule: every comment in `internal/tui` naming the residue or the bead id
(`grep -rn "arm-layout-calls-residue\|belt layout" internal/tui`) is updated in this item.
Depends on items 2–5.
**Regression guard.** The scan matches any receiver (`m.layout()`, heartbeat.go `next.layout()`,
prebound.go `bound.layout()`) and reads the same-line comment via fset positions. Add
`TestArmsLeaveLayoutToTailBites`, in the style of `TestNoParallelTestSwapsAPackageSeamBites`: a
fixture source the scanner must flag (unannotated `m.layout()`/`next.layout()`) and pass (annotated).
**Files:** internal/tui/settled_test.go; internal/tui/doc.go
**Read first:** internal/tui/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam,
TestNoParallelTestSwapsAPackageSeamBites, packageGoFiles; internal/tui/doc.go — "Invariant — an arm
mutates; Update's tail lays out and repaints"; heartbeat.go — foldBeatMsg; prebound.go — bindToServer; model.go — Model.settle, Model.layout
**Tests:** `TestArmsLeaveLayoutToTail`; `TestArmsLeaveLayoutToTailBites` proves the scan flags an
unannotated call and passes an annotated one.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `go test -count=1 -run '^(TestArmsLeaveLayoutToTail|TestArmsLeaveLayoutToTailBites|TestModelNoBuilderByValue|TestTranscriptLayoutGolden)$' ./internal/tui/`
- `grep -rn "arm-layout-calls-residue" internal/tui` prints nothing
**Closes:** apogee-arm-layout-calls-residue
**Commit:** `test(tui): pin that arms outside model.go leave layout to the tail`
