# Test readiness and Model size

**Goal:** e2e keys are typed only after a real readiness signal; the filewatch stall and burst tests arm on a signal from the watcher instead of skipping under load; and `tui.Model` shrinks from ~121 KB to ~22 KB, with a size guard that pins it there.
**Date:** 2026-10-13
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0de7ee16

**Sources:**
- `docs/design/test-drivers.md` §Waiting (WaitQuiet settles a frame, never stands in for readiness)
- `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §2 (filewatch census row)
- ADR 0011 (Model copied by value), ADR 0041 D3 (filewatch polls), ADR 0062 (no exported test hooks)
- capcom issues: `e2e-waitquiet-picker-keys`, `filewatch-stall-test-skipped`, `tui-model-frame-size`

**Ratified design calls:**
- **e2e scope:** every WaitQuiet-then-key site in `cmd/apogee`, not only the issue's three, and `openLastRun`'s guard is strengthened (owner, 2026-10-13).
- **Model size scope:** full shrink — theme styles behind a shared immutable pointer and the five overlay text fields behind a copy-on-write pointer; the prompt editor stays a value (owner, 2026-10-13).
- **filewatch seam:** widen the unexported `Watcher.sampled` hook; no fake clock (the package header's "a fake would test the fake") (writer, 2026-10-13).

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi 5 (8 GB): never run `go test -race` or `-cover` over a whole package or pattern; only named tests (`GOMEMLIMIT=2GiB go test -race -count=1 -run '^(TestA|TestB)$' ./pkg/`), never two test runs at once. Pass this to every sub-agent.
- Every test duration goes through `internal/testtime` (`TestNoFixedTimingInTests`).
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).

**Out of scope:**
- `default-wire-arm64-flake`, `demorig-eperm-root-cause` (owner confirmation)
- WaitQuiet immediately before a Golden/frame capture (the allowed use)
- The prompt editor's `lineEditor` (`Model.input`) stays held by value

**Regression check (2026-10-13, 0de7ee16):**
- 1: guard folded (picker-key grep acceptance; plan-wide waitIdle idiom)
- 2: recast (openLastRun returns on the run view's crumb; e2e_outcome_test.go added; Goal made checkable)
- 3: guard folded (only the pre-first-submit WaitQuiet goes; six unlisted sites added; grep acceptance)
- 4: guard folded (spoiled-attempt retry instead of skip; grep and bite acceptance)
- 5: recast (unix test gates on the first still tick; hook installed before Start; CHANGELOG.md SIGSTOP-test pin kept)
- 7: guard folded (deep clone; picker_test.go added; approval.go projectRuleEdit comment rewritten)
- 8: guard folded (mouse_test.go added; in-place editor writes in mouse.go/settings.go go through edit)
- 9: guard folded (autotitle_test.go added; sessions_test.go setValue/renamedField sites converted)
- 10: guard folded (textarea.Model's Styles exempt; walk without type de-dupe)
- Second round (regression-4.md; ids here are post-split — the round-1 lines above use pre-split ids, whose 4-10 are now 5-11):
- 2: guard folded (code-line-aware awk acceptance replaces the blind -B1 grep; zero WaitQuiet in approveEdit; openWorkingRun's stale comparison removed by item 3, which owns e2e_subagent_view_test.go)
- 3: recast (split: post-launch/applied-note sites, depends on nothing; Goal covers test bodies, seat/request-extra launch helpers out of scope; awk acceptance)
- 4: recast (split from 3: Esc/resume sites, depends on items 1 and 2; the /skills Esc waits on waitIdle alone)

## 1. Pickers advance on a moved cursor, not a quiet screen — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the moved-cursor check compares the highlight's frame row as well as its text (new `paneCursorRow`, which `paneCursor` now wraps): in `TestE2EBackgroundSessionSwitchDoesNotHang` both saved sessions paint the identical row text ("A session over a background scout · just now · 1 msg"), so a text-only comparison against the pre-`Down` value never resolves.
NOTES (2026-10-13): the `/fork` site waits for `paneCursor` to contain `forkPrompt2` (the target row) rather than for a generic change; the `/sessions` sites use the shared `pickerDown(driven)` helper.
NOTES (2026-10-13): post-resume `WaitQuiet`s after `⏎` (e.g. `restoreOtherSession`'s after "resumed:", `restoreNewestSession`'s after the resumed note) were left untouched — the plan's items 3-4 own the submit-after-resume gates.

**What:**
**Goal:** in `cmd/apogee`'s e2e tests, every `/fork` and `/sessions` picker key follows the picker's painted title/hint, and every `Enter` after a `Down` follows a frame whose selected row (the `❯ ` cursor row) is the target row; no `WaitQuiet` is the only gate before a picker key.
**Approach (assumed at the header base):** the pickers are unlatched (`picker.isArmed`), so the painted title (`forkPickerTitle`) or hint (`"⏎ resume"`) already proves they accept keys: delete the `WaitQuiet` before the first key. Replace the `WaitQuiet` between `Down` and `Enter` with a `WaitFor` until `paneCursor` changes from its pre-`Down` value (or, for `/fork`, shows the second prompt), with `tuitest.Awaiting`. Move `paneCursor` (`e2e_width_test.go`) to `e2e_support_test.go` and make it take `driven`, so the PTY driver in `restoreOtherSession` shares it. Sites: `TestE2EForkKeepsThePrefixAndSurvivesAResume`, `TestE2EBackgroundSessionSwitchDoesNotHang`, `restoreOtherSession` (console), `TestE2EDelegationRecordSurvivesSIGKILL`, `restoreNewestSession`, `TestE2EPresentServesWithoutLeakingTheToken`, `TestE2ESmokeInProcess`.
**Regression guard.** Green tests alone cannot check the Goal: Acceptance adds a grep that no picker-title/hint `WaitText` is followed by a `WaitQuiet`. Plan-wide idiom shared by items 1-4 (this item touches `e2e_smoke_test.go`): after an applied note, approval or resume, wait with `waitIdle`; `WaitQuiet` only immediately before a frame capture.
**Files:** cmd/apogee/e2e_fork_test.go, cmd/apogee/e2e_background_clear_test.go, cmd/apogee/e2e_console_test.go, cmd/apogee/e2e_delegation_test.go, cmd/apogee/e2e_present_test.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_width_test.go, cmd/apogee/e2e_support_test.go
**Read first:** cmd/apogee/e2e_width_test.go — paneCursor; cmd/apogee/e2e_support_test.go — driven; cmd/apogee/e2e_console_test.go — restoreOtherSession; cmd/apogee/e2e_fork_test.go — TestE2EForkKeepsThePrefixAndSurvivesAResume;
cmd/apogee/e2e_background_clear_test.go — TestE2EBackgroundSessionSwitchDoesNotHang; cmd/apogee/e2e_delegation_test.go — restoreNewestSession, TestE2EDelegationRecordSurvivesSIGKILL;
internal/tui/popup.go — popupRowLinesAt (selected row = glyphs.user+" ")
**Tests:** the changed tests themselves.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EForkKeepsThePrefixAndSurvivesAResume|TestE2EBackgroundSessionSwitchDoesNotHang|TestE2EDelegationRecordSurvivesSIGKILL|TestE2EPresentServesWithoutLeakingTheToken|TestE2ESmokeInProcess)$' ./cmd/apogee/`
- the tests calling `restoreOtherSession` and `restoreNewestSession`, by name, in a second command of the same shape
- `grep -n -A1 -E 'WaitText\(("⏎ resume"|forkPickerTitle|"type to filter · ↑/↓ select · ⏎ resume")\)' cmd/apogee/*_test.go | grep -c WaitQuiet` prints 0
**Commit:** `test(e2e): advance pickers on the moved cursor, not a quiet screen`

## 2. openLastRun and resumed transcripts wait on a real signal — ✅ DONE (2026-10-13)

NOTES (2026-10-13): `waitIdle` is called inside `openLastRun` itself, as its first line, rather than by each caller — so every caller (seven call sites) gets the mark-after-idle guarantee; at the sites the plan named for a caller-side `waitIdle` before `openLastRun` (TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume's resumed half, TestJudgeDelegationStepCap, TestE2EOutcomeSlotsCarryTheToolsVerdict) the `WaitQuiet` is simply removed, with no duplicate `waitIdle` added.
NOTES (2026-10-13): the crumb string is a new const `runViewCrumbHead = "← main ›"` in e2e_support_test.go (the prefix of crumbText's `← main › <name>`); the two delegation callers that go straight into `scrollTranscript` (which reads its first page immediately) also gained a `WaitQuiet(settled)` before it, as frame reads.

Depends on item 1.

**What:** Recast at the regression check (2026-10-13).
**Goal:** `openLastRun`'s mark is taken after `waitIdle`; it returns on the run view's crumb; no `openLastRun`/`expandLastBlock` call follows a `WaitQuiet` line.
**Approach (assumed at the header base):** `openLastRun` (`e2e_support_test.go`) takes its bytes-written mark only after the caller's `waitIdle`, so no start-up paint can satisfy the guard; its trailing `WaitQuiet(settled)` becomes `drv.WaitText("← main ›")`, the run view's crumb. Callers that read a frame next (`TestE2EDelegationStepCap`'s `opened := drv.Frame()`, `TestJudgeDelegationStepCap`'s `FrameArtifact`, `TestE2EOutcomeSlotsCarryTheToolsVerdict`'s `drv.Frame()`) add `WaitQuiet(settled)` before the read. In `TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume`'s resumed half, `next.WaitQuiet(settled)` before `openLastRun(next)` becomes `waitIdle(next)`; same at `TestJudgeDelegationStepCap` and before `expandLastBlock` in `TestE2EFiringMarksAnAbandonedFinalTurn`. In `e2e_outcome_test.go`, `waitIdle` goes before its `openLastRun` and inside `approveEdit`, replacing its `WaitQuiet`.
**Regression guard.** adopt all four G lines of regression-1.md item 2 — openLastRun ends on the run view's crumb (WaitText of the crumb string the code emits) instead of WaitQuiet; callers that read a frame next add WaitQuiet(settled) before the read (the allowed capture use); add cmd/apogee/e2e_outcome_test.go to Files with waitIdle before its openLastRun and inside approveEdit (replacing its WaitQuiet), and TestE2EOutcomeSlotsCarryTheToolsVerdict and TestE2EDelegationStepCap to the first Acceptance command; Acceptance states TestJudgeDelegationStepCap skips without APOGEE_JUDGE_ENDPOINT/APOGEE_LIVE_ENDPOINT (compile-only here); reword the Goal to the checkable form ("openLastRun's mark is taken after waitIdle; it returns on the run view's crumb") and add a grep acceptance that no openLastRun/expandLastBlock call follows a WaitQuiet line; drop the false premise "every caller already waits on the run view's crumb or text". Plan-wide idiom shared by items 1-4 (state it in each item touching e2e_outcome_test.go or e2e_smoke_test.go): after an applied note, approval or resume, wait with waitIdle; WaitQuiet only immediately before a frame capture.
**Regression guard.** The `-B1` grep is blind across a blank or comment line (at base it misses `e2e_schedule_test.go`'s `WaitQuiet` before `expandLastBlock` and the one inside `approveEdit`): Acceptance uses the code-line-aware awk below (base: delegation:503, schedule:55, toolsfold:182) plus a zero `WaitQuiet` count in `approveEdit`. Every comment naming `openLastRun`'s wait is made true (`grep -n 'openLastRun' cmd/apogee/*_test.go | grep '//'`): this item keeps `openLastRun`'s own doc true; `openWorkingRun`'s false comparison ("openLastRun ends on a quiet screen …") is removed by item 3, which owns `e2e_subagent_view_test.go`.
**Files:** cmd/apogee/e2e_support_test.go, cmd/apogee/e2e_toolsfold_subagent_test.go, cmd/apogee/e2e_delegation_test.go, cmd/apogee/e2e_schedule_test.go, cmd/apogee/e2e_outcome_test.go
**Read first:** cmd/apogee/e2e_support_test.go — openLastRun, expandLastBlock; cmd/apogee/e2e_subagent_view_test.go — openWorkingRun; cmd/apogee/e2e_delegation_test.go — TestE2EDelegationStepCap;
cmd/apogee/e2e_outcome_test.go — TestE2EOutcomeSlotsCarryTheToolsVerdict, approveEdit; cmd/apogee/e2e_toolsfold_subagent_test.go — TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume;
cmd/apogee/e2e_schedule_test.go — TestE2EFiringMarksAnAbandonedFinalTurn
**Tests:** the changed tests and every other `openLastRun` caller.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume|TestE2EToolsUmbrellaFoldsInsideARunView|TestE2EFiringMarksAnAbandonedFinalTurn|TestE2EOutcomeSlotsCarryTheToolsVerdict|TestE2EDelegationStepCap)$' ./cmd/apogee/`
- every remaining `openLastRun` caller, by name, in a second command of the same shape; `TestJudgeDelegationStepCap` skips without `APOGEE_JUDGE_ENDPOINT`/`APOGEE_LIVE_ENDPOINT` (compile-only here)
- `awk 'FNR==1{p=""} /^[[:space:]]*(openLastRun|expandLastBlock)\(/&&p~/WaitQuiet/{print FILENAME":"FNR} !/^[[:space:]]*($|\/\/)/{p=$0}' cmd/apogee/*_test.go` prints nothing
- `awk '/^func approveEdit/,/^}/' cmd/apogee/e2e_outcome_test.go | grep -c WaitQuiet` prints 0
**Closes:** e2e-waitquiet-picker-keys
**Commit:** `test(e2e): open a run only after the idle prompt and the cursor's own paint`

## 3. Submits after a note wait on readiness, not quiet — ✅ DONE (2026-10-13)

**What:** Recast at the regression check (2026-10-13).
**Goal:** no `submit`/`Type` in a test body of this item's files has a `WaitQuiet` as its only gate; where `waitIdle` already precedes the first submit, the redundant `WaitQuiet` is gone.
**Approach (assumed at the header base):** after an applied-note `WaitText` (`TestE2EEgress`, `TestE2EReactionsReloadSwapsTheArmedList`), replace the `WaitQuiet` with `waitIdle(drv)`. Delete the `WaitQuiet` that follows `waitIdle` in `TestE2EFanOutCeilingRefusesTheOverflow`, `TestE2ESubAgentStop`, `TestE2EWorkflowStages`, `TestE2ESubAgentView`.
**Regression guard.** Split at the regression check into two items with disjoint Files: this one takes the post-launch/applied-note sites and depends on nothing; item 4 takes the Esc/resume sites. Only the `WaitQuiet` directly before the first submit goes (`TestE2ESubAgentStop`, `TestE2EWorkflowStages`, `TestE2ESubAgentView`, `TestE2EFanOutCeilingRefusesTheOverflow`); every `waitIdle`+`WaitQuiet` that precedes a `Frame`/`Golden` read stays. Also convert the unlisted sites: `TestE2EFanOutTrailerStatesTheWidth`, `TestE2ELiveStateLauncherMoveKeepsTheSessionWorking` (before `/model`), `TestE2EPopupFramesLists` (before `Type("/")`; its `WaitQuiet` before `Golden` stays, the allowed capture use). Acceptance uses the awk below, which skips blank/comment lines and stops at a `Frame`/`Golden` read (at base it lists exactly the 12 inline sites, ten here and item 4's two). The Goal covers test bodies only: the launch helpers `launchSeatSessionOn` (`e2e_seat_test.go`) and `launchRequestExtraSession` (`request_extra_test.go`) keep their `waitIdle`+`WaitQuiet`, out of scope as the 2026-10-12-02 plan left them deliberately. Rewrite `openWorkingRun`'s doc (`e2e_subagent_view_test.go`) without its comparison to `openLastRun`'s wait, which item 2 makes false: it states only why a working run waits on the crumb. Plan-wide idiom shared by items 1-4: after an applied note, approval or resume, wait with `waitIdle`; `WaitQuiet` only immediately before a frame capture.
**Files:** cmd/apogee/e2e_egress_test.go, cmd/apogee/e2e_reactions_test.go, cmd/apogee/e2e_fanout_test.go, cmd/apogee/e2e_subagent_stop_test.go, cmd/apogee/e2e_workflow_stages_test.go, cmd/apogee/e2e_subagent_view_test.go, cmd/apogee/e2e_livestate_test.go, cmd/apogee/e2e_popups_test.go
**Read first:** cmd/apogee/e2e_approval_test.go — waitIdle, promptPlaceholder; cmd/apogee/e2e_egress_test.go — TestE2EEgress; internal/tui/settingswatcher.go — foldConfigChanged (Apply runs before the applied note);
cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesLists; cmd/apogee/e2e_seat_test.go — launchSeatSessionOn
**Tests:** the changed tests themselves.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EEgress|TestE2EReactionsReloadSwapsTheArmedList|TestE2EFanOutCeilingRefusesTheOverflow|TestE2ESubAgentStop|TestE2EWorkflowStages|TestE2ESubAgentView)$' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EFanOutTrailerStatesTheWidth|TestE2ELiveStateLauncherMoveKeepsTheSessionWorking|TestE2EPopupFramesLists)$' ./cmd/apogee/`
- `awk 'FNR==1{n=0} /^[[:space:]]*($|\/\/)/{next} /^func /{n=0} {if(n>0){if($0~/Frame\(|Golden\(/)n=0; else if($0~/(submit|typeSubmit)\(|\.Type\(/){print FILENAME":"FNR;n=0} else n--} if($0~/WaitQuiet\(settled\)/)n=2}' cmd/apogee/*_test.go | grep -v -E 'e2e_(outcome|smoke)_test\.go:'` prints nothing (item 4's two sites may remain until it lands)
- `grep -n 'ends on a quiet screen' cmd/apogee/e2e_subagent_view_test.go` prints nothing
**Commit:** `test(e2e): submit after the idle prompt, not after a quiet screen`

## 4. Submits after an Esc or a resume wait on readiness, not quiet — ✅ DONE (2026-10-13)

Depends on items 1 and 2.

**What:** Recast at the regression check (2026-10-13).
**Goal:** no `submit`/`Type` in a test body of this item's files has a `WaitQuiet` as its only gate; after an Esc out of a view or a resumed-session note, the next submit follows `waitIdle`.
**Approach (assumed at the header base):** `restoreOtherSession`'s `"resumed:"` note (`e2e_console_test.go`) is followed by `waitIdle` instead of `WaitQuiet`. The Esc→`WaitQuiet`→submit sites in `TestE2EOutcomeSlotsCarryTheToolsVerdict` and `TestE2ESmokeInProcess` wait on readiness instead (see the guard).
**Regression guard.** Split at the regression check from item 3, with disjoint Files; this item shares `e2e_smoke_test.go`, `e2e_console_test.go` (item 1) and `e2e_outcome_test.go` (item 2), hence its Depends line. `/skills` in `TestE2ESmokeInProcess` is a transcript note, not a pane (`noteSkillCatalog`, `internal/tui/skills.go`) whose text stays in scrollback, so a `WaitGone` on it never returns: that site is Esc then `waitIdle(drv)` alone. `WaitGone` on the run view's crumb (`"← main ›"`) + `waitIdle`, as `closePane` / `TestE2EDelegationStepCap`'s Esc do, applies only at `TestE2EOutcomeSlotsCarryTheToolsVerdict`'s Esc. Plan-wide idiom shared by items 1-4: after an applied note, approval or resume, wait with `waitIdle`; `WaitQuiet` only immediately before a frame capture.
**Files:** cmd/apogee/e2e_outcome_test.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_console_test.go
**Read first:** cmd/apogee/e2e_approval_test.go — waitIdle; cmd/apogee/e2e_smoke_test.go — submit, typeSubmit, closePane, TestE2ESmokeInProcess;
cmd/apogee/e2e_outcome_test.go — TestE2EOutcomeSlotsCarryTheToolsVerdict; cmd/apogee/e2e_console_test.go — restoreOtherSession, TestE2EConsolesDieWithTheirOwner
**Tests:** the changed tests themselves.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EOutcomeSlotsCarryTheToolsVerdict|TestE2ESmokeInProcess|TestE2EConsolesDieWithTheirOwner)$' ./cmd/apogee/`
- `awk 'FNR==1{n=0} /^[[:space:]]*($|\/\/)/{next} /^func /{n=0} {if(n>0){if($0~/Frame\(|Golden\(/)n=0; else if($0~/(submit|typeSubmit)\(|\.Type\(/){print FILENAME":"FNR;n=0} else n--} if($0~/WaitQuiet\(settled\)/)n=2}' cmd/apogee/*_test.go` prints nothing
**Commit:** `test(e2e): submit after a closed view or a resume on the idle prompt`

## 5. The stall test arms on a still tick inside the Settle — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the still branch now reads the hook's own `pending` (`if reportAt.IsZero() || pending`) instead of repeating `before.Before(reportAt)`, so the bite is the one-word swap of `pending` for `after.Before(reportAt)` there; bite run red ("a change was reported when the burst had already been reported once"), reverted.
NOTES (2026-10-13): Acceptance `! grep -n 't.Skip\|testSettle / 5' internal/filewatch/filewatch_test.go` still matches line 75 — `writeBurst`'s Skipf, the burst test's skip that item 6 owns (not yet done); the stall test itself has no skip and no pre-arm sleep. The grep goes clean once item 6 lands.
NOTES (2026-10-13): spoiled-attempt retry exercised with a temporary hook that held the poll past the Settle on the pending tick: 2 forced spoils then passed, 3 forced spoils Fataled ("all 3 attempts settled the first write…"); each attempt's first write has a fresh length so it is a change on the size witness alone. No CHANGELOG entry: test-only change; item 6 carries `filewatch-stall-test-skipped`'s Closes.

**What:**
**Goal:** `TestWatchNeverSettlesOnASampleTakenBeforeAStall` contains no `t.Skip` and no pre-arm sleep; it arms on the first poll tick that sees no change while a Settle is pending, and still fails against a watcher that judges the Settle on the post-sample clock.
**Approach (assumed at the header base):** widen the unexported `Watcher.sampled func()` (nil in production, called in `poll` between the stat and `after := time.Now()`) to `sampled func(changed, pending bool)`, where `changed` is the sample differing from `last` and `pending` is `!reportAt.IsZero() && before.Before(reportAt)`, both computed before the call. The stall test's hook arms itself on the first `!changed && pending` call, then stalls 1.5×Settle and makes the second write as today. Remove `stallResult.late` and the `wroteFirst` skip. Update the existing `sampled` users in the package's tests to the new signature.
**Regression guard.** A loaded box can hold the poll past the Settle so the first write reports and no `!changed && pending` tick comes: the hook also signals a spoiled attempt (the first still tick after a change with `!pending`); the test drains that report and retries with a fresh first write, bounded (3 tries), and Fatals only after — no skip, no red on a correct watcher. Acceptance adds a grep for the removed skip/sleep and the bite check as a step.
**Files:** internal/filewatch/filewatch.go, internal/filewatch/filewatch_test.go
**Read first:** internal/filewatch/filewatch.go — Watcher.sampled, Watcher.poll; internal/filewatch/filewatch_test.go — TestWatchNeverSettlesOnASampleTakenBeforeAStall, stallResult, awaitChange, expectNoChange, testSettle/testDeadline/testQuiet;
internal/testtime/guard_test.go — TestNoFixedTimingInTests
**Tests:** the stall test, run several times, including the spoiled-attempt retry; bite check: with `poll` judging on `after`, it fails.
**Acceptance:**
- `go vet ./internal/filewatch/`
- `GOMEMLIMIT=2GiB go test -race -count=5 -run '^TestWatchNeverSettlesOnASampleTakenBeforeAStall$' ./internal/filewatch/`
- `! grep -n 't.Skip\|testSettle / 5' internal/filewatch/filewatch_test.go`
- bite: judge on `after` at `poll`'s still branch, keep `pending` on `before` — the stall test goes red; revert
**Commit:** `test(filewatch): arm the stall on a still tick inside the Settle, never skip`

## 6. Burst writes step-lock with the watcher's samples — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the gate is a `sampleGate` (held/release/opened channels + armed flag) installed before Start through a new `startSampledWatcher` (startWatcher now delegates to it) and `startGatedWatcher`, whose `gate.open` Cleanup is registered after Start's `w.Stop` so LIFO frees a held poll first; every gate wait is bounded by `time.After(testDeadline)`. `writeBurst` disarms the gate before its final write (so before letting the previous hold go) and now takes the gate; TwoWatchers' two schedules writes go through it.
NOTES (2026-10-13): beyond the plan's guard, the unix SIGSTOP test also gets item 5's bounded spoiled-attempt retry (`stallAttempts`, a fresh-length child write each attempt): an attempt is spoiled only when the first still tick is `!pending` AND at least a Settle passed since the observation (measured on the poll goroutine), so a stale-tick watcher (gap ~one tick) still blocks, takes the second write and reports twice. The SIGSTOP/child staging moved into a helper `stopWhileAChildWrites`; the child now writes via `printf %b` so the content is parameterised.
NOTES (2026-10-13): CHANGELOG.md pin kept, not reversed: the still-tick gate preserves "a second write inside the Settle still coalesces". Bites run red and reverted: `reportAt = tick.Add(w.Settle)` in poll → unix test red ("a change was reported when the burst had already been reported once", 3/3); a `w.report()` on every observed change → Coalesces and TwoWatchers red. Retry path exercised with a temporarily forced spoil: 2 forced → pass, 3 forced → Fatal "all 3 attempts held the poll…". The const-block rationale rewritten (the testSettle/3 sleep is gone). No CHANGELOG entry: test-only change.

Depends on item 5.

**What:** Recast at the regression check (2026-10-13).
**Goal:** `TestWatchCoalescesABurstIntoOneReport`, `TestTwoWatchersReportOnlyTheirOwnFile` and `TestWatchSettlesOnTheClockNotTheTick` contain no `t.Skip` and no sleep that a later write must land inside; each multi-write sequence advances only after the watcher has sampled the previous write, so a loaded box cannot settle it early.
**Approach (assumed at the header base):** a test helper gates the poll goroutine through the `sampled` hook: on a `changed` call it signals the test and blocks on a release channel; the test makes the next write, then releases. `writeBurst` and the TwoWatchers writes go through it; the unix SIGSTOP test's `Sleep(Fixed(Settle/3))` before its second write becomes a gate on the first still tick after the post-SIGCONT change (see the guard). Disarm and release the gate in `t.Cleanup` registered after `Start`, so `Stop` never deadlocks on a blocked hook.
**Regression guard.** adopt all five G lines of regression-2.md item 5 — the unix SIGSTOP test gates on the first STILL tick after the post-SIGCONT change (block, make the second write, release), so a stale-tick watcher reports twice and the test keeps its bite; burst helpers disarm the gate before releasing the previous hold after the final write, Cleanup disarm+release as backstop; install the hook before Start (startWatcher or a sibling takes the hook), with the gate's Cleanup registered after Start so LIFO runs it before w.Stop; drop the "(CheckLeaks covers the package)" parenthetical and bound the gate's waits with time.After(testDeadline); rewrite the testSettle const-block rationale in filewatch_test.go and add an Acceptance grep for no t.Skip in writeBurst and no Sleep before the unix test's second write. The CHANGELOG.md pin (SIGSTOP test "pins that a second write inside the Settle still coalesces") is KEPT, not reversed, because the still-tick gate preserves it — say so in the item.
**Files:** internal/filewatch/filewatch_test.go, internal/filewatch/filewatch_unix_test.go
**Read first:** internal/filewatch/filewatch.go — Watcher.poll, Watcher.sampled; internal/filewatch/filewatch_test.go — startWatcher, writeBurst, TestWatchCoalescesABurstIntoOneReport, testSettle/testDeadline const-block comment;
internal/filewatch/filewatch_unix_test.go — TestWatchSettlesOnTheClockNotTheTick; CHANGELOG.md — "measures its settle on the clock, not on the tick" entry
**Tests:** the three tests, run several times; the unix test still fails against a stale-tick (`reportAt` from the tick time) watcher, keeping the CHANGELOG.md pin.
**Acceptance:**
- `go vet ./internal/filewatch/`
- `GOMEMLIMIT=2GiB go test -race -count=5 -run '^(TestWatchCoalescesABurstIntoOneReport|TestTwoWatchersReportOnlyTheirOwnFile|TestWatchSettlesOnTheClockNotTheTick)$' ./internal/filewatch/`
- `! grep -n 't.Skip' internal/filewatch/filewatch_test.go` and `! grep -n 'time.Sleep' internal/filewatch/filewatch_unix_test.go`
**Closes:** filewatch-stall-test-skipped
**Commit:** `test(filewatch): step-lock burst writes with the watcher's samples`

## 7. Theme styles behind a shared immutable pointer — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the guard also refuses taking a style's address, overwriting `*th.themeStyles`, and a `themeStyles` literal outside `newTheme`, and is proven on a fixture (`TestThemeStylesAreWrittenOnlyByNewThemeBites`); `TestThemeHeaderFitsInAKilobyte` additionally fails on a `lipgloss.Style` field left on the header or a non-Style field on `themeStyles`. The guard reuses `packageGoFiles` from seams_guard_test.go.
NOTES (2026-10-13): the literal-Model tests are seven, not six (TestSlashMenuShadowsASkillIDThatIsACommandLine, TestNoSkillIDCanOutParseABuiltinVerb, TestFanOutCardSeatsARunHeadPerItem, TestJoinedInterjectionsRebasesSkillSpans, TestAutocompleteRowsStripEscapes, TestSettingsDisplayRowsInterleaveSectionHeaders, TestSettingsRowCellsStripEscapes); all pass against the nil styles pointer, so none reads a style.

**What:**
**Goal:** `unsafe.Sizeof(theme{}) < 1 KiB`; the theme's `lipgloss.Style` fields live in one struct built only by `newTheme` and reached through an embedded pointer, so `th.<style>` reads compile unchanged; no non-test code assigns a style field after `newTheme`, and a guard test enforces it.
**Approach (assumed at the header base):** split `theme` (`internal/tui/theme.go`) into the mutable header (`measure`, `glyphs`, `padPrompts`, colours, `spinnerStops`) and `type themeStyles struct` holding every `lipgloss.Style`, embedded as `*themeStyles`. `newTheme` builds `&themeStyles{…}`; `applyColorScheme` keeps replacing the theme wholesale. Guard test: reflect over `themeStyles`' fields and fail on any `.<field> =` assignment in a non-test file outside `newTheme`. A zero `Model{}` in tests holds a nil pointer — assert the six literal-Model tests read no style. Add a paragraph to `doc.go` naming the shared-immutable rule.
**Files:** internal/tui/theme.go, internal/tui/theme_test.go, internal/tui/doc.go
**Read first:** internal/tui/theme.go — theme, newTheme; internal/tui/settingsapply.go — applyColorScheme (wholesale m.th = newTheme(s), carries measure/padPrompts/glyphs); internal/tui/model.go — Model.th, newModel (th := newTheme);
internal/tui/model_test.go — TestModelFitsTheStack; internal/tui/settings_test.go — settingsDisplayRows / settingRowCells zero-Model{} cases (read no style);
internal/tui/doc.go — "[theme] holds the palette, glyphs, and styles" paragraph
**Tests:** the new guard; a copy-independence test (switch scheme on copy B, copy A's styles unchanged); existing `colorscheme_test.go` tests, `TestWidthAuthoritySurvivesAValueCopy`, `TestModelFitsTheStack`.
**Acceptance:**
- `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^(TestModelFitsTheStack|TestWidthAuthoritySurvivesAValueCopy|TestModelNoBuilderByValue)$' ./internal/tui/` plus the new tests and the `colorscheme_test.go` tests, by name
**Commit:** `perf(tui): share the theme's styles behind an immutable pointer`

## 8. A copy-on-write line-editor field for the list filters — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the type is named `copyOnWriteField` (field `editor`, constructor `newCopyOnWriteField`, read accessors `isBuilt`/`value`/`textWithCaret`, writer `edit`); `projectRuleEdit.edit` is gone and its two callers call `m.ruleEdit.field.edit`; the deep clone is `lineEditor.clone`, which rebuilds the grid through `seatCaret` and so resets the widget's vertical goal column — harmless for single-line fields, worth knowing for item 9's multi-line system-prompt editor.
NOTES (2026-10-13): picker.go, sessions.go and panes.go needed no edit — they only pass the filter to `renderFilterListPlaced`/`filteredListContent`, whose parameter type changed in listsurface.go; doc.go's invariant section (which item 7 extended for the theme) carries no copy-on-write-field paragraph yet.

**What:**
**Goal:** one unexported copy-on-write field type in `internal/tui/lineeditor.go` holds a `*lineEditor` whose zero value is an empty, unbuilt editor; every write builds a fresh copy and swaps the pointer, never writing through a shared one; `listSurface.filter` and `projectRuleEdit.field` use it; a snapshot `a := m` is unchanged after typing into the filter on `b`.
**Approach (assumed at the header base):** generalise `projectRuleEdit.edit(change func(*lineEditor) tea.Cmd)` (`approval.go`) into the type: nil-safe read accessors and `edit` that clones, applies `change`, stores the clone. Move `projectRuleEdit` and `listSurface.filter` (`listsurface.go`; read by the picker, session browser, schedule and panes) onto it. Tests reach the editor through the accessors, never `field.p.input` directly.
**Regression guard.** adopt the G — the type's clone gives the copy its own value grid (deep, not `next := *p`), the copy-independence test snapshots AFTER the first keystroke; and the item rewrites approval.go's projectRuleEdit comment claiming a replace keeps an older copy's field "as it stood" so it is true under the deep clone. `picker_test.go`'s `typedFilter` returns the new type (its assignments into `m.picker.filter` must compile); keep the read accessor named `value()` so the `.filter.value()` reads in `listsurface_test.go`, `sessions_test.go`, `keymigration_test.go` and `ruleEdit.field.value()` in `approval_test.go` compile unchanged.
**Files:** internal/tui/lineeditor.go, internal/tui/approval.go, internal/tui/listsurface.go, internal/tui/picker.go, internal/tui/sessions.go, internal/tui/schedule.go, internal/tui/panes.go, internal/tui/lineeditor_test.go, internal/tui/picker_test.go
**Read first:** internal/tui/approval.go — projectRuleEdit, projectRuleEdit.edit; internal/tui/listsurface.go — listSurface.filter, typeIntoOverlayFilter; internal/tui/lineeditor.go — lineEditor, newPopupField;
internal/tui/schedule.go — `m.picker.filter = lineEditor{}` reset; internal/tui/picker_test.go — typedFilter
**Tests:** copy-independence for the picker filter and the browser filter, each snapshotting after a first keystroke and typing a second on the copy; existing `projectRuleEdit` and filter tests.
**Acceptance:**
- `go vet ./internal/tui/`
- the new copy-independence tests and the existing filter/`projectRuleEdit` tests, by name, single-package `-run`
**Commit:** `perf(tui): hold list filters behind a copy-on-write editor`

## 9. The settings editor behind the copy-on-write field

Depends on item 8.

**What:**
**Goal:** `settingsPane.editor` is the item-8 copy-on-write type; a snapshot `a := m` is unchanged after editing a setting on `b`; settings edit, mouse drag and save behave as before.
**Approach (assumed at the header base):** convert `settingsPane.editor` (`settings.go`) and its readers in `settingsapply.go` and `mouse.go`; the `lineEditor{}` resets become the type's zero value.
**Regression guard.** `mouse_test.go` writes through the field (`m.settings.editor.caretToRune(head)` in the caret-seat table): route it through `edit`. Also convert the in-place writes in `mouse.go` `handleSettingsClick` / `handleSettingsMotion` / `settingsWheel` and `settings.go`'s `deleteSelection` call (`settingsDeleteSelection`) to `edit`, since `handleSettingsClick` reads `pre` from the same pointer.
**Files:** internal/tui/settings.go, internal/tui/settingsapply.go, internal/tui/mouse.go, internal/tui/settings_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/settings.go — settingsPane.editor, newSettingsEditor, settingsTextValue readers (textWithCaret, caretLine); internal/tui/mouse.go — handleSettingsClick (pre snapshot), handleSettingsMotion, settingsWheel;
internal/tui/settingsapply.go — the `m.settings.editor = lineEditor{}` reset; internal/tui/mouse_test.go — the caret-seat/shade table (caretToRune at head)
**Tests:** copy-independence for the settings editor; the existing settings edit and mouse-drag tests.
**Acceptance:**
- `go vet ./internal/tui/`
- the new test and the existing settings editor tests, by name, single-package `-run`
**Commit:** `perf(tui): hold the settings editor behind a copy-on-write field`

## 10. Rename and name buffers behind the copy-on-write field

Depends on item 8.

**What:**
**Goal:** `sessionBrowser.renameBuf` and `workflowsPane.nameBuf` are the item-8 type; snapshots are unchanged after a rename or name edit on a copy; rename and workflow naming behave as before.
**Approach (assumed at the header base):** convert both fields and their resets in `sessions.go` and `workflows.go`; read-only by-value pane copies (`browserListContent`, the workflows render helpers) keep working through the accessors.
**Regression guard.** `autotitle_test.go` writes `m.sessionBrowser.renameBuf.setValue(...)` directly (three sites): convert them; `sessions_test.go`'s `renameBuf.setValue` writes and the `renameBuf: renamedField(...)` literal (`renamedField` returns `lineEditor`) convert too.
**Files:** internal/tui/sessions.go, internal/tui/workflows.go, internal/tui/sessions_test.go, internal/tui/workflows_test.go, internal/tui/autotitle_test.go
**Read first:** internal/tui/sessions.go — sessionBrowser.renameBuf, the rename open (newPopupField seed), rename key handling (editKey, value, resets); internal/tui/workflows.go — workflowsPane.nameBuf, the naming open, naming key handling;
internal/tui/sessions_test.go — renamedField; internal/tui/autotitle_test.go — renameBuf.setValue sites
**Tests:** copy-independence for both buffers; existing rename and workflow-name tests.
**Acceptance:**
- `go vet ./internal/tui/`
- the new tests and the existing rename/name tests, by name, single-package `-run`
**Commit:** `perf(tui): hold rename and name buffers behind a copy-on-write field`

## 11. The Model's size is pinned

Depends on items 7, 9 and 10.

**What:**
**Goal:** `TestModelFitsTheStack` fails when `unsafe.Sizeof(Model{})` exceeds 32 KiB; a structural walk (reusing `TestModelNoBuilderByValue`'s walker) fails when more than one `textarea.Model` or any `lipgloss.Style` outside `viewport.Model` is value-reachable from `Model`; `internal/tui/doc.go` states both rules and the copy-on-write field as the way to add a text field.
**Approach (assumed at the header base):** lower `modelStackCeiling` to a 32 KiB budget in `model_test.go` and add the walk beside the builder guard.
**Regression guard.** The one allowed value-held textarea carries `lipgloss.Style` by value in its `styles Styles`: exempt Styles inside `textarea.Model` as well as `viewport.Model` (the rule is "no Style outside the viewport and the one textarea"). Walk without `TestModelNoBuilderByValue`'s `seen[typ]` de-dupe (a by-value type graph is acyclic) and count `textarea.Model` occurrences by path, so the ">1 textarea" check can fire.
**Files:** internal/tui/model_test.go, internal/tui/doc.go
**Read first:** internal/tui/model_test.go — TestModelNoBuilderByValue (walker), modelStackCeiling, TestModelFitsTheStack; internal/tui/prompteditor.go — promptEditor (embeds lineEditor by value); internal/tui/model.go — Model.viewport, Model.th;
internal/tui/doc.go — "Invariant — the value-copied Model holds no self-referential no-copy type by value" paragraph
**Tests:** the tightened and new guards.
**Acceptance:**
- `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^(TestModelFitsTheStack|TestModelNoBuilderByValue)$' ./internal/tui/` plus the new walk test, by name
**Closes:** tui-model-frame-size
**Commit:** `test(tui): pin the Model under 32 KiB with one value-held textarea`
