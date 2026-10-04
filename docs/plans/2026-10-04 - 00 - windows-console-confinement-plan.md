# Windows Console: final frame and confined launch — plan

**Goal:** A short-lived ConPTY Console keeps its last painted frame, a Windows Console can run Confined under the restricted Low token (so Auto opens it without a prompt), and the 2uh.6 hands-on check runs as driven Windows-host tests, leaving only a real-TUI tail for the owner.
**Date:** 2026-10-04
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0b03a680
**Sources:**
- `docs/adr/0059-*.md` — Bounds (Windows bullet, amended 2026-10-03; 2026-10-04 Amendment and Note)
- `docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md` — items 14–17 (ConPTY launcher, reap-time release, send demotion)
- `docs/design/confinement-execution-contract.md`, `docs/design/test-drivers.md`
- beads `apogee-conpty-final-frame-lost`, `apogee-6ef3`, `apogee-2uh.6` (read from `.beads/issues.jsonl` when `bd` is not on PATH)

**Ratified design calls** (owner, 2026-10-04):
- **2uh.6:** automate every step a Windows-host `go test` can drive; rewrite the bead to the real-TUI tail only; it stays open, owner-run.
- **Spike gate:** if a Low-integrity restricted-token child cannot run under a pseudoconsole (item 2), the run stops there; item 1 lands regardless.
- **CI:** the `test-windows` job also runs `./internal/console/...`.
- **Item 1 bite test:** the reviewer's 690 base-order runs on build 26200 lost no frame, so the looped `cmd /c echo` test is a regression guard only, never the bite test; the fix stays (drain before closing the output pipe) and its bite test asserts the ordering at a seam — the output read end is not closed until the reader reaches EOF — through an injected or fake pipe/closer, failing against the pre-item tree; the bead still closes.

**Regression check (2026-10-04, 0b03a680):**
- 1: guard folded (owner decision: seam bite test, looped test a regression guard only; drain-before-close ordering; launch-failure path keeps closing `c.out`)
- 2: guard folded (real-confiner `Close()` via `t.Cleanup`; spike probe passed on build 26200)
- 3: guard folded (real-confiner `Close()` rule; comment grep incl. `host.go`; `TestConsoleSend` in Acceptance); yields to ADR 0059 Bounds until item 5 amends it
- 4: guard folded (real-confiner `Close()` rule; Windows-only variant file, no `fakeConfiner`; comment rule + grep)
- 5: guard folded (POSIX-only send-wording sentences; read the Console sections whole)
- 6: guard folded (ci.yml step comment and building.md `make check` row)
- 7: guard folded (real-confiner `Close()` rule; Goal read at the tool seam; "Windows" in every test name; no jsonl hand edit without `bd`)

**Standing requirements:**
- skills: coding-standards
- Needs a Windows host. `go` is not on PATH on the owner's box: prepend `%LOCALAPPDATA%\Programs\go\bin`. `-race` is unsupported on windows/arm64 — Windows-host Acceptance runs with `-count=1` and no `-race`.
- Every Windows-only change keeps the other platforms building: Acceptance includes `GOOS=linux go vet` and `GOOS=darwin go vet` of the touched packages.
- Closeout's `make check` needs a race-capable host (Linux/macOS or CI); on windows/arm64 record that it ran elsewhere.
- Bubble Tea is v2 (`tea.KeyPressMsg`); no item touches the TUI.

**Out of scope:**
- Kill-on-denial for a confined Windows Console: the denial watch does not match Windows `Access is denied.` by design (`internal/platform/denialkill.go`), same as `terminal` on Windows.
- Closing the pre-join window of the `os/exec` Bash path (`teardown_windows.go`).
- Any TUI change.

## 1. Keep a short-lived ConPTY command's final frame

**What:** fix for `apogee-conpty-final-frame-lost`: when a short command exits, the reap path closes the pseudoconsole and then immediately closes the output pipe, cancelling the pending read before conhost's final flush is drained.
**Regression guard.** Ratified design call (owner, 2026-10-04) — **Item 1 bite test:** the reviewer's 690 base-order runs on build 26200 lost no frame, so the looped `cmd /c echo` test is a regression guard only, never the bite test; the fix stays (drain before closing the output pipe) and its bite test asserts the ordering at a seam — the output read end is not closed until the reader reaches EOF — through an injected or fake pipe/closer, failing against the pre-item tree; the bead still closes.
Ordering: `Close` closes the pseudoconsole and `c.in`, then waits (bounded) for the reader's EOF before closing `c.out` — or `reap` calls a new release step instead of `Close`; never let `reap`'s `Close` close `c.out` at once, which voids the fix. Close-only callers (`internal/tui/conpty_windows_test.go` `runConPTYChild`, platform tests) still end with `c.out` closed. `StartPseudoConsole`'s launch-failure path (`releaseConsole`, no reader) keeps closing `c.out`, so a failed launch leaks no read handle.
**Goal:** a Console running `cmd /c echo <marker>` always has `<marker>` in its ring after exit (repeated runs, no sleep in the test), and closing still completes within the existing `closeJoinTimeout` bound.
**Approach (assumed at the header base):** in `internal/platform/conpty_windows.go`, `(*PseudoConsole).releaseConsole` closes the pseudoconsole and `c.in` only; the read end `c.out` is closed after the reader reaches EOF (conhost exiting closes its write end), with a separate exported close step (e.g. `CloseOutput`) that the console layer calls after `joinBefore(readerDone)` and that `Close` still runs as the bounded fallback so no handle leaks. In `internal/console/process_windows.go`, `(*Process).reap` keeps calling `console.Close()` after `recordExit`, then waits for the reader (bounded by `closeJoinTimeout`) before the output close. `collectOutput`/`ring.close` in `process_shared.go` stay unchanged.
**Files:** internal/platform/conpty_windows.go; internal/platform/conpty_windows_test.go; internal/console/process_windows.go; internal/console/process_windows_test.go
**Read first:** internal/platform/conpty_windows.go — Close, releaseConsole, StartPseudoConsole, Read; internal/console/process_windows.go — reap, Close; internal/console/process_shared.go — joinBefore, closeJoinTimeout
**Closes:** apogee-conpty-final-frame-lost
**Tests:** bite test (seam): a `PseudoConsole`-named test in `internal/platform/conpty_windows_test.go` (or a `FinalFrame`-named one in `internal/console/`) that drives the release through an injected or fake pipe/closer and asserts the output read end is not closed until the reader reaches EOF — it fails against the pre-item tree. Regression guard only: `internal/console/process_windows_test.go` — `TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows`: 50 iterations of `cmd /c echo <unique marker>`, await exit and reader done, assert the marker in the stripped read; `internal/platform/conpty_windows_test.go` — a pseudoconsole-level twin over `startTestPseudoConsole`/`drainPseudoConsole`. The bounded-Close tests and `TestStartPseudoConsoleRefusesABadSpec` stay green; a Close-only call and a failed launch both leave `c.out` closed.
**Acceptance:**
- `go test -count=1 -run 'OnWindows|FinalFrame' ./internal/console/`
- `go test -count=1 -run 'PseudoConsole' ./internal/platform/`
- `GOOS=linux go vet ./internal/console/... ./internal/platform/...` and `GOOS=darwin go vet ./internal/console/... ./internal/platform/...`
**Commit:** `fix(console): drain a ConPTY's final frame before closing its output pipe`

## 2. Launch a pseudoconsole child under a restricted token (spike gate)

**What:** the platform half of `apogee-6ef3`, and the run's gate: if the tests below cannot pass because a Low-integrity restricted-token child does not run under a pseudoconsole, STOP the run here, revert the item, and add a dated NOTES line with the observed failure (Win32 error, build number) to this item and a comment on `apogee-6ef3`. Do not fall back to a weaker token.
**Regression guard.** Any test using a real Windows confiner (platform.NewConfiner) must Close() it via t.Cleanup — it journals labels under the real %USERPROFILE%\.apogee (winlabel.Home) and a leaked journal is replayed by the next session's Recover.
**Goal:** `platform.StartPseudoConsole` accepts an optional token; with one, the child runs under it inside the same Job Object, prints through the pseudoconsole, and is denied a write outside its labelled box.
**Approach (assumed at the header base):** add a `Token windows.Token` field to `PseudoConsoleSpec`. In `(*PseudoConsole).launch`, a non-zero token takes `windows.CreateProcessAsUser(token, …)` with the same `StartupInfoEx`, attribute list (`pseudoConsoleAttributes`) and flags (`EXTENDED_STARTUPINFO_PRESENT|CREATE_UNICODE_ENVIRONMENT|CREATE_SUSPENDED`); the zero token keeps `windows.CreateProcess`. `launch` duplicates the borrowed token (`DuplicateHandle`) for the call's lifetime and closes the duplicate, so a concurrent confiner `Close` cannot free it mid-launch. Job containment and `ResumeThread` order stay unchanged.
**Files:** internal/platform/conpty_windows.go; internal/platform/conpty_windows_test.go
**Read first:** internal/platform/conpty_windows.go — PseudoConsoleSpec, launch, pseudoConsoleAttributes; internal/platform/wintoken_windows.go — mintRestrictedLowToken; internal/platform/confiner_windows.go — tokenConfiner.Confine, Close; internal/platform/confiner_windows_test.go — newProbeConfiner; internal/platform/conpty_windows_test.go — pollJob
**Tests:** `TestStartPseudoConsoleRunsUnderARestrictedToken` — a token from `NewConfiner().Confine` on a temp box (its `Close()` registered with `t.Cleanup`); `cmd /c echo` reaches the output; a `cmd /c echo x > <path outside the box>` fails with output naming the denial and a non-zero exit, and the file does not exist; `pollJob` shows the tree torn down on `Kill`. Skip only when `NewConfiner` reports Windows confinement unavailable (pre-17763 floor), never otherwise.
**Acceptance:**
- `go test -count=1 -run 'PseudoConsole|TestWindowsTokenProbe$' ./internal/platform/`
- `GOOS=linux go vet ./internal/platform/...` and `GOOS=darwin go vet ./internal/platform/...`
**Commit:** `feat(platform): launch a pseudoconsole child under a restricted token`
**NOTES:** 2026-10-04 regression check — the reviewer's empirical probe on build 26200 passed the spike (CreateProcessAsUser + Low token under ConPTY echoes; outside write prints "Access is denied.", exit 1, no file).

## 3. Let a Windows Console open Confined

**What:** the console half of `apogee-6ef3`. Depends on item 2.
**Regression guard.** Any test using a real Windows confiner (platform.NewConfiner) must Close() it via t.Cleanup — it journals labels under the real %USERPROFILE%\.apogee (winlabel.Home) and a leaked journal is replayed by the next session's Recover. This item yields to ADR 0059 Bounds (Windows bullet, amended 2026-10-03, fail-closed Windows Console) until item 5 amends it; this item's CHANGELOG entry says so. Comment rule: every non-historical "cannot confine / no restricted-token path" sentence tied to Windows in a touched file is reworded — including `host.go`'s `consoleConfines` field comment — found with `grep -rniE 'windows.{0,80}(confin|restricted-token)|(confin|token).{0,80}windows' internal/console internal/platform/host.go internal/platform/platform.go`. The flip changes `console_send`'s Windows demotion to the `ConfineDemoteError` at this commit, so Acceptance runs `TestConsoleSend`.
**Goal:** on Windows, `console.Start` with a confined spec runs the Console under the token `Prepare` set, the host reports that Windows Consoles confine, and a confined `cmd` Console cannot write outside its box.
**Approach (assumed at the header base):** in `internal/console/process_windows.go`, drop the `spec.Confined` refusal in `Start` and the `SysProcAttr.Token != 0` refusal in `prepareLaunch`; `prepareLaunch` reads the token back from the prepared `exec.Cmd` (with Path, CmdLine, Dir, Env) and passes it on `PseudoConsoleSpec.Token`. A confined spec whose `Prepare` set no token still fails closed with `ErrConfinementUnavailable`. In `internal/platform/host.go`, `windowsRules` sets `consoleConfines: true`. Reword every comment that says a Windows Console cannot be confined (grep `-i 'windows' ` near `confin` in `internal/console/` and `internal/platform/platform.go` — `doc.go`, `Spec.Confined`, `Start` doc, `Registry.Open`, `Terminal.ConsoleConfines`).
**Files:** internal/console/process_windows.go; internal/console/process_windows_test.go; internal/console/registry_test.go; internal/console/doc.go; internal/console/process_shared.go; internal/console/registry.go; internal/platform/host.go; internal/platform/host_test.go
**Read first:** internal/console/process_windows.go — Start, prepareLaunch, launchCommandLine; internal/subprocess/subprocess.go — ConfinementHandoff; internal/console/registry.go — Registry.Open; internal/console/registry_test.go — TestRegistryOpenRefusesAConfinedConsoleOnWindows; internal/platform/host.go — windowsRules, hostRules.consoleConfines; internal/tools/console_send.go — unfencedSendError
**Closes:** apogee-6ef3
**Tests:** replace `TestProcessStartRefusesAConfinedSpecOnWindows` and `TestProcessStartRefusesARestrictedTokenOnWindows` with `TestProcessStartRunsAConfinedSpecOnWindows` (real confiner, its `Close()` registered with `t.Cleanup`; echo inside works, write outside denied) and `TestProcessStartRefusesAConfinedSpecWithoutATokenOnWindows`; turn `TestRegistryOpenRefusesAConfinedConsoleOnWindows` into its opening counterpart; `TestHostTerminalRulesPerPlatform` expects `wantConfineable: true` on windows.
**Acceptance:**
- `go test -count=1 ./internal/console/`
- `go test -count=1 -run 'TestHostTerminalRules' ./internal/platform/`
- `go test -count=1 -run 'TestConsoleSend' ./internal/tools/`
- the comment grep in the guard, read line by line
- `GOOS=linux go vet ./internal/console/... ./internal/platform/...` and `GOOS=darwin go vet ./internal/console/... ./internal/platform/...`
**Commit:** `feat(console): open a Windows Console confined under the restricted token`

## 4. Tools: confined Console journeys on Windows

**What:** the tool layer follows item 3. Depends on item 3.
**Regression guard.** Any test using a real Windows confiner (platform.NewConfiner) must Close() it via t.Cleanup — it journals labels under the real %USERPROFILE%\.apogee (winlabel.Home) and a leaked journal is replayed by the next session's Recover. The Windows variants use `platform.NewConfiner()` (skip unless `Capabilities().FSWrite`; `Close()` via an `io.Closer` assertion) or a fake whose `Confine` sets a token — never `terminal_test.go`'s `fakeConfiner`, which sets none and now fails closed — and assert `Confined` on the opened Console, not `confineCount`. They live in a new `internal/tools/console_open_windows_test.go` (Windows-only build; the untagged file would re-exec under landlock with no `TestMain` and lacks `cmd.exe` off Windows), named under `TestConsoleOpen_`/`TestConsoleSend_`. Comment rule: every comment in `internal/tools` that ties "cannot confine/never fenced" to Windows is reworded — `grep -rniE 'windows.{0,80}(confin|fenc)|(confin|fenc).{0,80}windows' internal/tools/console*.go`, plus the wrapped ones (`console_send.go` `unfencedSendError` doc, `console_send_test.go` `terminalRulesHost` doc).
**Goal:** `console_open` under a confinement box on Windows confines through the handle on the context and seeds the scratch env, `console_send` to an unconfined Console on Windows gets the same reopen-fenced advice as POSIX, and no tool comment says no Windows Console is ever fenced.
**Approach (assumed at the header base):** add `cmd.exe` variants beside `TestConsoleOpen_ConfinesThroughTheHandleOnContext` and `TestConsoleOpen_ConfinedConsoleCarriesTheSeededScratchEnv` (today `skipWithoutPOSIXShell`), driving the real host and the exact `console_open` arguments the model sends. With `consoleConfines` true, `unfencedSendError` in `console_send.go` takes the `ConfineDemoteError` branch on Windows; reword its type comment and the comment of `TestConsoleSend_UnconfinedConsoleIsDemotedWhereAConsoleCannotBeConfined` (which keeps its fake `terminalRulesHost`).
**Files:** internal/tools/console_open_test.go; internal/tools/console_open_windows_test.go; internal/tools/console_send.go; internal/tools/console_send_test.go
**Read first:** internal/tools/console_send.go — unfencedSendError, ConsoleSend; internal/tools/console_send_test.go — terminalRulesHost, withFSConfinement, openTestConsole; internal/tools/console_open_test.go — skipWithoutPOSIXShell; internal/tools/terminal_test.go — fakeConfiner; internal/platform/confiner_windows.go — NewConfiner, Close
**Tests:** the two Windows variants above (a real confiner, its `Close()` registered with `t.Cleanup`); a Windows-host test that an unconfined Console's `console_send` under a confinement box returns the reopen advice.
**Acceptance:**
- `go test -count=1 -run 'TestConsoleOpen|TestConsoleSend' ./internal/tools/`
- `GOOS=linux go vet ./internal/tools/...`
- the comment grep in the guard, read line by line
**Commit:** `test(tools): drive confined Console journeys on Windows`

## 5. Docs: a Windows Console can be fenced

**What:** the user- and design-facing half of `apogee-6ef3`. Depends on item 3.
**Regression guard.** The rule also covers every sentence that restricts the `console_send` `ConfineDemoteError` wording to POSIX (e.g. `confinement-execution-contract.md`:791, :834) — find with `grep -rnE 'POSIX.{0,40}(send|console_send)'` over the same paths. The line-based greps miss wrapped sentences, so read the Console sections whole (`configuration.md` ~2549-2563, `CONTEXT.md` ~1398-1408).
**Goal:** no shipped doc says a Windows Console cannot be fenced or that Auto fails closed for it; ADR 0059 carries a dated amendment recording the restricted-token ConPTY launch and that kill-on-denial stays POSIX-only.
**Approach (assumed at the header base):** amend ADR 0059 Bounds (dated 2026-10-04 amendment below the existing ones; history stays). Update `docs/manual/configuration.md` Console section, the `CONTEXT.md` Console entry, and the Remedy paragraph of `docs/design/confinement-execution-contract.md`. Rule: every non-historical sentence saying a Windows Console is never/not yet fenced, or that Windows keeps the generic demote wording — find with `grep -rniE 'windows.{0,80}(console|conpty).{0,80}(fenc|confin)|(fenc|confin).{0,80}windows' docs/manual docs/design CONTEXT.md README.md docs/adr/0059*`. `CHANGELOG.md` history lines stay.
**Files:** docs/adr/0059-*.md; docs/manual/configuration.md; CONTEXT.md; docs/design/confinement-execution-contract.md
**Read first:** docs/manual/configuration.md — Console section ("On Windows, through ConPTY — and never fenced"); CONTEXT.md — Console entry; docs/design/confinement-execution-contract.md — Remedy paragraph, runtime-demote fallback paragraph; docs/adr/0059-a-console-is-live-host-state-the-model-drives-across-turns.md — Bounds, Amendment (2026-10-04), Note (2026-10-04); internal/tools/manual_drift_test.go — manualPath
**Tests:** none (docs only); the greps above return only historical/ADR-amendment lines.
**Acceptance:**
- the greps above, read line by line, plus the Console sections read whole
- `go test -count=1 -run 'Manual|Readme' ./internal/...` with no new failure
**Commit:** `docs(console): record the confined ConPTY launch on Windows`

## 6. CI gates the Windows Console tests

**What:** owner call: `test-windows` also covers `./internal/console/...`. Depends on item 3.
**Regression guard.** Rule: every sentence naming the trees the Windows job tests or the Windows vet covers follows — including the `ci.yml` step comment (~175-184, "These two trees rather than `./...`", "the Windows half … is the confinement backend") and `building.md`'s `make check` table row (~35); find with `grep -rn 'two trees\|internal/platform` and `internal/probe' .github Makefile docs/manual`.
**Goal:** CI's `test-windows` job and the Makefile's Windows vet target both include `./internal/console/...`, and `docs/manual/building.md` names the trees they cover.
**Approach (assumed at the header base):** `.github/workflows/ci.yml` `test-windows` step `go test -race -count=1 ./internal/platform/... ./internal/probe/...` gains `./internal/console/...`; the Makefile `GOOS=windows go vet` line gains it; `building.md`'s "vets the two trees" sentence follows.
**Files:** .github/workflows/ci.yml; Makefile; docs/manual/building.md
**Read first:** .github/workflows/ci.yml — test-windows job (go test (race) step and its comment); Makefile — the GOOS=windows go vet line under check; docs/manual/building.md — make check table row, "Windows-tagged tests run on a windows-latest job" paragraph; internal/console/process_windows_test.go — TestProcessStartRunsAConfinedSpecOnWindows
**Tests:** none beyond the targets themselves.
**Acceptance:**
- `GOOS=windows go vet ./internal/platform/... ./internal/probe/... ./internal/console/...`
- `go test -count=1 ./internal/console/` on the Windows host
**Commit:** `ci(windows): gate the Console package on the Windows runner`

## 7. Automate the 2uh.6 hands-on check

**What:** owner call: drive every `apogee-2uh.6` step a Windows-host `go test` can reach; leave the bead open with only the real-TUI tail. Depends on items 1, 3 and 4.
**Regression guard.** Any test using a real Windows confiner (platform.NewConfiner) must Close() it via t.Cleanup — it journals labels under the real %USERPROFILE%\.apogee (winlabel.Home) and a leaked journal is replayed by the next session's Recover. The Goal's approval clauses are read at the tool seam: the confined open under a real-confiner handle returns no Go error and a Console with `Confined=true`; the send returns a `ConfineDemoteError` wrapping `ErrConfinementUnavailable`. Every test in `console_windows_test.go` carries "Windows" in its name (`TestConsole..._Windows...`). When `bd` is absent, do not hand-edit `.beads/issues.jsonl` (Dolt is the source of truth; the next export reverts it): carry the new description in the item's sidecar for the owner to apply with `bd update apogee-2uh.6 --description` then `bd export -o .beads/issues.jsonl`.
**Goal:** a Windows-host test file drives, through the registered tools on a real host: `console_open` of `cmd` in Ask-Before, a non-raw `console_send` ending in `\r` that runs, `console_read` with escapes stripped, `console_close` with the job tree gone; and in Auto, a confined `console_open` that needs no approval plus a `console_send` to an unconfined Console demoted to Approval with the reopen advice.
**Approach (assumed at the header base):** build on the tools fakes (`execHost`, `consoleTestCtx`, `openTestConsole`, `withFSConfinement`) with the real Windows confiner; the Approval demotion is asserted at the tool's returned error (`ConfineDemoteError` via `unfencedSendError`) and, if reachable without the TUI, through `internal/agent/dispatch.go` `executeConfineFallback`. Then rewrite the bead: `bd update apogee-2uh.6 --description "<real-TUI steps only: the visible Approval prompt wording after /mode auto, the Console pane rendering>"` (or the jsonl row when `bd` is absent), and `bd export -o .beads/issues.jsonl`.
**Files:** internal/tools/console_windows_test.go; .beads/issues.jsonl
**Read first:** internal/tools/console_send_test.go — openTestConsole, withFSConfinement; internal/tools/console_open_test.go — consoleTestCtx; internal/tools/terminal_test.go — waitForPIDFile; internal/tools/terminal_windows_test.go — TestTerminal_WindowsCancelKillsTheProcessTree; internal/agent/dispatch.go — executeConfineFallback, confineDemotePrompt; internal/platform/confiner_windows.go — NewConfiner, Close
**Tests:** the new file's tests, each skip-free on a Windows host with confinement available, each real confiner's `Close()` registered with `t.Cleanup`.
**Acceptance:**
- `go test -count=1 -run 'Windows' ./internal/tools/`
- with `bd`: `grep 'apogee-2uh.6' .beads/issues.jsonl` shows the rewritten tail-only description; without it: the sidecar carries the description and the two `bd` commands
**Commit:** `test(tools): drive the Windows Console hands-on check`
