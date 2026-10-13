# Open bugs sweep — plan

**Goal:** fix or advance every open `type: bug` issue in capcom as of 2026-10-13. Six are fixed and closed here; the two that only reproduce off this machine (windows/arm64 wire flake, demorig EPERM) get self-diagnosing failure output plus the most likely fix, and stay open until the owner confirms on the real environment.
**Date:** 2026-10-13
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** b718bda5

**Sources:**
- capcom issues: `esc-disarm-fold-time`, `waitquiet-readiness-proxy`, `e2e-unwaited-record-reads`, `default-wire-arm64-flake`, `conpty-reap-join-bound`, `demorig-eperm-root-cause`, `settings-source-mark-stale`, `glyphs-missing-from-fonts`
- `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §3, §4
- `docs/plans/archived/2026-10-12 - 02 - test-suite-stability-plan.md` items 1, 24–26 (helpers and patterns reused here)
- `docs/plans/archived/2026-08-29 - 01 - double-esc-abort-plan.md` (the original 1 s window)
- `docs/design/test-drivers.md` §Waiting; `docs/layout/settings-screen-layout.md`; `layout.md`
- MS Learn `ClosePseudoConsole`: from Win11 24H2 it returns at once; the output pipe closing is the documented "all clients gone" signal

**Ratified design calls** (owner, 2026-10-13, unless marked):
- **Double-press window:** the reset tick is the only disarm for both esc×2 (stop) and ctrl+c×2 (quit); no wall-clock compare. A second press folded before the tick always acts.
- **Unreproducible bugs:** diagnose and apply the likely fix; `default-wire-arm64-flake` and `demorig-eperm-root-cause` stay open for owner confirmation.
- **Settings mark:** mark and editability both follow the layered files on disk after every save and external reload.
- **Glyphs:** opt-in `ui.glyphs: unicode | safe`, default `unicode` (look unchanged). `safe` swaps the theme glyphs, the spinner and the `⟳ ⑂ ✗` literals plus the live ask pane's checked marker; task-list cards and recorded ask exchanges keep `[✔]` under `safe` because that marker is model-facing and persisted (writer, 2026-10-13, narrowing the owner's call for correctness); key hints stay as they are.
- **Safe glyph values** (writer, within the owner's call): leader `·`, assistant/skill `♦`, user/prompt `›`, interject `»`, presented `≡`, done `√`, collapsed `►`, modes plan `□` / ask-before `○` / allow-edits `√` / auto `►►`, schedule tag `@`, fork tag `+`, failure `x`, live ask pane checked `[x]`, spinner frames `| / - \` for every spinner style.

**Standing requirements:**
- skills: coding-standards
- Run tests narrowly: one package and a `-run` pattern per command, never `./...` or two heavy packages on one line, and never two test runs at once.

**Out of scope:**
- Changing the default glyphs, the key hints (`⇥ ⇧ ⌃ ⌥ ⌫ ⏎ ␣`) or the block eighths.
- WaitQuiet sites that settle a frame about to be captured (allowed by `docs/design/test-drivers.md` §Waiting).
- Every non-bug ready issue (`lsp-symbol-navigation`, `serve-http-driver`, …).
- `tasklist.doneMarker` (model-facing parse key, not a drawn glyph)

**Regression check (2026-10-13, b718bda5):**
- 1: guard folded (time.Time stays; every backdate folds the reset msg; window-wording rule over README, docs, layout.md and Go comments)
- 2: guard folded (all eight stopRun tests named; stopRun fails on the one wait; awaitIdlePrompt comment rewritten)
- 4: decision folded (Windows-only sharing test, Windows compile and CI lines) + guard folded (retry loop in an untagged rename.go; past-budget via testtime.Fixed)
- 5: guard folded (Acceptance names TestE2EAnthropicWireCompletesAToolLoop; strays in their own accessor, not Unmatched(); dump stops the session first)
- 6: decision folded (Windows-CI line) + guard folded (reap's closeJoinTimeout comments move to finalFrameJoinTimeout)
- 7: guard folded (startUnderPTY opens the pair itself; probe injected; linux-only owner scan; darwin vet; report rides the error)
- 8: guard folded (nil-edits fallback; server keys keep the boot entries; untargeted-project outranks note narrowed) — supersedes the "this run resolved" wording of the mark
- 9: recast (domain value only; registry row, live apply and manual entry move to item 11) + guard folded ("" reads as `unicode`); round 2: guard folded (Goal and Approach trimmed to the field, its default and `Validate`)
- 10: recast (tasklist `✔` out of scope) + guards folded (task-list parse keys and the persisted ask record stay constants; tests read the default set; Acceptance grep corrected to the 14 declarations at base) — supersedes the one-shared-checkbox note at toolregistry.go and tasklist.go; round 2: guard folded (Goal keeps `askCheckedMarker`/`askUncheckedMarker` constants for the task-list markers and recorded exchange; comment rule widened to every shared-checkbox comment, with its grep)
- 11: recast (gains the `ui.glyphs` registry row, live apply and manual entry; item 9's template and section guards land here) + guards folded (glyph set survives a scheme switch; width equals the unicode counterpart's); round 2: decision folded (task-list cards and recorded ask exchanges keep `[✔]` under `safe`; header **Glyphs:** call and "Safe glyph values" narrowed to the live ask pane) + guards folded (`newTheme(s)` keeps its signature, `newModel` sets the set; live apply clears the paint cache and the set joins `frameKey` — supersedes paintcache.go's "nothing else about a theme changes mid-session"; domain/config/cmd test pins join Files and Acceptance; safe spinner frames one cell; allow-edits `√` and live ask `[x]` checked)

## 1. Double-press gestures disarm only on their reset tick — ✅ DONE (2026-10-13)

NOTES (2026-10-13): consequential edit — internal/tui/e2e_test.go: made necessary by the new window rule (its "Ctrl+C twice within the window" comment restated)
NOTES (2026-10-13): consequential edit — .capcom/knowledge/decisions.md: made necessary by ADR 0099 (the decisions index gains its line)
NOTES (2026-10-13): cmd/apogee/e2e_smoke_test.go:562 (stopRun's doc comment, "escStopWindow, one second … measured when each press is FOLDED") is left for item 2, which rewrites that comment with stopRun

**What:**
**Goal:** in `internal/tui`, a second esc folded while the esc-stop gesture is armed stops the worker, and a second ctrl+c folded while the quit gesture is armed quits, however much wall-clock time passed between the two folds. Only the gesture's own reset message (matching generation) or the worker unwinding disarms it. A decision record states the new window definition.
Fixes `esc-disarm-fold-time`: the window was measured at fold time, so a loaded program re-armed instead of stopping.
**Approach (assumed at the header base):** in `internal/tui/model.go` `handleKey`, the `"esc"` case compares `time.Now()` against `m.lastEsc` with `escStopWindow`; make the stop condition "armed" (`!m.lastEsc.IsZero()`, or a bool if the status hint no longer needs the time) and drop the `now.Sub` compare. `escStopResetMsg{gen}` stays the only timed disarm. Do the same for `lastCtrlC` / `ctrlCGen` / `ctrlCQuitWindow` / `ctrlCResetMsg`. Keep `statusRight`'s armed hint keyed on the same state. Record the decision with `capcom know add --parent decisions` (next ADR number): the window runs from the first press's fold until its reset tick folds, and Bubble Tea's ordered message delivery puts a second press typed inside it ahead of the tick. Every doc line that describes the window as "within a second of the first press" says the new rule (grep `docs/` and `layout.md` for `escStopWindow`, `twice`, `double`, `Esc` near `second`).
**Regression guard.** Keep `lastEsc`/`lastCtrlC` as `time.Time` (zero = disarmed), never a bool, so the late-fold tests can backdate them. Every backdate in `TestModelStopKeys` (`lastEsc` at :1001, :1124; `lastCtrlC` at :1105, :1144) folds `escStopResetMsg{gen: m.escGen}` / `ctrlCResetMsg{gen: m.ctrlCGen}` instead, and the late-tick subtests refold that same msg after the re-arm. The wording rule is: every line, doc or comment, that says the second press is measured against a window or one second says the new rule — grep `escStopWindow|ctrlCQuitWindow|within the window|one second` over README.md, docs/, layout.md and *.go (README.md:170, model.go, approval.go, prompteditor_test.go, cmd/apogee/e2e_approval_test.go at base).
**Files:** internal/tui/model.go, internal/tui/model_test.go, .capcom/knowledge/decisions/ (new ADR via capcom), docs/manual/ (only lines that state the window), README.md, internal/tui/approval.go, internal/tui/prompteditor_test.go, cmd/apogee/e2e_approval_test.go (comments only)
**Read first:** internal/tui/model.go — Model.handleKey ("esc", "ctrl+c" cases), escStopResetMsg, ctrlCResetMsg, finishWorker, statusRight; internal/tui/model_test.go — TestModelStopKeys; internal/tui/interject_test.go — startStubWorker; docs/manual/commands.md — the "Cancelling is a double-tap" paragraph
**Tests:** in `TestModelStopKeys`: rewrite every subtest that backdates `m.lastEsc` or `m.lastCtrlC` past its window (:1001, :1105, :1124, :1144) to fold `escStopResetMsg{gen: m.escGen}` / `ctrlCResetMsg{gen: m.ctrlCGen}` instead; add "a late-folded second esc still stops" (arm, backdate `lastEsc` past the window, fold esc, assert the worker stopped) and the ctrl+c twin (backdate `lastCtrlC`, fold ctrl+c, assert quit). Keep "a stale reset tick never disarms a fresher arm".
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestModelStopKeys|TestRunningPlaceholderAnnouncesTheDoubleEsc|TestModelApprovalEscapeIsLiveBeforeArming' ./internal/tui/`
- `capcom check`
**Closes:** esc-disarm-fold-time
**Commit:** `fix(tui): double-press esc and ctrl+c disarm only on their reset tick`

## 2. stopRun presses esc×2 once — ✅ DONE (2026-10-13)

NOTES (2026-10-13): stopRun no longer waits for the armed hint between its two presses (the plan's "presses esc, esc, then waits for the idle prompt once"): the driver's escapeGap queues the second press long before the first fold's one-second reset tick can fire, so it always folds ahead of it; escStopArmedHint, whose only reader was that wait, is removed with it.
NOTES (2026-10-13): awaitIdlePrompt is kept (stopRun Fatalfs with the frame when awaitIdlePrompt(drv, tuitest.DefaultTimeout) is false); its doc comment is rewritten to drop [stopRunRetryBudget] and "another press to try". TestJudgeHostileRowsReadAsOneRow skips without its live-judge endpoint, so wrappedPaneFrame's stopRun path ran only under the other seven.

**What:**
Test-only follow-up to item 1. `stopRun` (`cmd/apogee/e2e_smoke_test.go`) presses esc, esc, then waits for the idle prompt once; drop `stopRunAttempts` and `stopRunRetryBudget` and the retry loop. Fix the stale "70 ms" in its doc comment (the driver's `escapeGap` in `internal/tuitest/driver.go` is the authority). Depends on item 1.
**Regression guard.** `stopRun` reaches eight tests, not seven: `TestE2EApprovalForcedLookSurvivesAutoMode`, `TestE2EApprovalKeysAreArmedAfterPaint`, `TestE2EConsolesDieWithTheirOwner`, `TestE2EOutcomeCancelledDelegationCarriesTheFailureTone`, `TestE2EHostileWrapsUnderItsOwnIndent`, `TestE2EStreamCancelKeepsWhatArrived`, `TestE2EStreamRepaintCeiling`, and `TestJudgeHostileRowsReadAsOneRow` (via `wrappedPaneFrame`). `stopRun` `t.Fatalf`s with the frame when `awaitIdlePrompt(drv, tuitest.DefaultTimeout)` is false, and `awaitIdlePrompt`'s doc comment (which names `[stopRunRetryBudget]` and "[stopRun] has another press to try") is rewritten, or the helper folded into `stopRun`, in the same edit.
**Files:** cmd/apogee/e2e_smoke_test.go
**Read first:** cmd/apogee/e2e_smoke_test.go — stopRun, awaitIdlePrompt, stopRunAttempts, stopRunRetryBudget, idlePromptHead; internal/tuitest/driver.go — escapeGap, Driver.Press; cmd/apogee/e2e_hostile_test.go — wrappedPaneFrame
**Tests:** the eight tests that reach `stopRun` are the tests: `TestE2EApprovalForcedLookSurvivesAutoMode`, `TestE2EApprovalKeysAreArmedAfterPaint`, `TestE2EConsolesDieWithTheirOwner`, `TestE2EOutcomeCancelledDelegationCarriesTheFailureTone`, `TestE2EHostileWrapsUnderItsOwnIndent`, `TestE2EStreamCancelKeepsWhatArrived`, `TestE2EStreamRepaintCeiling`, `TestJudgeHostileRowsReadAsOneRow`.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EApprovalForcedLookSurvivesAutoMode|TestE2EApprovalKeysAreArmedAfterPaint|TestE2EConsolesDieWithTheirOwner|TestE2EOutcomeCancelledDelegationCarriesTheFailureTone|TestE2EHostileWrapsUnderItsOwnIndent|TestE2EStreamCancelKeepsWhatArrived|TestE2EStreamRepaintCeiling|TestJudgeHostileRowsReadAsOneRow)$' ./cmd/apogee/`
**Commit:** `test(e2e): stopRun stops a run with one double esc`

## 3. Remaining WaitQuiet readiness proxies in the e2e tests — ✅ DONE (2026-10-13)

NOTES (2026-10-13): left as found, outside the item's named sites: the `next.WaitQuiet(settled)` before `openLastRun(next)` in TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume's resumed half, and the WaitQuiet before the /fork picker's Down and the /sessions picker's Down in the fork and background-switch tests — keys typed after a quiet screen on pickers the item did not name.

**What:**
Closes the rest of `waitquiet-readiness-proxy` and all of `e2e-unwaited-record-reads`. Use the helpers items 24–26 of the archived plan introduced (`awaitArmed`, `waitIdle`, `awaitRecorded` in `cmd/apogee/e2e_approval_test.go`); no new helper is needed.
- `toolsFoldDelegatedRun` (`e2e_toolsfold_subagent_test.go`): drop its trailing `WaitQuiet(settled)`; its callers wait on what they need. `TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume` keeps `drv` and runs `waitIdle(drv)` and `awaitRecorded(t, drv, sess.Home(), toolsFoldDelegateWrapUp)` before `sess.Quit()`; `TestE2EToolsUmbrellaFoldsInsideARunView` runs `waitIdle(drv)` before `openLastRun`.
- `TestE2EForkKeepsThePrefixAndSurvivesAResume` (`e2e_fork_test.go`): before `parentAndChild`, `drv.WaitFor` until `sess.sessionRecords()` holds 2 records (the inline pattern already in that file). The WaitQuiet before a whole-frame read stays.
- `e2e_reactions_test.go`: in `TestE2EGateAsksInTheTUI`, `TestE2EGateFailureIsReportedInTheTUI` and `TestE2EHooksFireFromTheTUI`, the `WaitQuiet` before `Type("a")` becomes `awaitArmed(drv)`.
- `e2e_background_clear_test.go` `stopAtTheConfirm`: delete the `WaitQuiet` between `WaitText(bgBoundaryConfirm)` and `Type("y")`, with a comment that the boundary confirm is unlatched (`confirmBoundary`, `picker.isArmed`), so the painted title is the readiness proof. Do not use `awaitArmed` there.
**Files:** cmd/apogee/e2e_toolsfold_subagent_test.go, cmd/apogee/e2e_fork_test.go, cmd/apogee/e2e_reactions_test.go, cmd/apogee/e2e_background_clear_test.go
**Read first:** cmd/apogee/e2e_toolsfold_subagent_test.go — toolsFoldDelegatedRun; cmd/apogee/e2e_approval_test.go — awaitArmed, waitIdle, awaitRecorded; cmd/apogee/e2e_fork_test.go — parentAndChild; cmd/apogee/e2e_support_test.go — e2eSession.sessionRecords; cmd/apogee/e2e_background_clear_test.go — stopAtTheConfirm; internal/tui/commandrun.go — confirmBoundary
**Tests:** the changed tests themselves.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '^(TestE2EToolsUmbrellaFoldInARunViewSurvivesAResume|TestE2EToolsUmbrellaFoldsInsideARunView|TestE2EForkKeepsThePrefixAndSurvivesAResume|TestE2EGateAsksInTheTUI|TestE2EGateFailureIsReportedInTheTUI|TestE2EHooksFireFromTheTUI)$' ./cmd/apogee/`
- the test(s) calling `stopAtTheConfirm`, by name, in a second command of the same shape
**Closes:** waitquiet-readiness-proxy; e2e-unwaited-record-reads
**Commit:** `test(e2e): wait on the record, the idle prompt or the armed pane, not a quiet screen`

## 4. Session record rename survives a Windows sharing violation — ✅ DONE (2026-10-13)

**What:**
**Goal:** a session save on Windows whose rename over the record fails with a sharing violation or access-denied (a reader holding the record open without FILE_SHARE_DELETE) is retried with short backoff for a bounded total time before the error surfaces; other errors and other platforms surface at once.
Likely cause of `default-wire-arm64-flake`: the test's `savedTranscript` poll opens the record while the app's `atomicWrite` renames over it. The issue stays open (owner confirms with repeated green runs on windows/arm64).
**Approach (assumed at the header base):** `atomicWrite` in `internal/session/store.go` calls `os.Rename`. Route that rename through a small helper: a `rename_windows.go` that retries on `windows.ERROR_ACCESS_DENIED` and `windows.ERROR_SHARING_VIOLATION` (the set and ~2 s budget Go's own `cmd/go/internal/robustio` uses), and a `rename_other.go` that calls `os.Rename`. The retry loop takes the rename func and the retryable predicate as parameters so it is testable on any OS.
**Regression guard.** Acceptance gains `GOOS=windows go test -c -o /dev/null ./internal/session/` and a Windows-CI line (not runnable on this host) `go test -count=1 -run 'Rename|AtomicWrite' ./internal/session/`; Tests gain a windows-only test (`rename_windows_test.go`) that holds the record open with os.Open (no FILE_SHARE_DELETE) while a save renames over it, releases it within the budget, and asserts the save succeeds. The retry loop (rename func, retryable predicate and budget as parameters) lives in an untagged `rename.go`, so `rename_test.go` reaches it on this host; the tagged files only wire the predicate and `os.Rename`. The "past the budget" case is a timeout meant to expire: `testtime.Fixed`, not `Budget`.
**Files:** internal/session/store.go, internal/session/rename.go, internal/session/rename_windows.go, internal/session/rename_other.go, internal/session/rename_test.go, internal/session/rename_windows_test.go
**Read first:** internal/session/store.go — atomicWrite, Store.save, Store.Save, Store.hold, lockPath; internal/testtime/testtime.go — Fixed, Budget; cmd/apogee/e2e_stream_test.go — savedTranscript
**Tests:** `rename_test.go` drives the untagged loop with a fake rename: retryable error then success → nil after 2 calls; retryable error past the budget → the last error (budget via `testtime.Fixed`); non-retryable error → returned after 1 call. Windows-only `rename_windows_test.go`: a reader holding the record open with `os.Open` (no FILE_SHARE_DELETE) while a save renames over it releases it within the budget, and the save succeeds.
**Acceptance:**
- `go build ./internal/session/` and `GOOS=windows go vet ./internal/session/`
- `GOOS=windows go test -c -o /dev/null ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Rename|AtomicWrite' ./internal/session/`
- Windows CI (not runnable on this host): `go test -count=1 -run 'Rename|AtomicWrite' ./internal/session/`
**Commit:** `fix(session): retry the record rename through a Windows sharing violation`

## 5. Default-wire e2e failures explain themselves — ✅ DONE (2026-10-13)

NOTES (2026-10-13): the dump reads the frame BEFORE `sess.stop()` (a stopped run has left the alternate screen, so a post-stop frame is blank) and the output and store directory after it; the item text listed `drv.Frame()` beside the post-stop reads.
NOTES (2026-10-13): strays are detected by wrapping the mux and asking `mux.Handler(r)` for an empty pattern, so both unknown paths (404) and known paths under a wrong method (405) are logged and still answered exactly as before; the unit test pins both. No new stubllm file — the accessor sits beside `Probes()` in log.go and doc.go's log.go line names the stray log.
NOTES (2026-10-13): consequential edit — internal/stubllm/doc.go: made necessary by the stray log added to log.go (the file map's log.go line lists its logs).
NOTES (2026-10-13): verified by a throwaway forced failure (reverted) that the dump prints requests, probes, the live frame and the session store's .json/.lock names; `default-wire-arm64-flake` is advanced, not closed.

**What:**
Test-only. In `driveWireLoop` (`cmd/apogee/e2e_wire_anthropic_test.go`) add a `t.Cleanup` that, when `t.Failed()`, logs: every `stub.Requests()` entry (N, wire, turn index, unmatched, time, last message), `stub.Probes()` and `stub.Unmatched()`, `drv.Frame()`, `sess.Output()`, the file names in the session store directory, and `runtime.GOOS/GOARCH` with `testtime.Scale()`. If `internal/stubllm`'s mux drops requests to unknown paths silently, record them so `Unmatched()` (or a new accessor) reports them. Advances `default-wire-arm64-flake`; does not close it.
**Regression guard.** The anthropic caller is `TestE2EAnthropicWireCompletesAToolLoop`; no test is named `TestE2EWireAnthropic`. Stray (unknown-path) requests are logged in their own slice behind a new accessor beside `Probes()` (e.g. `Strays()`), never in `s.requests` or `Unmatched()`: `log.go` keeps non-completions out of `Requests()` on purpose and ~14 test sites gate on `Unmatched()`; a new stubllm file joins `doc.go`'s map (`TestDocMapNamesEveryFile`). The dump cleanup is registered after `launchTUIOn` (LIFO: stub and driver still live) and calls `sess.stop()` (idempotent) before reading `sess.Output()` and the store directory, since the app writes `s.out` until then.
**Files:** cmd/apogee/e2e_wire_anthropic_test.go, internal/stubllm/ (only if 404 paths are not recorded; a new file joins internal/stubllm/doc.go's map)
**Read first:** cmd/apogee/e2e_wire_anthropic_test.go — driveWireLoop, assertRequestsOnWire; internal/stubllm/log.go — Probes, Unmatched; internal/stubllm/server.go — Server.Handler, Server.probe; cmd/apogee/e2e_support_test.go — e2eSession.start, e2eSession.stop
**Tests:** if `internal/stubllm` changes, a unit test that a request to an unknown path is reported by the new stray accessor and leaves `Requests()` and `Unmatched()` unchanged.
**Acceptance:**
- `go vet ./cmd/apogee/ ./internal/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestE2EDefaultWireStaysChatCompletions|TestE2EAnthropicWireCompletesAToolLoop' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/stubllm/`
**Commit:** `test(e2e): dump requests, frame and store when a wire loop fails`

## 6. Windows reap drains the ConPTY to EOF

**What:**
**Goal:** after the console process exits, `reap` on Windows joins the output reader until EOF (conhost closing its write end, the documented signal that the final frame is flushed), bounded only by a separate long safety constant for a wedged conhost; the explicit `Close` path keeps its short bound.
Fixes `conpty-reap-join-bound`: under load conhost's final flush arrived after the 5 s join and `CloseOutput` cancelled the read carrying it.
**Approach (assumed at the header base):** `Process.reap` in `internal/console/process_windows.go` calls `joinBefore(p.readerDone, now+closeJoinTimeout)` then `CloseOutput()`. Add `finalFrameJoinTimeout` (60 s) beside `closeJoinTimeout` in `internal/console/process_shared.go`, with a comment naming reader EOF as the flush signal and the bound as wedged-conhost protection only; use it in `reap`. Nothing waits on `reap` synchronously (`Close` joins `reaped` under its own deadline), so the longer wait has no user-visible cost. Leave `shutdown` / `Close` on `closeJoinTimeout`. This host cannot run Windows tests: the bite check runs on the Windows CI lane (`TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows`, `TestProcessCloseIsBoundedOnWindows`).
**Regression guard.** Acceptance gains a Windows-CI line (not runnable on this host) `go test -count=1 -run 'TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows|TestProcessCloseIsBoundedOnWindows' ./internal/console/`. Every comment naming `closeJoinTimeout` as reap's bound moves to `finalFrameJoinTimeout` (`grep -n closeJoinTimeout internal/console/*.go`; `reap`'s doc comment at process_windows.go:155 at base); `shutdown`'s and `Close`'s stay.
**Files:** internal/console/process_windows.go, internal/console/process_shared.go
**Read first:** internal/console/process_windows.go — Process.reap, Process.Close; internal/console/process_shared.go — closeJoinTimeout, Process.shutdown, joinBefore; internal/console/process_windows_test.go — TestProcessClosesTheOutputOnlyAfterTheFinalFrameOnWindows, finalFrameConsole, TestProcessCloseIsBoundedOnWindows
**Tests:** none new on this host; the two Windows tests above are the guard.
**Acceptance:**
- `go build ./internal/console/`
- `GOOS=windows go vet ./internal/console/` and `GOOS=windows go test -c -o /dev/null ./internal/console/`
- Windows CI (not runnable on this host): `go test -count=1 -run 'TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows|TestProcessCloseIsBoundedOnWindows' ./internal/console/`
**Closes:** conpty-reap-join-bound
**Commit:** `fix(console): reap drains the ConPTY reader to EOF before closing output`

## 7. demorig names which launch step refuses on EPERM

**What:**
**Goal:** when `StartTerminal`'s launch fails with EPERM, the error and log say which of three probe launches on the same pty also fail (`/bin/true` with no attrs, with `Setsid`, with `Setsid`+`Setctty`) and the controlling-terminal owner of that pty, so the next occurrence names the cause. The retry stays.
Advances `demorig-eperm-root-cause`; does not close it. A failure of the no-attrs probe means the environment refuses clone/execve; only `Setctty` failing means pty ownership.
**Approach (assumed at the header base):** `startRetryingEPERM` in `cmd/demorig/term.go` wraps `startUnderPTY` (creack/pty `StartWithAttrs`). On an EPERM attempt, run the three probes against the same slave path, and scan `/proc/*/stat` for processes whose `tty_nr` matches the slave's device number; append the result to the attempt's log line. Keep the probe in its own function so it is unit-testable without a failing launch.
**Regression guard.** creack/pty v1.1.24's `StartWithAttrs` closes both ends when `cmd.Start` fails, so `startUnderPTY` opens the pair itself (`pty.Open`, tty on stdio, `Setsid`/`Setctty`, `cmd.Start`) and on EPERM runs the probes on the still-open tty before both ends close; the probe is injected into `startRetryingEPERM` as a func so `TestStartRetryingEPERM`'s fake EPERMs launch nothing real. term.go builds for darwin in CI's cross job: the `/proc` owner scan sits behind `//go:build linux` with a non-linux stub reporting "owner unknown" (new files join `cmd/demorig/doc.go`'s map), and `true` resolves via `exec.LookPath`. The report rides the returned error, which still `errors.Is(syscall.EPERM)`; the Goal's "and log" is dropped (StartTerminal has no log line).
**Files:** cmd/demorig/term.go, cmd/demorig/term_test.go, cmd/demorig/doc.go, the new linux-only owner-scan file and its non-linux stub in cmd/demorig/
**Read first:** cmd/demorig/term.go — StartTerminal, startUnderPTY, startRetryingEPERM, startAttempts; cmd/demorig/term_test.go — TestStartRetryingEPERM; cmd/demorig/doc.go — file map; .github/workflows/ci.yml — cross
**Tests:** the probe on a healthy pty reports all three launches succeeding and lists no foreign owner. `TestStartRetryingEPERM` gains an injected probe and asserts the last EPERM error carries its report and still `errors.Is(syscall.EPERM)`.
**Acceptance:**
- `go vet ./cmd/demorig/`
- `GOOS=darwin go vet ./cmd/demorig/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'EPERM|Probe' ./cmd/demorig/`
**Commit:** `chore(demorig): probe which launch step refuses when a take start hits EPERM`

## 8. /settings source mark follows saves and reloads

**What:**
**Goal:** after any `/settings` save or reset (global or targeted) and after an external config-file change is reloaded, each row's source mark and editability reflect the layered files on disk; env and flag overrides from boot still rank first.
Fixes `settings-source-mark-stale`: the mark came from the boot resolution, so an edited default row read `(default)`.
**Approach (assumed at the header base):** `settingsHost.Rows()` (`cmd/apogee/wire_options.go`) calls `settingsRows(h.opts)` with the boot `Options`. `externalEdit` (`cmd/apogee/settingsedit.go`) already re-resolves both files into `e.baseline` (`projection()`, via `retake`/`refresh` after `Write`, `Reset`, `SaveTo`, `ResetIn`, and `changed` on watcher reload). Add a mutex-guarded getter for the baseline's `GlobalKeys`/`ProjectKeys`; `Rows()` copies `h.opts`, swaps in those two maps and keeps the boot `Overrides`. `settingEdit.source` / `settingsSourceOf` may stay (they agree). Update `docs/layout/settings-screen-layout.md`: the mockup row `false *      (default)` becomes `(global)` and the prose says the mark follows saves; same for any `docs/manual/` line stating the mark is fixed at launch.
**Regression guard.** `Rows()` falls back to `h.opts`' own `GlobalKeys`/`ProjectKeys` when `h.edits == nil` (the `settingsHost` literals in projectrules_test.go and settingsrows_test.go build none). The two keys `changed()` never reports (`settingKeyServer`, `settingKeySubAgentsServer`) keep their boot `GlobalKeys`/`ProjectKeys` entries, so value and mark agree. Once the mark follows disk, the untargeted-project case of `settingsOutranksNote` (internal/tui/settings.go) would call a just-applied project value outranked: drop or narrow it and update `TestSettingsPaneProjectRowSaysTheProjectConfigOutranksItAtTheNextStart`. Supersedes the boot-resolution wording rather than a closed list of sites: every comment or doc saying the mark reports the boot resolution says the new rule — `grep -rn 'THIS run\|this run resolved\|this run' cmd/apogee/settings*.go cmd/apogee/wire_options.go internal/tui/settings.go docs/manual/commands.md docs/layout/settings-screen-layout.md` (wire_options.go:389-397, settingsrows.go, settings.go at base).
**Files:** cmd/apogee/wire_options.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsrows.go, cmd/apogee/settingsrows_test.go, internal/tui/settings.go, internal/tui/settings_test.go, docs/layout/settings-screen-layout.md, docs/manual/commands.md
**Read first:** cmd/apogee/wire_options.go — settingsHost.Rows, settingsHost.landed; cmd/apogee/settingsedit.go — externalEdit.retake, externalEdit.changed, externalEdit.projection; cmd/apogee/settingsrows.go — settingsRows; internal/tui/settings.go — settingsOutranksNote, settingsNote
**Tests:** in `settingsrows_test.go`: a global-only key saved from a default row reads `(global)` and after reset `(default)`; an external save adding a key flips its mark; an env-overridden key keeps `(env)` after a save; a `settingsHost` with no edits still builds `Rows()` from its boot maps; `server` keeps its boot mark after a reload. In `internal/tui/settings_test.go`: the project-row outranks test follows the narrowed note.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestSettingsRows|TestSettingsHost' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettingsPaneProjectRow|SettingsOutranks' ./internal/tui/`
**Closes:** settings-source-mark-stale
**Commit:** `fix(settings): source mark follows saves and external reloads`

## 9. `ui.glyphs` preference value

**What:**
Recast at the regression check (2026-10-13).
**Goal:** `domain.UIPrefs` carries `Glyphs`, `unicode` (`DefaultUIPrefs`) or `safe`; `""` reads as `unicode`; `Validate` refuses any other value; no key, row or reader yet.
Part of `glyphs-missing-from-fonts`.
**Approach (assumed at the header base):** add a `Glyphs` field to `domain.UIPrefs` (`internal/domain/uiprefs.go`), its `unicode` default in `DefaultUIPrefs`, and its `Validate` case.
**Regression guard.** item 9 adds only the `domain.UIPrefs` `Glyphs` field, its default and `Validate` (with tests) — no registry row and no manual entry, so `TestEveryEditableSettingKeyHasAnApply` holds at this commit; retitle it "`ui.glyphs` preference value" and trim its Files/Acceptance to internal/domain. `""` parses as the default `unicode`, `ParseSpinnerStyle`'s posture: `validateSpinnerName` validates `domain.UIPrefs{Spinner: v}` with `Glyphs` zero, so rejecting `""` would refuse every `ui.spinner` edit.
**Files:** internal/domain/uiprefs.go, internal/domain/uiprefs_test.go
**Read first:** internal/domain/uiprefs.go — UIPrefs, DefaultUIPrefs, UIPrefs.Validate, UIPrefs.land; internal/domain/uivocab.go — ParseSpinnerStyle; internal/config/registry.go — validateSpinnerName; internal/domain/uiprefs_test.go — TestUIPrefsValidateSpeaksTheParsersSentences, TestUIPrefsSetLandsEveryKey
**Tests:** default is `unicode`; `safe` accepted; `""` accepted as the default `unicode`; any other value rejected by `Validate`.
**Acceptance:**
- `go build ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
**Commit:** `feat(config): ui.glyphs selects the unicode or font-safe glyph set`

## 10. Theme glyphs move onto the theme

**What:**
Recast at the regression check (2026-10-13).
**Goal:** every theme glyph (`glyphAssistant`, `glyphLeaderDot`, `glyphUser`, `glyphDone`, `glyphSkill`, `glyphPresented`, `glyphInterject`, `glyphCollapsed`, the four mode glyphs) and the literals `scheduleTagGlyph`, `forkTagGlyph`, the settings failure `✗` and the live ask pane's checked marker (`askChoiceRows`) are read from a glyph set carried on the `theme` struct; `askCheckedMarker`/`askUncheckedMarker` stay constants for the task-list markers and the recorded exchange; the default set holds today's values, so every golden frame and test is unchanged.
Behaviour-preserving refactor for item 11. Glyphs composed into other `const`s (`memberGutter`, `subAgentOpenMarker`, `tableDivider`) are out of scope and stay constants.
**Approach (assumed at the header base):** add a `glyphSet` struct and a `glyphs glyphSet` field to `theme` (`internal/tui/theme.go`); `m.th` already reaches every free renderer. Replace each package-level glyph const with its field; where a call site has no theme in reach, thread `th` as the function's existing theme-taking neighbours do, never a package var. Spinner frames are untouched here.
**Regression guard.** The tasklist `✔` is `tasklist.doneMarker`, a model-facing parse key persisted in Details — it is out of scope. `taskListDoneMarker`/`taskListOpenMarker` stay constants equal to tasklist's markers, independent of the display set. Only the live ask pane (`ask.go` `askChoiceRows`) reads the theme's checked marker; `askExchangeLines` keeps the constant `askCheckedMarker`, since `enrichWithResult` bakes it into persisted `toolOutcome.Details`. The ~170 const references in ~32 internal/tui `*_test.go` files read one default-set value (e.g. `defaultGlyphs.collapsed`) — tests only; call sites read `th`. This parts, under item 11's `safe`, the live ask checkbox from the task-list marker, superseding the one-shared-glyph note: rewrite every comment saying the task-list or recorded-exchange markers are the ask pane's own glyphs (at base ask.go:230-237, toolregistry.go:1377-1379, 1427-1431, 1855-1856, tasklist.go:36-39), found with `grep -nE 'askCheckedMarker|question pane already draws|checkbox glyph|pinned ASCII pair' internal/tui/*.go internal/tasklist/*.go`.
**Files:** internal/tui/theme.go, and the internal/tui files that reference the listed glyphs (grep each const name; expect ~12 files, ~60 sites), the ~32 internal/tui `*_test.go` files that reference them, internal/tasklist/tasklist.go (comment only)
**Read first:** internal/tui/theme.go — theme, newTheme; internal/tui/ask.go — askCheckedMarker, askChoiceRows; internal/tui/toolregistry.go — taskListDoneMarker, askExchangeLines;
internal/tui/toolleader.go — leaderRow (appends glyphDone then CutSuffix's it: both reads must take the same th); internal/tui/model.go — modeSymbol
**Tests:** no new behaviour; existing tests and goldens are the guard; tests read the default set's values (`defaultGlyphs.<name>`) where they read a const today.
**Acceptance:**
- `go build ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Theme|Glyph|Golden|Frame' ./internal/tui/`
- `grep -nE '^\s*(const\s+)?(glyph(Assistant|LeaderDot|User|Done|Skill|Presented|Interject|Collapsed|Mode[A-Za-z]+)|scheduleTagGlyph|forkTagGlyph)\s+=' internal/tui/*.go` prints nothing (14 hits at base)
**Commit:** `refactor(tui): carry the drawn glyphs on the theme`

## 11. Font-safe glyph set

**What:**
Recast at the regression check (2026-10-13).
**Goal:** with `ui.glyphs: safe` the TUI draws the safe values from the header's "Safe glyph values" call for every glyph item 10 moved and for every spinner style; with `unicode` (default) nothing changes. Each safe theme glyph measures as many cells as its unicode counterpart (`theme.measure`); the safe spinner frames are one cell for every style.
Depends on items 9 and 10. Closes `glyphs-missing-from-fonts`; under `safe` the task-list cards and recorded ask exchanges keep `[✔]` by design (model-facing and persisted), and the close reason names that residue — no follow-up issue.
**Approach (assumed at the header base):** a `safeGlyphs` set beside the default in `internal/tui/theme.go`; `newTheme(s)` keeps its signature and returns the default set, and `newModel` sets `th.glyphs` from `opts.UI.Glyphs` after construction; `internal/tui/spinner.go` returns the ASCII frames for every style when the theme's set is safe. Add one line to `layout.md` (glyph section) naming the setting and pointing at `docs/manual/configuration.md`.
**Regression guard.** item 11 also adds the `ui.glyphs` registry row beside `ui.spinner` in internal/config/registry.go, a live apply that switches the theme's glyph set (and repaints, per the paint-cache guard below), the `docs/manual/configuration.md` entry (what `safe` swaps; for terminals whose font lacks the default glyphs, e.g. VS Code with GPU acceleration off), and the registry/apply tests including `TestEveryEditableSettingKeyHasAnApply`; extend its Files and Acceptance accordingly. The row and its starter-template line go AFTER `ui.spinner` (section "Interface"); the template (`internal/config/defaults/config.yaml`, `ui:` block) and `uiConfig`'s yaml field join Files, or `TestTemplateMentionsEveryRegistryKey`/`TestRegistryFollowsTheTemplateOrder` go red. `applyColorScheme` carries `th.glyphs` across its `newTheme(s)` rebuild beside `measure`/`padPrompts` (or derives it from `m.opts.UI.Glyphs`), so a scheme switch keeps `safe`. Each safe glyph measures as many cells as its unicode counterpart (`►►` 2 like `⏵⏵`, `[x]` 3 like `[✔]`), not 1.
**Regression guard.** `newTheme(s)` keeps its signature (257 internal/tui test call sites): `newModel` sets `th.glyphs` from `opts.UI.Glyphs` after construction, in the posture of `measure`/`padPrompts`. The `ui.glyphs` apply is not a bare `Set`: it clears `m.transcript.paints` as `applyColorScheme` does, and the glyph set joins `frameKey`, so already-painted blocks repaint — superseding paintcache.go's "nothing else about a theme changes mid-session" (`paintKey` and `frameKey` comments; rewrite both).
The new key reds `TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase` (want list gains `ui.glyphs` at its template position; `uiKeyCases` gains an entry with a refused value), every resolved `UIPrefs` literal in internal/config/config_test.go (`wantUIDefault`, :289, :5365, :5636-5686 gain `Glyphs: "unicode"`) and `TestSettingsRowsFormatEffectiveValues` (a `ui.glyphs` pin; `fabricatedSettings` UI gains `Glyphs: "safe"`).
**Files:** internal/tui/theme.go, internal/tui/spinner.go, internal/tui/theme_test.go, internal/tui/spinner_test.go, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go, internal/config/registry.go, internal/config/registry_test.go, internal/config/defaults/config.yaml, internal/config/config.go, internal/domain/uiprefs.go (the `ui.glyphs` key), cmd/apogee/wire_settings_test.go, docs/manual/configuration.md, layout.md, internal/tui/paintcache.go, internal/tui/model.go (newModel), internal/domain/uiprefs_test.go, internal/config/config_test.go, cmd/apogee/settingsrows_test.go
**Read first:** internal/tui/settingsapply.go — settingsApplyLocal, applyColorScheme; internal/tui/paintcache.go — paintKey, frameKey; internal/tui/model.go — newModel, modeMarker;
internal/tui/spinner.go — spinnerSpecs; internal/domain/uiprefs_test.go — TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase; cmd/apogee/settingsrows_test.go — TestSettingsRowsFormatEffectiveValues
**Tests:** a frame rendered with `safe` contains none of `⋯ ✦ ❯ ⧖ ▤ ✓ ▶ ⊞ ◐ ⏵ ⟳ ⑂ ✗` nor any braille rune (U+2800–U+28FF), and contains the leader `·`; each safe theme glyph measures as many cells as its unicode counterpart, and `TestSpinnerFrameWidth` checks the safe frames at width 1; under `safe`, `modeMarker(allow-edits)` is `√ allow edits` and `modeMarker(auto)` `►► auto`, an allow-edits footer frame contains `√ allow edits` and no `✔ allow edits`, and `askChoiceRows` with a checked row yields `[x]`, never `[✔]`; `unicode` frame equals today's golden; a scheme switch under `safe` keeps the safe set; a `ui.glyphs` edit applied live swaps the set and an already-painted `▶`/`⋯` block repaints to `►`/`·`; `TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase` with `ui.glyphs` and its `uiKeyCases` entry; the config resolved-`UIPrefs` literals and `TestSettingsRowsFormatEffectiveValues` with the `ui.glyphs` pin; `TestEveryEditableSettingKeyHasAnApply` (with `ui.glyphs` in `settingKeysAppliedByTheRenderer`), `TestSettingsApplyLocalLandsEveryUIKeyThroughSet` (a `ui.glyphs` value pair), the registry row round-trip and the template tests hold.
**Acceptance:**
- `go build ./internal/tui/ ./internal/config/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Glyph|Spinner|Theme|SettingsApplyLocal' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestSettingsRows|TestEveryEditableSettingKeyHasAnApply|TestManualDocumentsEverySettingsKey' ./cmd/apogee/`
**Closes:** glyphs-missing-from-fonts
**Commit:** `feat(tui): font-safe glyph set behind ui.glyphs: safe`
