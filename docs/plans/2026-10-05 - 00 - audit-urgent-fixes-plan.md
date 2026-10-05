# Plan — urgent fixes from the 2026-10-05 code audit

**Goal:** Fix the four most urgent findings of the 2026-10-05 audit (action order 1–4): the TUI deadlock on `/clear` or a session switch while a background workflow runs, workflow-runner reads that follow symlinks out of the workspace, the two confinement gaps (landlock thread pinning, confined children keeping the controlling tty), and the git command-config probe that fails open.
**Date:** 2026-10-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** fe24e40b

**Regression check (2026-10-05, fe24e40b):**
- 1: guard folded (Close waits on runs and retiring; retiring runs hold their server; test cleanups wait; note assertion via heldNotes)
- 2: guard folded (seed a second session; retype or twin restoreOtherSession; confirm text is boundaryConfirmTitle)
- 3: guard folded (doc.go file map; skillFiles kept) and decision applied (relative in-root symlinks only)
- 4: guard folded — owner decision: absolute in-root symlinks refused, like read_file
- 5: guard folded (stdlib-only pick read; missing walk root stays no-files; FIFO tests in a !windows file with a deadline) and wording made "relative"; yields to internal/workflow/doc.go:9 (ADR 0087 D10 boundary)
- 7: guard folded (build-tagged session attrs; Windows/Linux cross-builds; doc-sweep grep)
- 8: guard folded (Truncated on the stdout-holding buffer; SplitStdout test; doc-sweep grep) and depends on item 7

**Sources:**
- `docs/reviews/code-audit-2026-10-05.md` — findings "`/clear` or a session switch deadlocks the TUI…", "Workflow runner reads follow symlinks…", "Landlock helper does not pin its OS thread…", "bwrap-confined children keep the controlling terminal…", "Git command-config probe fails open…"
- `docs/adr/0031-*.md` (Driver-sufficient engine), `docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md`, `docs/adr/0089-*.md` (background workflows)
- `docs/design/confinement-execution-contract.md`, `docs/design/test-drivers.md`

**Ratified design calls (owner, 2026-10-05):**
- **Scope:** audit action order 1–4 only; items 5–8 of the audit stay out.
- **Deadlock:** the engine cancels and returns — no wait on the caller's goroutine; folders reach Stopped once the Runner settles. Not a TUI `tea.Cmd` rework.
- **Controlling tty:** every POSIX confined non-Console run (bwrap, landlock, seatbelt) starts with `Setsid` in place of `Setpgid`; the Console pty path is unchanged; bwrap argv keeps omitting `--new-session`.
- **Absolute in-root symlinks in workflow reads:** refused, like read_file (owner, 2026-10-05).
- **gitexec incomplete probe:** a truncated listing, a timeout or a wedged drain refuses through the existing refusal path with a reason naming the incomplete probe; never memoised.

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).
- Raw `go test` on one package at a time; never `./...`.

**Out of scope:**
- Audit action order 5–8 (circuit breaker, malformed tool call, custom-regex example, shell-write guard, quick wins, wire and config fixes).
- Unconfined runs' process-group setup (`NewProcessTeardown` for userexec, MCP, TUI commands).
- TUI send-path hold check (audit "A message typed during a session load…").

## 1. Stop background workflows without waiting on the caller's goroutine — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the retiring state is a `retiring` slice on backgroundManager; stopAllBackground drops held/delivered notes inline under the same lock, so the now-unused `dropNotes` helper was removed; the finish note goes through a new `holdFinish(run, note)` that skips a retired run under the lock (`hold` stays for state_test)
NOTES (2026-10-05): the RestoreSession case of the new test is a subtest of TestBackground_AClearDoesNotWaitOnABlockedSink (clear/restore table); the retiring-server test also asserts that relaunching the retiring run's own plan is refused (startBackground's duplicate check)

**What:**
Fixes the audit's High finding: `/clear` or a session switch with a running background workflow hangs the TUI for good (`stopAllBackground` waits on `done` from `Update`, while the run's final event blocks in `tea.Program.Send`).

**Regression guard.** `Close` calls `a.background.waitAll()` (covering runs and retiring) after `stopAllBackground` and before `closeConsoles`; keep its "Close waits for them to end" doc (agent.go:818). Retiring runs count in `serverBusyLocked` and in `startBackground`'s duplicate check, so ADR 0089 D2's one-workflow-per-server queue holds through the retiring window (`endBackground` of a retired run starts the next queued run on its server). Every `t.Cleanup(…stopAllBackground)` in `background_test.go` and `wake_test.go` becomes `stopAllBackground` followed by `background.waitAll` (or `a.Close`).

**Goal:** `ClearContext` and `RestoreSession` return while a background run's event sink is blocked; after they return no note from a stopped run is held or delivered, the incoming session's snapshot lists no stopped run, the stopped run's folder reaches `PhaseStopped` once its Runner settles, and `Close` still waits for every run.

**Approach (assumed at the header base):**
- `internal/agent/background.go`: in `stopAllBackground`, under `m.mu`, cancel each running run and move it from `m.runs` into a new `m.retiring` set (or mark it retired); drop queued runs as today; run `dropNotes()` and set `m.launched = nil` immediately; never `<-done` on the caller's goroutine.
- `driveBackground`: skip `hold(finishNote…)` for a retired run so no late note leaks into the new session; `endBackground` removes it from `retiring` and closes `done`.
- `waitAll` (used by `Close`) waits on `runs` and `retiring`.
- `entries()` / `liveStates` must ignore retired runs.
- Update the doc comments: the file header comments on stop/lifetime, `ClearContext`, `RestoreSession` ("stopped too, after the swap and before the call returns" becomes "cancelled before the call returns; Stopped once it settles") and `Close` in `internal/agent/agent.go`.
- Binding calls: one owner of the retiring state (the `backgroundManager`); no new goroutine per stop; `StopWorkflow`'s non-waiting shape is the precedent.

**Files:** internal/agent/background.go; internal/agent/agent.go; internal/agent/background_test.go; internal/agent/wake_test.go
**Read first:** internal/agent/background.go — stopAllBackground, driveBackground, endBackground, dropLocked, waitAll, serverBusyLocked, liveLocked; internal/agent/agent.go — Close, ClearContext, RestoreSession; internal/agent/background_test.go — newBackgroundParent, TestBackground_AClearStopsTheSetAndDropsItsNotes, TestBackground_CloseStopsEveryWorkflow; internal/agent/workflowcall.go — workflowObserver.emitLocked

**Tests:**
- New `TestBackground_AClearDoesNotWaitOnABlockedSink`: a sink whose `Emit` blocks until released; launch a run, arm the blocking sink only after the child has started (started/stage events also go through `cfg.Events`); call `ClearContext` — it returns before the sink is released; then release and `waitAll`; the folder is `PhaseStopped`, `a.background.heldNotes()` is empty and `Wake` opens nothing (`noteCount` cannot bite: `ClearContext` empties `a.conv`).
- A retiring run still holds its server: a second launch on that server queues until the retired run ends.
- Same for `RestoreSession`.
- Relax `TestBackground_AClearStopsTheSetAndDropsItsNotes` and its neighbours (the "before the clear returned" Stopped assertions) to "not live right after return, `PhaseStopped` after `waitAll`"; keep the no-notes and `Wake`-finds-nothing assertions immediate.

**Acceptance:**
- `go build ./internal/agent/`
- `go test -race -count=1 -run 'TestBackground' ./internal/agent/`

**Commit:** `fix(agent): stop background workflows without blocking the caller`

## 2. Driven `/clear` and session-switch test with a running background workflow — ✅ DONE (2026-10-05)

NOTES (2026-10-05): restoreOtherSession was neither retyped nor reused: the switch passes through the boundary confirm between Enter and "resumed:", so the new file carries its own browser steps (open /sessions, Down, Enter, answer `y`) and e2e_console_test.go is untouched
NOTES (2026-10-05): the other session is seeded in the same run (one greeting turn, saved, then a /clear with nothing running) rather than by a relaunch; the test waits for each session record before opening /sessions so the browser's second row is the greeting session
NOTES (2026-10-05): bite check run — with item 1's internal/agent/background.go reverted, both tests time out after the driver's 60s default (clear: the closed reply never leaves the screen; switch: "resumed:" never lands); background.go restored afterwards

**What:** Depends on item 1. End-to-end guard for the deadlock through the real TUI.

**Regression guard.** Seed the other session first: say one turn, then `/clear` or relaunch on the same home, as `TestE2EConsolesDieWithTheirOwner` does. `restoreOtherSession` (e2e_console_test.go:199) takes `*tuitest.PTYDriver`: retype it over the `driven` interface (and list `cmd/apogee/e2e_console_test.go`) or write a `tuitest.Driver` twin. The confirm text is `boundaryConfirmTitle` (internal/tui/commandrun.go:262), raised by `startNewSession` / `confirmBoundary` before the load, not by `resumeLoaded`.

**Goal:** A driven test launches the TUI with a background workflow held running, types `/clear`, answers the "stop running workflows?" prompt with yes, and sees the cleared view within the driver's default timeout; a second test does the same through `/sessions` switching to another session.

**Approach (assumed at the header base):**
- Copy the shape of `TestE2EBackgroundSubAgent` in `cmd/apogee/e2e_background_subagent_test.go`: stubllm script with an `await:` gate (`bgSubAgentGate`) keeps the child running; `launchTUIOn` / `launchTUIConfigured`, `tuitest.NewDriver`, `drv.Press`, `drv.WaitText` from `cmd/apogee/e2e_support_test.go`.
- Drive the exact prompt text the TUI emits for the stop confirmation (`boundaryConfirmTitle` in `internal/tui/commandrun.go`, raised via `startNewSession` / `confirmBoundary`).
- Release the gate only after the clear view is seen, then quit cleanly.

**Files:** cmd/apogee/e2e_background_clear_test.go; cmd/apogee/testdata/stubllm/ (new fixture script if needed); cmd/apogee/e2e_console_test.go (if `restoreOtherSession` is retyped)
**Read first:** cmd/apogee/e2e_background_subagent_test.go — TestE2EBackgroundSubAgent, bgSubAgentGate; cmd/apogee/e2e_support_test.go — launchTUIOn, driven; cmd/apogee/e2e_console_test.go — restoreOtherSession, TestE2EConsolesDieWithTheirOwner; internal/tui/commandrun.go — startNewSession, boundaryConfirmTitle; internal/tui/sessions.go — resumeLoaded

**Tests:** `TestE2EBackgroundClearDoesNotHang`, `TestE2EBackgroundSessionSwitchDoesNotHang`.

**Acceptance:**
- `go test -race -count=1 -run 'TestE2EBackground(Clear|SessionSwitch)' ./cmd/apogee/`
- Bite check: both tests fail (time out) with item 1's `background.go` change reverted.

**Commit:** `test(apogee): drive /clear and session switch over a running background workflow`

## 3. An `os.Root`-backed workspace FS helper in `internal/security` — ✅ DONE (2026-10-05)

**What:** Preparation for item 4. One shared helper replaces the unexported `skillFiles` / `unopenedFS` pair in `internal/skills/catalog.go`.

**Goal:** `internal/security` exports a function returning an `fs.FS` pinned to a directory through `os.Root` — escaping symlinks (relative or absolute) are refused, relative in-root symlinks are followed, absolute symlinks are refused even in-root, a directory that cannot be opened yields a non-nil FS whose every `Open` fails with the open error — and `internal/skills` uses it, with skills tests unchanged and passing.

**Regression guard.** `os.Root` follows only relative in-root symlinks: relative in-root symlinks are followed; absolute symlinks are refused even in-root. Keep `skillFiles` (catalog_test.go calls it directly) with its `shipped:` and `!filepath.IsAbs` nil branches; only its disk tail calls `security.RootFS`. There is no SYMLINK POLICY note: add `rootfs.go` (RootFS, unopenedFS) to the "filesystem boundary" paragraph of the `internal/security/doc.go` file map, next to `safeio.go`; `TestDocMapNamesEveryFile` is the guard.

**Approach (assumed at the header base):**
- New `internal/security/rootfs.go`: `RootFS(dir string) fs.FS` built from `os.OpenRoot(dir)` + `root.FS()`, relying on the `os.Root` finalizer for close (as `skillFiles` documents); move `unopenedFS` here.
- `internal/skills/catalog.go`: `skillFiles`'s disk tail calls `security.RootFS`; `skillFiles` stays. Check `internal/skills` may import `internal/security` without a cycle; if not, put the helper in the lowest package both can import and NOTE it.
- Name `rootfs.go` (RootFS, unopenedFS) in the "filesystem boundary" paragraph of the `internal/security/doc.go` file map, next to `safeio.go`.

**Files:** internal/security/rootfs.go; internal/security/rootfs_test.go; internal/security/doc.go; internal/skills/catalog.go
**Read first:** internal/skills/catalog.go — skillFiles, unopenedFS, Catalog.Recipe; internal/skills/catalog_test.go — TestSkillFilesFencesADiskFolderAgainstEscapingSymlinks, TestSkillFilesServesAnErrorForAnUnopenableFolder, mustSymlink; internal/security/doc.go — file map "The filesystem boundary" paragraph; internal/security/docmap_test.go — TestDocMapNamesEveryFile; internal/workflow/recipe.go — Recipe.Files

**Tests:** `rootfs_test.go`: escaping relative symlink refused, absolute symlink refused, relative in-root symlink followed, absolute in-root symlink refused, missing dir gives erroring FS (not nil). `TestDocMapNamesEveryFile` passes.

**Acceptance:**
- `go build ./internal/security/ ./internal/skills/`
- `go test -race -count=1 ./internal/security/`
- `go test -race -count=1 ./internal/skills/`

**Commit:** `refactor(security): share an os.Root-pinned workspace FS`

## 4. Pin the workflow runner's workspace and fan_out prompts to the workspace root — ✅ DONE (2026-10-05)

NOTES (2026-10-05): tests also cover a `split:` directory and a context file that escape, beyond the plan's listed cases, since the Goal names both; the launch test sets a 65536-token window so `split:` reaches its read instead of refusing for want of a budget.
NOTES (2026-10-05): internal/workflow/stages.go pickEntries still reads the workflow folder through os.DirFS; that is item 5's scope (symlinked scratch reads) and was left untouched.

**What:** Depends on item 3. Fixes the audit finding "Workflow runner reads follow symlinks out of the workspace": `lines:`, `files:`/`split:` walks, context files and fan_out prompt files are read through `os.DirFS`, which follows symlinks out.

**Goal:** A committed symlink pointing outside the workspace cannot be read by a workflow: `fan_out` with `lines:` naming it, a `files:` or `split:` source whose root is it, a context file that is it, and a fan_out plan's `prompt:` file that is it each fail with an error naming the source; relative in-root symlinks still work; absolute symlinks are refused even when they resolve in-root.

**Regression guard.** Absolute symlinks are refused even when they resolve inside the workspace, matching read_file and @file (TestReadFile_Execute_RefusesAbsoluteInRootSymlink); the Goal says "relative in-root symlinks still work; absolute symlinks are refused even when they resolve in-root", the item's CHANGELOG sidecar entry names this behaviour change, and a test pins an absolute-in-root symlink case for lines: and a files: prefix.

**Approach (assumed at the header base):**
- `internal/agent/launch.go` `newLaunchRunner`: `Workspace: security.RootFS(a.cfg.WorkspaceDir)` in place of `os.DirFS`.
- `internal/agent/workflowspawn.go` `(*workflowSpawner).readPrompt`: the `s.prompts == nil` fallback uses `security.RootFS(root)`.
- Consumers of `Runner.Workspace` (`Expand`, `promptSource`, `keyInput.workspace` → `ItemKey`) keep the `fs.FS` type; `fstest.MapFS` test injections stay valid.

**Files:** internal/agent/launch.go; internal/agent/workflowspawn.go; internal/agent/launch_test.go; internal/agent/workflowspawn_test.go
**Read first:** internal/agent/launch.go — newLaunchRunner; internal/agent/workflowspawn.go — workflowSpawner.readPrompt, newWorkflowSpawner; internal/workflow/runner.go — Runner.Workspace, Runner.promptSource; internal/agent/workflowspawn_test.go — writeOutsideSecret, diskRecipe, TestReadPromptRefusesASymlinkOutOfTheSkillFolder; internal/agent/launch_test.go — writeWorkspaceFile

**Tests:** On a real `t.TempDir` with `os.Symlink` (reuse `writeOutsideSecret`, `diskRecipe` from `workflowspawn_test.go`): a launch whose `lines:` names an escaping symlink fails and the secret text is in no item prompt; a fan_out plan prompt via an escaping symlink is refused (mirror `TestReadPromptRefusesASymlinkOutOfTheSkillFolder`); a relative in-root symlink still reads; an absolute symlink resolving in-root is refused for `lines:` and for a `files:` prefix.

**Acceptance:**
- `go build ./internal/agent/`
- `go test -race -count=1 -run 'Launch|ReadPrompt|Workflow' ./internal/agent/`

**Commit:** `fix(agent): pin workflow workspace reads to the workspace root`

## 5. Refuse non-regular workflow sources and symlinked scratch reads — ✅ DONE (2026-10-05)

NOTES (2026-10-05): the pick-stage symlink test sits in internal/workflow/recipe_stages_test.go beside TestPickFromAFileInTheWorkflowFolder (the plan's Read-first anchor) rather than in stages_test.go; store_test.go is untouched because the context-file FIFO case lives in items_fifo_unix_test.go as the Tests line asks.
NOTES (2026-10-05): splitParts already refused a root that is not a directory ("split takes a directory"); unchanged. The whole-package run used `GOMEMLIMIT=2GiB go test -count=1 ./internal/workflow/` (no -race, per the machine's memory rule); -race ran on the new and touched tests only.

**What:** Depends on item 4. Completes the symlink finding: `os.Root` does not refuse FIFOs or devices, a relative symlinked walk root is still followed in-root, and the pick stage reads its `file:` through `os.DirFS` on the workflow folder.

**Goal:** A `lines:` target or context file that is not a regular file (FIFO, device, socket) is refused with an error naming the source and never blocks; a `files:`/`split:` walk root that is not a directory is refused; the per-entry walk keeps skipping every symlink; the pick stage's `file:` read refuses a symlink escaping the workflow folder.

**Regression guard.** `internal/workflow` stays stdlib-plus-`internal/domain` (internal/workflow/doc.go:9, ADR 0087 D10 — the item yields to it): `pickEntries` reads through `os.OpenRoot(dir)` + `root.FS()` directly, with no import of `internal/security`. `walkFiles` keeps its ErrNotExist → no-files return (items_test.go "a glob under a missing directory"); refuse only a root that exists and is not a directory. FIFO cases go in a `//go:build !windows` file (`internal/workflow/items_fifo_unix_test.go`, the `safeio_fifo_unix_test.go` pattern) and run `Expand` / `ItemKey` in a goroutine with a short deadline (e.g. 5s), so the pre-item tree fails instead of hanging. Wherever an in-root symlink is claimed to work, it is a relative in-root symlink.

**Approach (assumed at the header base):**
- `internal/workflow/items.go`: `nonBlankLines` and `ItemKey`'s context-file read in `internal/workflow/store.go` `fs.Stat` first and require `Mode().IsRegular()`; `walkFiles` / `splitParts` refuse a root that exists and is not a directory (a missing `walkFiles` root still yields no files); keep the "Symlinks are passed over" entry skip.
- `internal/workflow/stages.go` `(*runState).pickEntries`: read through `os.OpenRoot(dir)` + `root.FS()` in place of `os.DirFS(dir)` (stdlib only).

**Files:** internal/workflow/items.go; internal/workflow/store.go; internal/workflow/stages.go; internal/workflow/items_test.go; internal/workflow/items_fifo_unix_test.go; internal/workflow/store_test.go; internal/workflow/stages_test.go
**Read first:** internal/workflow/items.go — nonBlankLines, walkFiles, splitParts, globFiles; internal/workflow/store.go — ItemKey; internal/workflow/stages.go — runState.pickEntries; internal/workflow/items_test.go — TestExpand error table (glob under a missing directory); internal/workflow/recipe_stages_test.go — TestPickFromAFileInTheWorkflowFolder, newTestRunner; internal/workflow/validate_test.go — TestWorkflowStaysOffTheLoopAndItsDrivers; internal/security/safeio_fifo_unix_test.go

**Tests:** `t.TempDir` cases: `lines:` on a FIFO (`syscall.Mkfifo`, in `items_fifo_unix_test.go`, run under a 5s deadline) refused without blocking; context-file FIFO refused (same file and deadline); walk root that is a file refused; a glob under a missing directory still yields no items; pick-stage `file:` symlink out of the workflow folder refused.

**Acceptance:**
- `go build ./internal/workflow/`
- `go test -race -count=1 ./internal/workflow/`

**Commit:** `fix(workflow): refuse non-regular sources and escaping scratch reads`

## 6. Pin the OS thread before landlock restriction and exec — ✅ DONE (2026-10-05)

NOTES (2026-10-05): added TestApplyLandlockAndExecLocksItsThreadFirstBites (fixture table: late lock, no lock, deferred/closure unlock, aliased import, method of the same name, missing body) beside the named guard so the detector is proven, not assumed; the guard also requires a plain `import "runtime"` and no runtime.UnlockOSThread anywhere in the function.
NOTES (2026-10-05): landlock_linux.go's header comment and applyLandlock doc comment now say the restriction is per-thread and name the lock step (same listed file; the old "calling process is confined" wording was what the fix corrects).

**What:** Fixes the audit finding "Landlock helper does not pin its OS thread": `PR_SET_NO_NEW_PRIVS` and `landlock_restrict_self` bind to the calling thread only, then `syscall.Exec`.

**Goal:** The first statement of `ApplyLandlockAndExec` is `runtime.LockOSThread()`, never undone, and a source-level guard test fails if it is not.

**Approach (assumed at the header base):**
- `internal/platform/landlock_linux.go` `ApplyLandlockAndExec`: add `runtime.LockOSThread()` first; doc comment says why (thread-scoped restriction must be on the exec'ing thread).
- Guard test parses `landlock_linux.go` with `go/parser` + `go/ast` (pattern: `cmd/apogee/seams_guard_test.go`) and asserts the first body statement is a `runtime.LockOSThread()` call.
- Docs: `docs/design/confinement-execution-contract.md` §2.3 in-child sequence gains the lock step; `internal/platform/doc.go` where it lists the sequence.

**Files:** internal/platform/landlock_linux.go; internal/platform/landlock_guard_test.go; internal/platform/doc.go; docs/design/confinement-execution-contract.md
**Read first:** internal/platform/landlock_linux.go — ApplyLandlockAndExec, applyLandlock; internal/platform/landlock_linux_test.go — TestApplyLandlockAndExecRejectsEmptyArgv; cmd/apogee/seams_guard_test.go — go/parser guard pattern; cmd/apogee/confined_exec_linux.go — __confined-exec dispatch; internal/platform/doc.go — landlock_linux.go paragraph; docs/design/confinement-execution-contract.md — §2.3 Linux in-child sequence

**Tests:** `TestApplyLandlockAndExecLocksItsThreadFirst` (guard test; build-tag free so it runs on every OS since it only parses source).

**Acceptance:**
- `go vet ./internal/platform/`
- `go test -race -count=1 -run 'LocksItsThreadFirst|ApplyLandlockAndExec' ./internal/platform/`
- `GOOS=linux go build ./internal/platform/ ./cmd/apogee/`

**Commit:** `fix(platform): lock the OS thread before landlock restrict and exec`

## 7. Start confined non-Console runs in a new session

**What:** Fixes the audit finding "bwrap-confined children keep the controlling terminal and can inject keystrokes" (TIOCSTI via `/dev/tty`), for every POSIX backend per the ratified call.

**Regression guard.** `subprocess.go` is untagged and Windows' `syscall.SysProcAttr` has no `Setsid`/`Setpgid`: put the session upgrade in a build-tagged pair (`session_unix.go` `!windows` / `session_windows.go` no-op, the `cmdline_unix.go` / `cmdline_other.go` pattern) that allocates `SysProcAttr` when nil. The doc sweep covers every comment or doc line saying a confined non-Console run is a Setpgid group, keeps the controlling terminal, or that `--new-session` would detach it: `grep -rn "Setpgid\|new-session\|controlling t" internal/platform internal/subprocess docs/design/confinement-execution-contract.md docs/adr/0081-*.md`.

**Goal:** Every confined run started through `subprocess.run` on POSIX has `SysProcAttr.Setsid == true` and `Setpgid == false` at start; the Console pty path (`console.Start`) keeps `Setsid` + `Setctty`; teardown's negative-PID group kill still reaches the confined child's descendants; the bwrap argv still has no `--new-session`.

**Approach (assumed at the header base):**
- `internal/subprocess/subprocess.go` `run`: after the confinement `prepare` succeeds and the run is confined, set `Setsid = true`, `Setpgid = false` (Go fails `setpgid` on a session leader with EPERM, so replace, never add). A session leader's pgid == pid, so `killProcessGroup` in `internal/platform/teardown_unix.go` is unchanged.
- Leave `setConfinedPgid` in `internal/platform/confine_posix.go` as is (Console overrides attrs itself); note in its doc that `subprocess.run` upgrades to a session.
- Docs: `internal/platform/namespace_linux.go` header (the `--dev /dev` tty paragraph and "No `--new-session`" note: the session detach now comes from the fork), `docs/design/confinement-execution-contract.md` (the "Never `--new-session`" line and the device-set/tty paragraph), `docs/adr/0081-*.md` gains a dated note.

**Files:** internal/subprocess/subprocess.go; internal/subprocess/session_unix.go; internal/subprocess/session_windows.go; internal/subprocess/subprocess_unix_test.go; internal/platform/teardown_unix.go; internal/platform/confine_posix.go; internal/platform/namespace_linux.go; docs/design/confinement-execution-contract.md; docs/adr/0081-linux-falls-back-to-a-namespace-fence-through-bwrap.md
**Read first:** internal/subprocess/subprocess.go — run, ConfinementHandoff; internal/subprocess/cmdline_unix.go — setRawCommandLine (build-tag pattern); internal/platform/teardown_unix.go — NewProcessTeardown, killProcessGroup; internal/platform/confine_posix.go — setConfinedPgid; internal/subprocess/subprocess_test.go — fakeConfiner, TestRunSubprocessRecordsConfined; internal/subprocess/teardown_test.go — installFakeTeardown; internal/tools/console_open.go — ConfinementHandoff caller; internal/platform/namespace_linux.go — header, namespaceArgv

**Tests:** A confined run (fake confiner, nil `SysProcAttr`) has `Setsid` true / `Setpgid` false at start; an unconfined run keeps `Setpgid`; a confined child that opens `/dev/tty` fails with ENXIO (Linux/macOS, skip when the test process has no controlling tty); a confined run's grandchild is killed on timeout.

**Acceptance:**
- `go build ./internal/subprocess/ ./internal/platform/`
- `go test -race -count=1 ./internal/subprocess/`
- `go test -race -count=1 -run 'Confine|Namespace|Teardown|Seatbelt' ./internal/platform/`
- `GOOS=windows go build ./internal/subprocess/`
- `GOOS=linux go build ./internal/subprocess/ ./internal/platform/`

**Commit:** `fix(subprocess): detach confined runs from the controlling terminal`

## 8. Make the git command-config probe fail closed

**What:** Depends on item 7. Fixes the audit finding "Git command-config probe fails open on truncation, timeout or wedged drain".

**Regression guard.** Item 8 depends on item 7 (both edit internal/subprocess/subprocess.go `run`, non-overlapping hunks). `Truncated` = discarded > 0 on the buffer holding stdout (`stdoutOnly` under `SplitStdout`, `out` otherwise; false for `RunSubprocessTo`); the subprocess test drives a `SplitStdout` oversize run (the probe's shape) besides the plain one. The doc sweep covers every comment describing how a timed-out, wedged or truncated probe result is treated (`configFiles`, `parseConfigListing` included): `grep -n "truncat\|reached\|subprocess contract\|timed out" internal/gitexec/*.go`.

**Goal:** When `rev-parse --git-path` or any `git config … --list` probe call times out, wedges its drain, or its output is truncated at the subprocess cap, `Host.Capture` returns a refusal result and `Host.queryDiagnosed` returns a refusal error, both through the existing refusal path with text naming the incomplete probe; that answer is never stored in `commandConfigProbes`; a clean non-zero listing exit stays the pass case.

**Approach (assumed at the header base):**
- `internal/subprocess/subprocess.go`: `SubprocessResult` gains `Truncated bool`, set when the buffer holding stdout discarded bytes (`stdoutOnly` under `SplitStdout`, `out` otherwise; `CappedBuffer.discarded > 0`).
- `internal/gitexec/gitexec.go`: `repoLocalCommandConfig` and `configFiles` return an incomplete state (with reason) when a probe result has `TimedOut`, `DrainWedged` or `Truncated`; `probeCommandConfig` never stores it; callers render it through `CommandConfigRefusal` or a sibling `CommandConfigIncompleteRefusal(reason)` sharing the "git refused:" prefix.
- Update doc comments on `repoLocalCommandConfig`, `Capture`, `probeCommandConfig`, `internal/gitexec/doc.go`, and `docs/design/confinement-execution-contract.md` where the probe is described.

**Files:** internal/subprocess/subprocess.go; internal/subprocess/subprocess_test.go; internal/gitexec/gitexec.go; internal/gitexec/gitexec_test.go; internal/gitexec/doc.go; docs/design/confinement-execution-contract.md
**Read first:** internal/gitexec/gitexec.go — probeCommandConfig, repoLocalCommandConfig, configFiles, probeGit, Capture, CommandConfigRefusal, parseConfigListing; internal/subprocess/subprocess.go — run, SubprocessResult, CappedBuffer; internal/gitexec/gitexec_test.go — fakeHost, cleanRepository; internal/subprocess/subprocess_test.go — TestRunSubprocessCapsAnOversizeStdout, oversizeStdoutScript; internal/agent/secretsguard_test.go — TestCommitSecretsIncompleteScanForcesApproval

**Tests:** With `fakeHost` / `cleanRepository` in `gitexec_test.go` (fresh root per test): listing `TimedOut` → refusal; `DrainWedged` → refusal; `Truncated` → refusal; timed-out `rev-parse` with no output → refusal; each followed by a clean probe that passes (proves no memo); exit-128 `--worktree` listing still passes. `subprocess_test.go`: output over `MaxSubprocessOutputBytes` sets `Truncated`, for a plain run and for a `SplitStdout` run.

**Acceptance:**
- `go build ./internal/subprocess/ ./internal/gitexec/`
- `go test -race -count=1 ./internal/gitexec/`
- `go test -race -count=1 -run 'Truncat|Capped' ./internal/subprocess/`

**Commit:** `fix(gitexec): refuse when the command-config probe is incomplete`
