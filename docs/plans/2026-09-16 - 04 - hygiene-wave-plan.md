# Plan — hygiene wave: fifteen small beads (2026-09-16 - 04)

**Goal:** Close fifteen open P3/P4 beads that need no further design: four missing tests, two comment/template drift sweeps, four small bugs, three config-notice features, two TUI structural folds, and the internal/tui `t.Parallel` sweep. Every item is self-contained and closes its bead at the commit.

**Date:** 2026-09-16 · **Status:** unexecuted · **sized for:** ~200k-context host

**Sources:** `bd show apogee-289 apogee-1kv apogee-1ah apogee-ah5 apogee-vf1 apogee-t74 apogee-bsg apogee-lq4 apogee-add apogee-2sj apogee-ibd apogee-970 apogee-lpi apogee-agk apogee-11s`; `docs/adr/0011-*.md`; `docs/adr/0012-*.md`; `docs/adr/0021-*.md`; `docs/adr/0041-*.md`; `docs/adr/0053-*.md` D3/D7; `docs/adr/0054-*.md`; `docs/adr/0064-*.md` §4; `docs/adr/0075-*.md` §4; `docs/design/test-drivers.md`; `cmd/apogee/seams_guard_test.go`.

**Ratified design calls (owner, 2026-09-16, via AskUserQuestion):**
- **lq4 — defaults follow the template:** registry defaults become `remember-model: true` and `ui.stall-after: 120s`; the template's `Default: false` comment and the manual's "though the starter file…" caveats go.
- **add — fence per Firing:** `KeyResolver.ResolveWithin(entry, workspaceRoot)` judges the exec fence against the given root on every call; `firingConfig` passes the Firing's workspace.
- **agk — table + pickerOffering:** one package-level pointer table keyed by `framePane` walked in the click-chain order (key verdicts stay per pane, ADR 0053 D3; nothing on `Model`, ADR 0011), then `pickerOffering` folds picker.go's switches.
- **2sj — ephemeral notes:** `tui.Options.StartupNotices []string` carries exactly the strings `announceConfinement` prints; posted like `ColorSchemeWarnings` (`addEphemeralNote`); the stderr line stays.
- **lpi — value only:** `apogee probe config` prints `notices`, `migration`, `resolved` in `Host.Report()`'s `field()` layout; no provenance, no `--format`; it never writes the file (live read, `mayMigrate=false`).
- **970 — eight members:** `ConfigHost` = SaveHostAcknowledgement, ReloadConfig, AwaitConfigChange, MigrateKey, KeepPlaintextKey, MigrateSubAgentsServer, RecordModelChoice, ExternalEditSpec; nil = unwired degrade (ADR 0054 D2).
- **bsg — reorder the registry:** `KeyRegistry` rows take the starter template's order; a new test pins it.
- **bsg — reactions stays under bypass:** the starter template's Reactions block moves to sit directly AFTER the `bypass:` block (corrected at the re-check, 2026-09-16: an earlier wording said before), so template and registry both read `editor, bypass, reactions, model-profiles` and registry, `settingsTable` and the /settings sections agree.
- **11s — three sweeps then the guard:** file-groups A/B/C in turn, then the seam-guard test plus the shards/manual rewrite.
- **Writer's calls (mechanical):** `MergeDangerousRules` stays (the seam a project-config key would call — apogee-089), its prose is reworded; the demorig stage error becomes a `FAIL` row; `ReloadConfig` returns a `ConfigReload{Applied, Notices}` value.

**Regression check (2026-09-16, 73345950):** five independent read-only reviewers over BASE 73345950; items 5, 6 and 19 SAFE (`Read first` lines only).
- 1: What extended per the writer's decision — the popup goldens and `pickerFork` items 16–17 quote are read against the post-03 tree, not BASE.
- 2: rejected — `TestQueuedCommandRunsAtIdle` does not exist, so widen the Acceptance to `TestQueued|TestCommandsQueued`; it exists at `internal/tui/interject_test.go:550` at BASE 73345950 (commit 6a8f3bab), so the Acceptance as written already runs it.
- 3: guard folded — `find_files` has no `paths` argument; its case pins the shared helper only.
- 4: guard folded — the two window keys ride a binding (compose `binding` + `rebindProbe`); no live double exists, seed the real `*liveSettings` with non-default values.
- 7: guard folded — unconditional approval-pane wait, a git-initialised workspace, `console_open` enabled on the roster, `planMenuTools` lives in internal/agent.
- 8: guard folded — (f) the clip sentence goes on the stacked bullet, (e) a positive ADR grep; (a) reworded per the writer's decision.
- 9: recast — reactions stays under bypass: the template's Reactions block moves to directly AFTER the `bypass:` block, `settingsTable` is reordered, `t16-settings-rows.txt` is re-recorded, the real order guards named; yields to `cmd/apogee/settingsrows.go` :78-80.
- 10: guard folded — widened test grep, the remember-model `fromFile` projection flips, CONTEXT.md added; supersedes ADR 0048 decision 1 under the ratified lq4 call (dated note in the ADR).
- 11: guard folded — `userexec.ResolveProgram` exported, `resolveFiringRouting` fenced too, the daemonfire test plants its own program and reads `raise`'s error, manual + keyresolve.go reworded; supersedes CHANGELOG.md :3125's daemon clause under the ratified add call.
- 12: guard folded — neither test reaches `w`; both assert `rec.opts.StartupNotices`.
- 13: guard folded — notices diffed against the baseline, the stale clause lives in config.go, three non-consumers dropped from Files, WaitText the quoted key.
- 14: guard folded — the two member gates collapse to `Config == nil`, go-doc link sweep, `internal/tui/settingsedit.go` dropped; supersedes ADR 0054 D7's `SaveHostAcknowledgement` example under the ratified 970 call.
- 15: guard folded — no Masked row exists, the legacy fixture is the retired quadruple, `ErrRetiredShape` sentinel, a `--config`-only helper with the notice spelled literally.
- 16: recast — the click-chain order is pinned literally as `TestPointerPanesWalkInTheClickChainOrder`, the settings `sel` drop on non-claim is kept and pinned; yields to `mouse.go` :471-474.
- 17: guard folded — every enum value read from the tree (twelve, `pickerFork` included) per the writer's decision; rows are `popupRow`, accept takes the offering index.
- 18: guard folded — the subtest clause per the writer's decision (shared unlocked `*paintCache`), the `srv.Close` hazard, the no-op tuitest sentence dropped.
- 20: guard folded — the clause names the shared paint cache, transcript arrays and textarea value.
- 21: guard folded — transitive helper resolution with a `TestParallelViaHelper` fixture, `Makefile` added to Files and the acceptance grep.
- Re-check (2026-09-16, 73345950), one reviewer over items 9 and 16:
- 9: recast again — the Reactions block moves AFTER `bypass:` (not before, which put `reactions` under Interface); the What's `mcp-servers` clause deleted, the registry reorder stated as exactly three moves; still yields to `cmd/apogee/settingsrows.go` :78-80.
- 16: guard folded — the click func carries both `m` and the pre-click frame `pre` (`withFrameSpans` composed once), the wheel func returns no Cmd.

**Standing requirements:**
- `skills: coding-standards`.
- Each item closes its bead at the commit: `bd close <id> --reason="<commit subject>"`; a bead spanning several items closes at the last.
- CHANGELOG entries travel in item sidecars and land at closeout; never edit `CHANGELOG.md` in an item.
- Any authorized deviation lands as a dated NOTES line under its item.

**Out of scope:** the internal/tools `execHost` seam (apogee-11s NOTES — its own bead); provenance in `probe config`; a persistent startup banner; `apogee-7ui` (owned by the archived `2026-09-13 - 00` plan); every bead not named above.

## 1. Verify plan 2026-09-16 - 03 is archived — ✅ DONE (2026-09-16)

NOTES (2026-09-16): verified on the tree — `docs/plans/archived/2026-09-16 - 03 - urgent-beads-wave-plan.md` exists (archived at 81c14792), the live `docs/plans/2026-09-16 - 03 - urgent-beads-wave-plan.md` is absent, `git status --porcelain | grep -v '^??'` is empty, and `internal/tui/picker.go` carries twelve `pickerKind` values ending in `pickerFork` (line 93) — the fork wave is on the tree.

**What:** This plan's tree assumes the urgent-beads wave (Anthropic wire, session fork) has landed on `main`: items 12–14 and 16–20 edit `internal/tui/tui.go`, `messages.go`, `sessionsave.go` and `cmd/apogee/wire_session.go`, all of which that run changes. Confirm `docs/plans/archived/2026-09-16 - 03 - urgent-beads-wave-plan.md` exists and `docs/plans/2026-09-16 - 03 - urgent-beads-wave-plan.md` does not; if either check fails, stop the run — do not proceed onto a half-landed tree. The popup goldens and `pickerFork` items 16–17 quote are read against the tree this item gates on (post-03), not BASE.

**Files:** none.
**Read first:** docs/plans/2026-09-16 - 03 - urgent-beads-wave-plan.md — (the file whose absence is checked); docs/plans/archived/ — (where it must land); internal/tui/picker.go — pickerFork (proof the fork wave is on the tree: BASE has eleven kinds, the in-flight tree twelve)

**Tests:** none.

**Acceptance:** `test -f "docs/plans/archived/2026-09-16 - 03 - urgent-beads-wave-plan.md" && test ! -f "docs/plans/2026-09-16 - 03 - urgent-beads-wave-plan.md" && git status --porcelain | grep -v '^??' | wc -l | grep -qx 0`

commit: none — verification only.

## 2. `quiescent()` counts queued commands (apogee-289) — ✅ DONE (2026-09-16)

**What:** Fix apogee-289. `Model.quiescent()` in `internal/tui/schedule.go` gates a Firing on `pendingInterjections` only; a command queued in `deferredCommands` while `stateErrored` does not hold a Schedule. Add `len(m.deferredCommands) == 0` as the fourth term and extend the doc comment ("no row the human typed — an interjection or a queued command — is still waiting to go out"). Its sole caller `reportActivity` needs no change.

**Files:** `internal/tui/schedule.go`, `internal/tui/schedule_test.go`.
**Read first:** internal/tui/schedule.go — quiescent, reportActivity; internal/tui/commandrun.go — queueCommand, runDeferredCommands, commandRunnable; internal/tui/schedule_test.go — TestReportActivityHoldsWhileAQueueIsHeld, activityModel; internal/tui/interject.go — popQueuedCommand (the other deferredCommands writer, unchanged)

**Tests:** in `schedule_test.go` beside `TestReportActivityHoldsWhileAQueueIsHeld`: `TestReportActivityHoldsWhileACommandIsQueued` — a Model with one entry appended via `queueCommand` (see `commandrun.go`) and nothing else in flight has `quiescent() == false`; after `runDeferredCommands` drains it, `true`. The new test must fail against the pre-item tree.

**Acceptance:** `go test -race ./internal/tui/ -run 'TestReportActivity|TestScheduleFiring|TestQueuedCommandRunsAtIdle'`

commit: `fix(tui): a queued command holds a Schedule's Firing like a staged interjection`

## 3. grep transcript slot names a `paths` scope (apogee-1kv) — ✅ DONE (2026-09-16)

**What:** Fix apogee-1kv. `searchScopeArg` in `internal/tui/toolregistry.go` reads `path` only, so a grep call scoped through the tool's `paths` array (see `grepArgs` in `internal/tools/grep.go`) shows no scope qualifier. Extend `searchScopeArg`: when `paths` carries entries, the qualifier is the entries joined by `, ` (a `path` other than `.` is listed first); the existing `path`-only spellings are unchanged. `findFilesTarget` shares the helper and gains the same behaviour — pin it too.

**Regression guard.** `find_files` takes no `paths` argument — `internal/tools/find_files.go`'s schema and `findFilesArgs` carry `path` only — so the `TestFindFilesTarget` `paths` case pins the shared helper alone: say so in the test ("find_files itself has no paths parameter") and add nothing to the tool. The spelling the slot matches is grep's own `searchScopeAll` (`internal/tools/grep.go`): `path` first, `", "`-joined.

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolregistry_test.go`.
**Read first:** internal/tui/toolregistry.go — searchScopeArg, grepTarget, findFilesTarget, qualifiedTarget; internal/tools/grep.go — searchPaths, searchScopeAll; internal/tui/toolregistry_test.go — TestGrepTarget, TestFindFilesTarget

**Tests:** extend `TestGrepTarget` with `{"pattern":"KeyMsg","paths":["internal/tui","cmd/apogee"]}` → `KeyMsg · internal/tui, cmd/apogee`, `path` + `paths` → `KeyMsg · internal/tui, cmd/apogee, docs`-shaped (path first), and `paths` + `include` → `… · *.go`; extend `TestGrepBranchRowShowsTheSearchedPath` with a `paths`-only call whose rendered row contains the joined qualifier; one `TestFindFilesTarget` case with `paths`, pinning the shared helper only (`find_files` itself has no `paths` parameter). The new cases must fail against the pre-item tree.

**Acceptance:** `go test -race ./internal/tui/ -run 'TestGrepTarget|TestGrepBranchRowShowsTheSearchedPath|TestFindFilesTarget'`

commit: `fix(tui): the grep and find_files transcript slots name a paths-scoped call's scope`

## 4. An empty int value lands the row's Default through `applySettingFor` (apogee-1ah) — ✅ DONE (2026-09-16)

**What:** Test-only, closes apogee-1ah. `landSetting` in `cmd/apogee/wire_settings.go` resolves an empty value to `row.Default` for every row, but no test drives the empty-int case (`context-window`, `working-window`, `delegate-max-steps`, `delegate-max-depth`, `delegate-max-tokens`) end to end. Add `TestApplySettingOnAnEmptyIntValueLandsTheRowDefault` in `wire_settings_test.go` beside `TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault`: per key, `applySettingFor(settingsApplier{engine: spy, live: <a live double that records update>})(key, "")` returns `"", nil` and the recorded `config.Options` field equals the row's `Default` parsed with `strconv.Atoi`; a second case with `"  "` (whitespace) lands the same. Read the live-double shape from the existing `applyWorkingWindow`/`applyDelegateMaxSteps` tests in the same file.

**Regression guard.** `context-window` and `working-window` ride the binding (`settingsApplier.rides`), so an applier of `{engine, live}` alone is refused by `unreachable` with cannotApply before `landSetting` runs: compose `binding: func() upstreamBinding { return upstreamBinding{} }` and `rebind: (&rebindProbe{}).rebind` as `TestApplySettingRideIsSilentBeforeAServerIsBound` does — the ride is silent unbound. There is no live double: `settingsApplier.live` is the concrete `*liveSettings`, and those two rows' `Default` is `"0"`, so seed `live := newLiveSettings(config.Options{ContextWindow: 4096, WorkingWindow: 8192, DelegateMaxSteps: 7, …})` with non-default values and read back `live.options()` / `live.pin()`, asserting each field equals `strconv.Atoi(row.Default)` after the empty apply.

**Files:** `cmd/apogee/wire_settings_test.go`.
**Read first:** cmd/apogee/wire_settings.go — landSetting, applySettingFor, settingsApplier.rides, settingsApplier.unreachable; cmd/apogee/wire_settings_test.go — TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault, TestApplySettingRideIsSilentBeforeAServerIsBound, rebindProbe; internal/config/registry.go — KeyRegistry (the five int rows)

**Tests:** the one above.

**Acceptance:** `go test -race ./cmd/apogee/ -run 'TestApplySetting'`

commit: `test(cmd/apogee): an empty int value lands the registry row's Default through applySettingFor`

## 5. The file pass pins the full refusal sentence for cursor-shape and sub-agents-choice (apogee-ah5) — ✅ DONE (2026-09-16)

**What:** Test-only, closes apogee-ah5. In `internal/config/config_test.go`, `TestFilePassRefusesThroughTheRows` matches `startupErr` by prefix, so the domain tail of two rows' sentences is unpinned. Replace the two `wantErr` prefixes with the full sentences the emitters produce — cursor-shape (`validateCursorShapeName` → `domain.UnknownCursorShapeError`): `apogee: invalid cursor-shape: unknown cursor shape "sideways" (known shapes: block, underline, bar)`; sub-agents-choice (`ParseSubAgentsChoice`): `apogee: invalid sub-agents-choice: "banana" — it takes "fixed" (the sub-agents-server: key alone picks where a delegation runs) or "model" (the top-level model may say run_on per delegation)` — and change the assertion from `strings.HasPrefix` to equality for every case (the delegate-timeout case already carries its full sentence). Take the sentences from the emitting code, not from this plan, if they differ.

**Files:** `internal/config/config_test.go`.
**Read first:** internal/config/config_test.go — TestFilePassRefusesThroughTheRows, testConfigHome; internal/config/registry.go — validateCursorShapeName, validateSubAgentsChoice, Key.admit; internal/config/options.go — ParseSubAgentsChoice; internal/domain/uivocab.go — UnknownCursorShapeError; internal/config/config.go — applyFile

**Tests:** the amended table.

**Acceptance:** `go test -race ./internal/config/ -run 'TestFilePassRefusesThroughTheRows'`

commit: `test(config): the file pass pins every refusal row's full sentence`

## 6. demorig check judges an unreadable stage as a FAIL row (apogee-vf1) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): the "missing stage" case builds its expected `beat N |` rows from the loaded hero storyboard rather than a hand-typed list, so the assertion tracks the fixture; the git-refusal detail carries the first stderr line (`fatal: …`), falling back to the exec error only when stderr is empty.

**What:** Fix apogee-vf1. `judgeStage` in `cmd/demorig/check.go` returns an error when `git -C <stage> status --porcelain` fails (nonexistent or non-git dir), so the table aborts before any beat row prints. Make it a row instead: `{Subject: stageSubject, Verdict: verdictFail, Detail: "stage: " + <one-line reason>}` where the reason is `<stage> is not a git work tree (git status: exit status 128)`-shaped — the first line of git's stderr, or the exec error, after the path. `checkTake` keeps printing every beat row; `countFailed` then returns the error and the exit stays `exitRunFailed`. A missing `git` binary stays a hard error (the rig cannot judge anything).

**Files:** `cmd/demorig/check.go`, `cmd/demorig/check_test.go`.
**Read first:** cmd/demorig/check.go — judgeStage, checkTake, countFailed, writeCheckTable; cmd/demorig/check_test.go — TestJudgeStage_NotARepoIsAnError, TestCheckCommand_ExitStatus, stageRepo; cmd/demorig/main.go — exitCodeFor

**Tests:** rewrite `TestJudgeStage_NotARepoIsAnError` as `TestJudgeStage_NotARepoIsAFailRow` (a `t.TempDir()` with no `.git` → `verdictFail`, detail starts `stage: `); a nonexistent path case; extend `TestCheckCommand_ExitStatus` with a `"missing stage"` case asserting the output holds every `beat N |` row AND `stage  | FAIL |`, exit `exitRunFailed`. The rewritten tests must fail against the pre-item tree.

**Acceptance:** `go test -race ./cmd/demorig/ -run 'TestJudgeStage|TestCheckCommand_ExitStatus|TestWriteCheckTable'`

commit: `fix(demorig): check prints every beat row when the --stage dir cannot be judged`

## 7. Journey: the real Terminal's shell-write view reaches the guard (apogee-t74) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): the workspace's control plane is seeded by hand (`.git/config` + `.git/hooks/`, no HEAD) rather than `git init -q`, the plan's stated alternative, so the test needs no git binary; the refused exchange is told from the allowed one by a script turn matching `last_message: ^refused by the dangerous-action guard` with its own wrap-up text, so the second `WaitText` cannot be satisfied by the first exchange's line.

**What:** Test-only, closes apogee-t74. No test drives a read-only command through the registry's real `*tools.Terminal` (`ShellCommandKeys`) to a `TierNone` verdict from the `write-git-control-plane` rule; `internal/security/dangerous_test.go` uses `stubTool`. Two tests: (a) `cmd/apogee/e2e_guard_controlplane_test.go` in the shape of `TestE2EApprovalForcesALookAtTheControlPlane` (`e2e_approval_test.go`): a new script `testdata/stubllm/guard-controlplane.yaml` whose first prompt issues `terminal` with `{"command":"ls -la .git/hooks"}` — approve if the approval pane appears (`decide`), then `WaitText` the stub's final line — and whose second prompt issues `{"command":"echo hooked > .git/config"}`; judge that the second call's tool result on the wire (`stub.Requests()`) carries the rule's reason `write or delete under a repository's git control plane` and that the workspace's `.git/config` is byte-identical to before. (b) `internal/tools/shellmarker_test.go`: the registry's real `terminal` and `console_open` tools (via `DefaultToolsWithHost`, see `planmenu_test.go`) both satisfy `domain.ShellCommandTool` and `domain.ShellCommandArgKeys` returns `[]string{"command"}` for each.

**Regression guard.** (a) Wait the approval pane unconditionally (`awaitApprovalPane`, asserting `controlReason`) and assert via `stubSawMessage` that the first call's result does NOT carry the rule's reason, only then `decide(drv, "a")` — `guard.yaml`'s `tool_result: terminal` wrap-up fires on a refusal too, so a conditional approve asserts nothing. `e2eWorkspace` seeds only `a.txt` (no `.git/` exists): build the workspace with `launchTUIIn` over `readFenceRealDir` plus `git init -q` (or a seeded `.git/config` and `.git/hooks/`), read `.git/config`'s bytes before the second prompt and compare after. (b) `console_open` is default-off: build the roster with `DefaultToolsWithHost(root, HostTools{Enabled: []string{"console_open"}})` (or walk `builtinTools(root, host)`, the unexported build set internal/tools tests already use); the shape to copy is `internal/agent/planmenu_test.go` — `planMenuTools` — not a file in internal/tools.

**Files:** `cmd/apogee/e2e_guard_controlplane_test.go`, `cmd/apogee/testdata/stubllm/guard-controlplane.yaml`, `internal/tools/shellmarker_test.go`.
**Read first:** cmd/apogee/e2e_approval_test.go — TestE2EApprovalForcesALookAtTheControlPlane, awaitApprovalPane, decide, stubSawMessage; cmd/apogee/e2e_support_test.go — launchTUIIn, readFenceRealDir; internal/security/rules.go — write-git-control-plane Reason; internal/tools/roster_test.go — consoleFamilyNames

**Tests:** the two above.

**Acceptance:** `go test -race ./cmd/apogee/ -run 'TestE2EGuardControlPlane' && go test -race ./internal/tools/ -run 'TestShellCommandMarker'`

commit: `test(cmd/apogee): the real Terminal's shell-write view reaches the control-plane rule end to end`

## 8. Comment and doc drift sweep (apogee-bsg, prose half) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): (a) is written to the plan's wording — "active keys restate the registry's defaults" — which item 10 makes true for remember-model and ui.stall-after; until item 10 lands the template's `remember-model: true` and `ui.stall-after: 120s` still differ from the registry.
NOTES (2026-09-16): (f) the clip sentence says "a numbered body line" — `stackedRow.line` clips only rows with a number; unnumbered rows (file headers, the `⋯` rule) are not clipped.
NOTES (2026-09-16): pre-existing drift left alone, not in Files — ADR 0064 Consequences (:131-134) still says the shipped template "sets exactly one key, and every other key parses to nothing"; `docs/design/archived/technical-design.md` :206 describes the merge as wired.

**What:** Closes the prose half of apogee-bsg (the registry order is item 9). Fix, verbatim against the tree: (a) `internal/config/defaults.go` — the `defaultConfigYAML` comment no longer claims every key is commented out; say the starter's active keys restate their defaults — item 10 makes that true for remember-model and ui.stall-after — and the rest are commented documentation (ADR 0064 §4 amended wording). (b) `internal/config/defaults/config.yaml` — "the three placeholders above" → "the four placeholders above". (c) The rule: every comment naming `MergeDangerousRules` (`grep -rn MergeDangerousRules internal/security/*.go`) says it is the config-merge seam ADR 0012 fixes, called by no key today (apogee-089 would wire it); the func and its tests stay. (d) `internal/eventjson/writer.go` `Emit` doc — an opted-in `seam_closed` line DOES consume a sequence number (`writeLine` increments `seq` for every line it writes); only a forwarded-not-encoded event consumes none. (e) `docs/adr/0075-*.md` §4 — dated amendment note: twenty kinds today; `mechanism_fired`/`floor_guard` became `reaction_fired` (ADR 0076); `ref_clipped` and `seam_closed` added; list from `Kinds()` in `internal/eventjson/encode.go`. (f) `docs/layout/split-diff-layout.md` "Wrap, don't clip" — a body line is first clipped at `detailClipRunes` (160) with `…`, then wrapped. (g) `cmd/apogee/probemodel.go` Long text — the record's path is printed when the battery completes, with or without `--no-save`; an incomplete battery records nothing and prints no path (`Model.recordSection` in `internal/probe/model.go`).

**Regression guard.** (f) The split reading never clips — `splitCell.paint` wraps `c.text` whole; only the stacked reading clips (`stackedRow.line` → `clipDetail`) — so the clip sentence goes on the stacked section's "Same wrap rule" bullet (`split-diff-layout.md` :107), saying it is the stacked reading that clips at `detailClipRunes`; the split bullet's "don't clip" stands. (e) The ADR breaks its line after "Nineteen", so the negative grep can never fire: the amendment is an inline dated parenthetical in the ADR's existing form ("(Amended 2026-09-15: …)", :91) and the acceptance checks it positively.

**Files:** `internal/config/defaults.go`, `internal/config/defaults/config.yaml`, `internal/security/rules.go`, `internal/security/dangerous.go`, `internal/security/ssrf.go`, `internal/security/doc.go`, `internal/eventjson/writer.go`, `docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md`, `docs/layout/split-diff-layout.md`, `cmd/apogee/probemodel.go`.
**Read first:** internal/tui/splitdiff.go — splitCell.paint; internal/tui/diffbody.go — stackedRow.line, clipDetail; internal/eventjson/writer.go — Writer.Emit, Writer.writeLine; internal/eventjson/encode.go — Kinds; internal/config/defaults_test.go — TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt; internal/probe/model.go — Model.recordSection

**Tests:** none new; `go vet` and the existing template/probe tests guard the touched files.

**Acceptance:** `go build ./... && go test -race ./internal/config/ -run 'TestTemplate' && go test -race ./cmd/apogee/ -run 'TestProbeModel' && ! grep -rn 'three placeholders' internal/config/defaults/config.yaml && grep -n 'reaction_fired' docs/adr/0075-*.md`

commit: `docs: comment and doc drift from the 2026-09-16 fact-check`

## 9. `KeyRegistry` follows the starter template's order, pinned (apogee-bsg, order half) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): consequential edit — internal/config/registry.go: the `use-default-prompt` row's own comment ("the three rows above") made false by the row's move; reworded to name its template position (two global keys above, per-model map below). Row Desc unchanged — it still matches the template's "neither key above" wording.
NOTES (2026-09-16): consequential edit — cmd/apogee/e2e_livestate_test.go: made necessary by the registry reorder — `use-default-prompt` moving up pushes `system-prompt-layers` one row below the settings pane's first screen, so `assertPanePaintsRow` (the ADR 0067 painted-row claim, read off the golden's frame) found 0 rows; the test now walks to that row with `settingsGoDown` first and asserts the same claim on that frame. The golden itself is recorded before the walk and changed by exactly the one row the reorder moves.
NOTES (2026-09-16): `settingsTable` already had `reactions` directly after `bypass`; only the two prompt/guard moves were needed there.

**What:** Recast at the regression check (2026-09-16). Closes apogee-bsg. Reorder the rows of `KeyRegistry` in `internal/config/registry.go` to the order the starter template first mentions each key — exactly three moves: `use-default-prompt` before `system-prompt-models`; `tool-loop-breaker` directly after `tool-call-repair`; `tool-call-salvage` directly after `read-cache` — with `bypass, reactions, model-profiles` staying last. Every registry key is a template key line today, so the order test skips nothing and bites on exactly those three rows. Keys the template names only in prose keep their relative position. Then pin it: `TestRegistryFollowsTheTemplateOrder` in `internal/config/defaults_test.go` walks `SplitConfigLines(defaultConfigYAML)` with `templateMentionsSetting`, records the first mention of every registry path, and asserts `KeyRegistry` is sorted by that index (paths never mentioned as a key line are skipped). The `KeyRegistry` doc comment stays true as written.

**Regression guard.** Ratified (owner, 2026-09-16, via AskUserQuestion, correcting the earlier bsg call's wording): move the template's Reactions block (its divider through the `advise:` example) in `internal/config/defaults/config.yaml` to directly AFTER the `bypass:` block — between `# bypass: false` and the model-profiles divider — so template and registry both read `editor, bypass, reactions, model-profiles` and registry, `settingsTable` and `settingSections` keep `bypass → reactions` untouched, add that file to Files; reorder the `settingsTable` entries in `cmd/apogee/wire_settings.go` (system-prompt-models/use-default-prompt, tool-call-salvage/tool-loop-breaker, reactions after bypass) to match and add that file to Files; list and re-record the golden `cmd/apogee/testdata/frames/t16-settings-rows.txt` (`go test ./cmd/apogee/ -run TestE2ELiveStateFollowsTheRunningSession -update`) and add `TestE2ELiveStateFollowsTheRunningSession`, `TestSettingsRowsCarryTheirSection` and `TestSettingsTableIsInRegistryOrder` to Acceptance as the real order guards (drop the claim that internal/tui/settings_test.go guards the order — it never reads KeyRegistry); the new order test uses a first-index sibling over `templateKeyLine` since `templateMentionsSetting` returns a bool. The item yields to `cmd/apogee/settingsrows.go` :78-80 — `bypass` opens the Reactions section as its off-switch with the `reactions` row under it — which the template move keeps.

**Files:** `internal/config/registry.go`, `internal/config/defaults_test.go`, `internal/config/defaults/config.yaml`, `cmd/apogee/wire_settings.go`, `cmd/apogee/testdata/frames/t16-settings-rows.txt`.
**Read first:** internal/config/registry.go — KeyRegistry; internal/config/defaults_test.go — templateMentionsSetting, templateKeyLine; internal/config/defaults/config.yaml — the Reactions block, the bypass block; cmd/apogee/settingsrows.go — settingSections; cmd/apogee/settingsrows_test.go — TestSettingsRowsCarryTheirSection; cmd/apogee/wire_settings.go — settingsTable; cmd/apogee/e2e_livestate_test.go — TestE2ELiveStateFollowsTheRunningSession

**Tests:** the new order test — must fail against the pre-item tree; `TestTemplateMentionsEveryRegistryKey`, `TestSettingsRowsCarryTheirSection`, `TestSettingsTableIsInRegistryOrder` and `TestE2ELiveStateFollowsTheRunningSession` (golden `t16-settings-rows.txt`, re-recorded) are the order guards.

**Acceptance:** `go test -race ./internal/config/ && go test -race ./internal/tui/ -run 'TestSettings' && go test -race ./cmd/apogee/ -run 'TestSettings|TestE2ESettings|TestE2ELiveStateFollowsTheRunningSession'`

commit: `refactor(config): KeyRegistry rows follow the starter template's order, pinned by test`

## 10. Registry defaults follow the starter template (apogee-lq4) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): `defaultStallAfter` became a var resolved from a new `defaultStallAfterText = "120s"` const (delegate-timeout's shape) so the registry row's `Default` and the code default read one constant, as the item asks.
NOTES (2026-09-16): consequential edit — internal/config/options.go: made necessary by the remember-model default flip (its field comment said "default false").
NOTES (2026-09-16): consequential edit — cmd/apogee/wire_verbs.go: made necessary by the remember-model default flip (a skip-ladder comment called "off" the default).
NOTES (2026-09-16): consequential edit — layout.md: made necessary by the stall-after default move (the layout spec named `90s` as the default).
NOTES (2026-09-16): consequential edit — cmd/apogee/upstream_test.go: made necessary by the remember-model default flip (a fixture comment called `false` "the default"; the assertion is unchanged).
NOTES (2026-09-16): `internal/config/defaults_test.go` dropped its two template-override lines (and the `time` import) because the template's active values now equal `wantDefaults()`; the comment says why. The `internal/tui` fixtures naming `90s` (activity_test.go, model_test.go, settings_test.go) and the two refusal sentences' "like 90s or 2m" are examples, left as the plan says.

**What:** Closes apogee-lq4 under the ratified call. Depends on item 9. `remember-model` row `Default: "true"`; `ui.stall-after` row `Default: "120s"` and the code default that `uiConfig.toUISettings` / `UISettings.StallAfter` falls back to (`internal/config/config.go`) becomes 120s — one constant, read by both. Template: delete the `# Default: false.` line above `remember-model: true`. Manual `docs/manual/configuration.md`: `remember-model` reads "on by default"; `ui.stall-after` reads "default `120s`"; drop both "though the starter file…" clauses. Every test that pins `90s` or the remember-model default `false` moves to the new values (`grep -rn '90s\|remember-model' internal/config/*_test.go internal/tui/*_test.go cmd/apogee/*_test.go`).

**Regression guard.** The test grep widens to `grep -rn '90s\|90 \* time.Second\|remember-model\|RememberModel' internal/config/*_test.go internal/tui/*_test.go cmd/apogee/*_test.go` — `wantUIDefault` (config_test.go:32, used 6×) and the ui-block cases at :237 and :4840-4882 pin `90 * time.Second` — scoped to tests asserting the DEFAULT of an absent key; the tui fixtures (activity_test.go:180, model_test.go:4336, settings_test.go:1807) and the two refusal sentences' "like 90s or 2m" (config.go:368, tui/settingsapply.go:325) are examples and stay. The remember-model code default lives in the file projection, not the row: `keyAccessors`' `fromFile` becomes `fc.RememberModel == nil || *fc.RememberModel` (auto-title's shape) or `TestRegistryDefaultsReadBackFromAnEmptyFile` and the new startup test go red; rule: every comment/doc saying remember-model is off by default moves — `grep -rn 'off by default\|absent ⇒ OFF' internal/config/config.go CONTEXT.md docs/manual docs/adr/0048-*.md`. Supersedes ADR 0048 decision 1 ("One toggle, off by default", :42-51) under the ratified lq4 call — amend the ADR with a dated note.

**Files:** `internal/config/registry.go`, `internal/config/config.go`, `internal/config/defaults/config.yaml`, `docs/manual/configuration.md`, `CONTEXT.md`, `docs/adr/0048-apogee-remembers-the-model-choice-per-server.md`, plus the test files the grep names.
**Read first:** internal/config/registry.go — KeyRegistry (remember-model, ui.stall-after rows), parseStallAfter; internal/config/config.go — defaultStallAfter, uiConfig.toUISettings, keyAccessors (remember-model fromFile); internal/config/registry_test.go — TestRegistryDefaultsReadBackFromAnEmptyFile; internal/config/config_test.go — wantUIDefault, TestApplyConfigRememberModel

**Tests:** extend the registry default test (`internal/config/registry_test.go`, the one that reads each row's `Default` through `Set`/`Read`) with the two new defaults; a `cmd/apogee` startup test asserting a config that omits both keys resolves `RememberModel == true` and `UI.StallAfter == 120*time.Second`.

**Acceptance:** `go test -race ./internal/config/ && go test -race ./internal/tui/ -run 'Stall|RememberModel|Settings' && go test -race ./cmd/apogee/ -run 'RememberModel|Stall|TestRunRoot'`

commit: `feat(config): remember-model and ui.stall-after default to the values the starter template ships`

## 11. The daemon fences an `api-key-cmd:` against the Firing's workspace (apogee-add) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): the fence pre-check refuses on `security.ErrExecFromWritablePath` only; a program merely missing from PATH is left to the run (a memoised key stays the key the command once printed, a fresh use reports the missing program in the run's own words). `keyCommandArgv` is split out of `runKeyCommand` so the pre-check and the run parse the line identically.
NOTES (2026-09-16): `plantKeyCommand` (the cmd/apogee twin) and `keyCommandAt` live in `cmd/apogee/daemonfire_test.go` rather than beside `keyCommandFor` in `keysource_test.go`, keeping the edit inside the item's Files list.

**What:** Fix apogee-add under the ratified call. `internal/config/keyresolve.go`: add `func (r *KeyResolver) ResolveWithin(e ServerEntry, workspaceRoot string) (string, error)` — for a command source it judges the program against `workspaceRoot` with the same fence `runKeyCommand` applies (`security.ResolveProgram` through `userexec`, refusal sentence unchanged: `apogee: server %q: api-key-cmd: refusing to run %q: …resolves inside…`) BEFORE consulting or filling the memo, so a memoised key is refused too when this root fences it; `Resolve(e)` becomes `ResolveWithin(e, r.workspaceRoot)`. `cmd/apogee/wire_firing.go` `firingConfig`: `keys.ResolveWithin(in.entry, in.roots.workspace)`. `cmd/apogee/daemonfire.go`: the `newDaemonWiring` comment now says the resolver is rootless because the fence is judged per Firing. Producers/consumers of the fence root: `NewKeyResolver` callers in `wire_boot.go`, `probe.go`, `probemodel.go`, `daemonfire.go` — unchanged.

**Regression guard.** Export the judge from userexec — `userexec.ResolveProgram(argv0, workspaceRoot) (string, error)`, the body of `resolveProgram` with its `filepath.Abs` step, called by `Run` — and have `ResolveWithin` call it, so the manual's relative `bin/getkey.sh` shape still runs (`TestKeyResolverRunsARelativeCommandProgramOutsideTheWorkspace`) and a `%w` wrap keeps `errors.Is(err, security.ErrExecFromWritablePath)`. `resolveFiringRouting` (`wire_firing.go`) resolves the Sub-agent server's entry through the same resolver: it calls `keys.ResolveWithin(entry, in.roots.workspace)` too. `keyCommandFor` spells `os.Executable()`, always outside a workspace, so the daemonfire test links/copies the test binary under the workspace (a `plantKeyCommand` twin of keyresolve_test.go:143 in cmd/apogee, argv `-test.run=^TestAPIKeyCommandFixture$ ` + marker) and asserts the refusal on `harness.raise`'s returned error — the `failed <name> … — <err>` line is daemon.go's, never `wiring.fire`'s. Reword `docs/manual/configuration.md` :1399 and `keyresolve.go` :126-129 to "judged per Firing against the Firing's workspace"; rule: `grep -rn 'daemon' internal/config/keyresolve.go docs/manual/configuration.md`. Supersedes CHANGELOG.md :3125's "`probe model` and `daemon` hold no workspace and so fence nothing there" for the daemon, under the ratified add call.

**Files:** `internal/config/keyresolve.go`, `internal/config/keyresolve_test.go`, `internal/userexec/userexec.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`, `docs/manual/configuration.md`.
**Read first:** internal/config/keyresolve.go — KeyResolver.Resolve, runKeyCommand, NewKeyResolver; internal/userexec/userexec.go — Run, resolveProgram; cmd/apogee/wire_firing.go — firingConfig, resolveFiringRouting; cmd/apogee/daemonfire_test.go — raise

**Tests:** `keyresolve_test.go`: `TestKeyResolverResolveWithinRefusesAProgramInsideTheGivenRoot` (rootless resolver, program under the root → refusal; a second call with a root elsewhere → runs); `TestKeyResolverResolveWithinRefusesAMemoisedKeyTheRootFences` (resolve once outside, then within the fencing root → refused, memo untouched). `daemonfire_test.go`: `TestDaemonFireFencesTheKeyCommandInTheEntrysWorkspace` — an entry whose `api-key-cmd:` program lives in its own `Run.Workspace` (a `plantKeyCommand` twin, not `keyCommandFor`) → `harness.raise`'s returned error carries the refusal sentence; a sibling entry with the program outside resolves. The new tests must fail against the pre-item tree.

**Acceptance:** `go test -race ./internal/config/ -run 'TestKeyResolver' && go test -race ./cmd/apogee/ -run 'TestDaemonFire|TestAPIKey|TestFiring'`

commit: `fix(daemon): an api-key-cmd is fenced against the Firing's workspace, memo included`

## 12. Startup notices reach the transcript (apogee-2sj) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): consequential edit — cmd/apogee/wire.go: made necessary by `w.startupNotices`, which the item names on `rootWiring`; that struct lives in wire.go, not wire_boot.go.
NOTES (2026-09-16): the three `fmt.Fprintln` sites in `announceConfinement` funnel through one `rootWiring.announce` helper (print + append) so the stderr line and the handed-over slice cannot drift; the printed strings and their order are unchanged.
NOTES (2026-09-16): `TestE2EAutoDegradationJourneyOnAnIncapableHost` takes the plan's first option (phase 3 asserts `rec.opts.StartupNotices` equals the captured stderr and is free of "running UNCONFINED"); no separate deny-confiner driven case was added.
NOTES (2026-09-16): the manual never described the stderr-only startup line, so no manual edit was needed; `docs/manual/daemon.md` :88-91 ("in the same words an unconfined interactive launch prints") stays true.

**What:** Closes apogee-2sj under the ratified call. `cmd/apogee/wire_boot.go` `announceConfinement` keeps its `os.Stderr` lines and ALSO appends each string it prints (`unconfinedAutoWarning`, `probe.DegradedNotice`, `probe.ResidualNotice`) to `w.startupNotices []string`; `wire_options.go` `options()` passes `StartupNotices: w.startupNotices`. `internal/tui/tui.go`: `Options.StartupNotices []string` (doc: "printed on stderr before the alternate screen opened; repeated here so the moment is not lost"). `Model` posts them exactly as `noteColorSchemeWarnings` posts `ColorSchemeWarnings` — one `addEphemeralNote` per notice, in order, after the colour-scheme warnings; the note text is the notice verbatim. Headless and daemon are untouched.

**Regression guard.** Neither test can reach `w`: `rootWiring` never leaves `runRoot`, and `recordingLauncher` (`root_test.go`) records the `tui.Options` — so `TestRunRootConfinementStartupNotices` asserts `rec.opts.StartupNotices`, joined with `"\n"+"\n"`, equals the captured stderr in every cell (`PrewarmLabelWalk` prints nothing off Windows). `TestE2EAutoDegradationJourneyOnAnIncapableHost` has no driver and its phase 3 runs the REAL host confiner, so a `WaitText` there is unwritable and a degraded-notice check host-dependent: assert `rec.opts.StartupNotices` in phase 3 instead (equal to the sentences `captureStderr` caught, free of "running UNCONFINED"), or add a separate non-parallel driven case that swaps `newConfiner` to `platform.NewDenyConfiner()` before `launchTUI(..., "--mode", "auto")` and `WaitText`s the first line.

**Files:** `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_options.go`, `cmd/apogee/wire_boot_test.go`, `internal/tui/tui.go`, `internal/tui/model.go`, `internal/tui/model_test.go`, `cmd/apogee/confinement_e2e_test.go`.
**Read first:** cmd/apogee/wire_boot.go — announceConfinement, unconfinedAutoWarning, rootWiring; cmd/apogee/wire_options.go — options; internal/tui/model.go — noteColorSchemeWarnings; internal/tui/theme_test.go — TestNewModelNotesTheColorSchemeWarnings; cmd/apogee/wire_boot_test.go — captureStderr; cmd/apogee/root_test.go — recordingLauncher

**Tests:** `internal/tui/model_test.go`: `TestStartupNoticesPostAsEphemeralNotes` (two notices → two notes in order, absent from the saved transcript — read the ephemeral assertion from the colour-scheme warning test). `wire_boot_test.go`: `TestRunRootConfinementStartupNotices` also asserts `rec.opts.StartupNotices` equals the stderr lines. `confinement_e2e_test.go`: `TestE2EAutoDegradationJourneyOnAnIncapableHost` additionally asserts `rec.opts.StartupNotices` in phase 3 (or a separate driven deny-confiner case `WaitText`s `apogee: auto mode is gating terminal commands`). The new tests must fail against the pre-item tree.

**Acceptance:** `go test -race ./internal/tui/ -run 'TestStartupNotices|TestColorScheme' && go test -race ./cmd/apogee/ -run 'TestRunRootConfinementStartupNotices|TestE2EAutoDegradation|TestRunRootWires'`

commit: `feat(tui): startup confinement notices repeat in the transcript as ephemeral notes`

## 13. Live config reload announces loader notices (apogee-ibd) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): consequential edit — cmd/apogee/keymigrate_test.go: made necessary by the `ReloadConfig`/`changed()` signature change to `tui.ConfigReload` (one call site reads `.Applied`).
NOTES (2026-09-17): consequential edit — docs/manual/configuration.md: made necessary by the live re-read now announcing notices ("printed at start-up only" sentence rewritten).
NOTES (2026-09-17): consequential edit — internal/config/unknownkeys.go: made necessary by the same ("through notify at startup" file comment now names the live re-read too).
NOTES (2026-09-17): a second tui test, `TestSettingsEditExitPostsTheLoaderNotices`, pins the editor-exit path the item's What names beside the watcher path; `configwatch_apply_test.go`'s `watchedConfig.reload` helper keeps its `[]tui.AppliedSetting` return and unwraps `.Applied` so its five callers stay untouched.

**What:** Closes apogee-ibd. `cmd/apogee/settingsedit.go` `externalEdit.projection` passes `func(string) {}` to `config.ApplyConfig`, discarding unknown-key and roster notices. Changed representation: `tui.Options.ReloadConfig` becomes `func() (ConfigReload, error)` with `type ConfigReload struct { Applied []AppliedSetting; Notices []string }` in `internal/tui/tui.go`; `projection` collects notices into `fileProjection.notices` and `changed()` returns them. Consumers (all updated): `internal/tui/settingswatcher.go` `foldSettingsEdit`, `foldConfigChanged`, `applyReloaded`; test doubles that set `ReloadConfig` in `internal/tui/skill_test.go`, `settings_test.go`, `settingsapply_test.go`, `cmd/apogee/settingsedit_test.go`, `configwatch_apply_test.go`, `confinement_e2e_test.go`. Posting: on both paths (watcher and editor exit) each notice is one `m.transcript.addNote(notice)` line, verbatim loader text (e.g. `apogee: config <path>: unknown key "bogus" at line 3 is ignored`), after the `config changed on disk — applied: …` line where that line posts. Also drop the "(ADR 0041; bead apogee-ibd)" clause in `internal/config/unknownkeys.go`'s comment — the notice now reaches live re-reads.

**Regression guard.** The watcher fires on apogee's OWN writes (`refresh`, settingsedit.go:285-300) and `foldConfigChanged` re-reads on every report, so a persistent notice (an unknown key, a roster typo, a malformed unconfined-hosts entry) would post — and be PERSISTED into the record — on every pane commit, `/confine off --save`, model-choice write and editor exit: `changed()` carries `notices` on `fileProjection` and returns only the notices absent from `before.notices`; the three tests still bite (each starts from a baseline without the key). The "(ADR 0041; bead apogee-ibd)" clause is in `internal/config/config.go` :2917-2921 — the comment above `for _, unknown := range unknownKeys(data)` in `parseConfigFile` — not in unknownkeys.go. `skill_test.go`, `settingsapply_test.go` and `confinement_e2e_test.go` set no `ReloadConfig`; only settings_test.go, settingsedit_test.go and configwatch_apply_test.go do. The e2e `WaitText` matches one screen row and the note wraps under a `t.TempDir` path: `WaitText` the quoted key alone (`"bogus-key"`) or flatten the frame (`flatten(drv.Frame().String())`) before the Contains.

**Files:** `cmd/apogee/settingsedit.go`, `cmd/apogee/settingsedit_test.go`, `internal/tui/tui.go`, `internal/tui/settingswatcher.go`, `internal/tui/settings_test.go`, `cmd/apogee/configwatch_apply_test.go`, `cmd/apogee/e2e_livestate_test.go`, `internal/config/config.go`.
**Read first:** cmd/apogee/settingsedit.go — externalEdit.projection, externalEdit.changed, externalEdit.refresh, fileProjection; internal/tui/settingswatcher.go — foldConfigChanged, foldSettingsEdit, applyReloaded; internal/config/config.go — parseConfigFile

**Tests:** `settingsedit_test.go`: `TestExternalEditReloadCarriesTheLoaderNotices` (a file gaining an unknown key → `Notices` holds the exact `unknownKeyNotice` sentence). `settings_test.go`: `TestConfigWatchPostsEveryLoaderNotice` (two notices → two note lines after the applied line; none → no extra line). `e2e_livestate_test.go`: a case writing `bogus-key: 1` into the live config and `WaitText`ing `"bogus-key"` (the quoted key alone, or the flattened frame contains the whole notice). The new tests must fail against the pre-item tree.

**Acceptance:** `go test -race ./internal/tui/ -run 'TestConfigWatch|TestSettings' && go test -race ./cmd/apogee/ -run 'TestExternalEdit|TestE2ELiveState|TestConfigWatch|TestRunRootWires'`

commit: `feat(config): a live config reload announces the loader's notices in the transcript`

## 14. `ConfigHost` folds the config-file callbacks on `tui.Options` (apogee-970) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): the three offer answers' nil degrade now reads through the act's own error — "could not move workstation's key: moving a key is not available in this build" where the per-func check said "moving a key is not available in this build" bare; those paths are unreachable with a nil host (the offers are gated on `Config == nil`), and the new test pins the sentence by containment.
NOTES (2026-09-17): the recording halves of `fakeKeyWriter`/`fakeSubAgentsMigrator` fold into one `configWriteLog` recorder wired into `fakeConfigHost`'s func fields via a `configSeams` helper (serverSeams' idiom); the plan's ten internal/tui and seven cmd/apogee test files were a floor — only the files that name the eight moved, the others never did.
NOTES (2026-09-17): `foldConfigChanged` keeps a `Config == nil` gate beside `awaitConfigChange`'s (ADR 0054 3a): with the noop's `ReloadConfig` refusing, a stray `configChangedMsg` would otherwise count as an unreadable config; the old nil-func branch returned unchanged with no wait, which the gate reproduces exactly.
NOTES (2026-09-17): consequential edit — internal/config/configwrite.go: made necessary by the field's move (its doc named `Options.SaveHostAcknowledgement`); cmd/apogee/keymigrate.go, cmd/apogee/settingsedit.go, cmd/apogee/wire_server.go, internal/tui/clipboard.go, internal/tui/doc.go, internal/tui/prebound_test.go carry the go-doc link sweep the Regression guard prescribes.

**What:** Closes apogee-970 under the ratified call. Depends on items 12, 13. In `internal/tui/tui.go` declare `type ConfigHost interface` with the eight methods `SaveHostAcknowledgement() (string, error)`, `ReloadConfig() (ConfigReload, error)`, `AwaitConfigChange(context.Context) bool`, `MigrateKey(entry string) (string, error)`, `KeepPlaintextKey(entry string) (string, error)`, `MigrateSubAgentsServer(entry string) (string, error)`, `RecordModelChoice(model string) (bool, error)`, `ExternalEditSpec(path string) (EditorCommand, error)`; `Options.Config ConfigHost` replaces the eight func fields; a nil `Config` degrades exactly as each nil func did (ADR 0054 D2 — one `configHostOrNoop` guard, not eight nil checks). `cmd/apogee/wire_options.go`: a `configHost` adapter struct beside `settingsHost` wires the same eight seams. Every reader of the eight fields in `internal/tui` (`grep -n 'opts\.\(SaveHostAcknowledgement\|ReloadConfig\|AwaitConfigChange\|MigrateKey\|KeepPlaintextKey\|MigrateSubAgentsServer\|RecordModelChoice\|ExternalEditSpec\)' internal/tui/*.go`) and every test double naming them (the files under Files) move; `fakeKeyWriter`/`fakeSubAgentsMigrator` (`keymigration_test.go`) fold into one `fakeConfigHost` with per-method func fields. `GenerateTitle`, `OnAutoTitle`, `ReloadSkills`, `ReportActivity`, `ReportUpstream` stay bare.

**Regression guard.** Two member gates are decide-before-call, not degrades an answer can carry: `openKeyMigration` raises no offer when `MigrateKey == nil` and `openSubAgentsMigration` when `MigrateSubAgentsServer == nil`, pinned at member granularity by `TestKeyMigrationOfferNeedsAStoreAnEntryAndASeam`'s "no seam" case and `TestSubAgentsMigrationNeedsAnEntryAndASeam` — both gates collapse to `Config == nil` (equivalent in the binary: `keyOffer` and `subAgentsFlagged` are set with the seams, keymigrate.go) and the seam halves of those two tests are recast to `opts.Config = nil`; "pass unchanged in intent" does not hold for them. Rule: every go-doc link naming one of the eight (25 non-test sites today, e.g. `[Options.RecordModelChoice]`, `[tui.Options.ReloadConfig]`) is rewritten to `ConfigHost.<member>` — `grep -rn 'Options\.\(SaveHostAcknowledgement\|ReloadConfig\|AwaitConfigChange\|MigrateKey\|KeepPlaintextKey\|MigrateSubAgentsServer\|RecordModelChoice\|ExternalEditSpec\)' --include=*.go internal cmd`. `internal/tui/settingsedit.go` does not exist (the readers are in settingswatcher.go). Supersedes ADR 0054 D7's example (:95 names `SaveHostAcknowledgement` as "a single act with no siblings") under the ratified 970 call — the dated line amends that example, not only adds the third family.

**Files:** `internal/tui/tui.go`, `internal/tui/confine.go`, `internal/tui/settingswatcher.go`, `internal/tui/keymigration.go`, `internal/tui/picker.go`, `internal/tui/{autotitle,heartbeat,keymigration,confine,settings,settingsapply,schedule,picker,skill,model}_test.go`, `cmd/apogee/wire_options.go`, `cmd/apogee/{configwatch_apply,confinement_e2e,keymigrate,settingsedit,schedule,title,upstream}_test.go`, `docs/adr/0054-options-groups-host-capabilities-into-named-interfaces.md`.
**Read first:** internal/tui/tui.go — Options, SettingsHost; internal/tui/keymigration.go — openKeyMigration, openSubAgentsMigration; cmd/apogee/wire_options.go — options, settingsHost; cmd/apogee/keymigrate.go — keyMigrator; internal/tui/keymigration_test.go — fakeKeyWriter

**Tests:** `TestNilConfigHostDegradesLikeTheUnwiredFuncs` in `internal/tui/tui_test.go` (each act with `Config == nil` yields the same outcome the nil func did — read those outcomes from the current nil checks before removing them); `TestRunRootWiresTheExternalEditSeams` and the key-migration tests pass, the seam halves of the two offer-gate tests recast to `opts.Config = nil`. ADR 0054 gains a dated line naming `ConfigHost` as the third family and amending D7's `SaveHostAcknowledgement` example.

**Acceptance:** `go build ./... && go vet ./internal/tui/ ./cmd/apogee/ && go test -race ./internal/tui/ && go test -race ./cmd/apogee/ -run 'TestRunRoot|TestKeyMigrat|TestExternalEdit|TestConfigWatch|TestConfinement|TestE2EAutoDegradation'`

commit: `refactor(tui): ConfigHost groups the eight config-file acts on Options (ADR 0054)`

## 15. `apogee probe config` (apogee-lpi) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): `%w` would have spelled the sentinel's text into refusal sentences two tests pin byte-for-byte (`configmigrate_test.go` :1257, :1285), so `ErrRetiredShape` is attached through a small `retiredShapeRefusal` wrapper whose `Is` method answers `errors.Is(err, config.ErrRetiredShape)`; the sentences are unchanged and the probe branches on `errors.Is` exactly as planned.
NOTES (2026-09-17): the `resolved` section keeps `field()`'s `"  %-Ns %s"` layout but sizes N to the registry's longest path (23 chars) instead of 14, so the 62 rows align; a `%-14s` column would have broken on more than half the keys.
NOTES (2026-09-17): consequential edit — cmd/apogee/doc.go: made necessary by the new probeconfig.go (the package map TestDocMapNamesEveryFile enumerates every file).
NOTES (2026-09-17): consequential edit — README.md, docs/manual/README.md: made necessary by the new verb (the two front-door lines enumerate the probe family as host/model/terminal).
NOTES (2026-09-17): a third test, `TestProbeConfigFailsOnAMalformedFile`, pins that a non-retired-shape reader error is the command's own failure rather than a `migration` finding; the test helper is `seedConfigHome` because `writeProbeConfig` already exists in probemodel_test.go.

**What:** Closes apogee-lpi under the ratified call. New verb `config` under `probe` (`cmd/apogee/probeconfig.go`, registered in `probe.go`'s `newProbeCommand`; flag `--config` only). It reads the file through the LIVE path — `config.LoadFileConfig(config.FilePath(dir), os.ReadFile, collect)` — so it never migrates or writes. Output, `Host.Report()`'s layout (`field()` = `"  %-14s %s"`): header `apogee probe — config report` + `  (nothing is written; the file is read the way a live reload reads it)`; section `notices` listing every collected notice one per line (or `  (none)`); section `migration` — when `LoadFileConfig` returns the legacy-shape refusal, its sentence verbatim and the `resolved` section is replaced by `  (start apogee once to migrate the file, then re-run)`; otherwise `  (none)` and section `resolved` with one `field(row.Path, value)` per `config.KeyRegistry` row in registry order, `value = row.Read(o)` or `row.Default` when `Read` is empty, and `Masked` rows rendered as `••••` when non-empty. Printed via `sanitize.StripEscapes` like `probe host`. `docs/manual/probe.md` documents the verb in the family's style.

**Regression guard.** No `KeyRegistry` row is `Masked` and none renders an api-key (the `servers` row reads "N servers"), so drop the masked assertion: assert the seeded key's value appears nowhere in the output and `servers` reads its summary; keep the `••••` branch as a one-line guard on `row.Masked`. A `sub-agents:`-era file is NOT the legacy shape (`servers[N].sub-agents` is exempted by `walkUnknownKeys` and folded only by the consented TUI offer; `LoadFileConfig` returns Options with no error): the fixture is the retired top-level `endpoint:`/`api-key:`/`host-alias:`/`model:` quadruple (→ `liveLegacyRefusal`) or a top-level `hooks:` list (`liveReactionsRefusal`). Those refusals are plain `fmt.Errorf`s with no sentinel: add `var ErrRetiredShape = errors.New(…)` in `configmigrate.go`, wrapped via `%w` by `legacyRefusal` and `liveReactionsRefusal`; the probe branches on `errors.Is` and returns any other error as the command's own. `runProbe` always passes `--workspace`, which the verb does not declare, and `unknownKeyNotice` is unexported: use a `runProbeModel`-shaped helper passing `config --config <home>` and spell the notice literally — `apogee: config <path>: unknown key "bogus" at line N is ignored`.

**Files:** `cmd/apogee/probeconfig.go`, `cmd/apogee/probeconfig_test.go`, `cmd/apogee/probe.go`, `cmd/apogee/probe_test.go`, `internal/config/configmigrate.go`, `docs/manual/probe.md`.
**Read first:** cmd/apogee/probe.go — newProbeCommand; cmd/apogee/probemodel.go — probeModelCommand (the --config-only verb shape); internal/probe/host.go — Host.Report, field; internal/config/config.go — LoadFileConfig; internal/config/configmigrate.go — legacyRefusal, liveReactionsRefusal; cmd/apogee/probemodel_test.go — runProbeModel

**Tests:** `probeconfig_test.go` with a `runProbeModel`-shaped helper (`config --config <home>`): `TestProbeConfigReportsUnknownKeysAndResolvedValues` (a home whose config carries `bogus: 1` and `ui.stall-after: 45s` → the literal `apogee: config <path>: unknown key "bogus" at line N is ignored` line under `notices`, `ui.stall-after: 45s` under `resolved`, the seeded `api-key` value nowhere in the output and `servers` as its summary); `TestProbeConfigRefusesToMigrateALegacyFile` (a retired top-level `endpoint:`/`api-key:`/`host-alias:`/`model:` file → the refusal sentence under `migration`, file bytes unchanged after the run); `TestSubcommandsRegistersProbe` gains the `config` child.

**Acceptance:** `go test -race ./cmd/apogee/ -run 'TestProbeConfig|TestProbeCommand|TestSubcommandsRegistersProbe'`

commit: `feat(probe): apogee probe config reports notices, pending migration and resolved registry values`

## 16. One pointer table keyed by `framePane` (apogee-agk, table half) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): the `rect func(Model) (…rect, bool)` field the What sketches was not added — neither chain reads a rect today (every handler hit-tests itself through popupPaneHit / reportPaneRect), so the field would have been dead; the guard's spelling (`pane`, `click`, `wheel`) is what shipped.
NOTES (2026-09-17): the report entries call `handleReportClick(kind)` / `reportWheel(kind)` directly through `reportPointer(kind)`, so the six one-line wrappers `handleUsageClick`/`usageWheel`, `handleInspectorClick`/`inspectorWheel`, `handleThinkingClick`/`thinkingWheel` lost their only caller and were deleted (usage.go, inspector.go, thinkingpane.go); `sessions.go`, `picker.go`, `approval.go`, `autocomplete.go` and `reportpane.go` needed no change — their handlers are entered by method expression and their comments still read true.
NOTES (2026-09-17): the settings `sel` drop is pinned by the new `TestSettingsEntryDropsTheHighlightOnAClickItDoesNotClaim`, asked of `pointerPanes[0].click` itself; `TestTranscriptDragOutlivesASettingsHighlight` keeps covering the end-to-end path.

**What:** Recast at the regression check (2026-09-16). First half of apogee-agk (bead closes at item 17). In `internal/tui/mouse.go` declare a package-level `var pointerPanes = []pointerPane{…}` — `pointerPane{pane framePane; rect func(Model) (…rect, bool); click func(Model, tea.MouseClickMsg) (Model, tea.Cmd, bool); wheel func(Model, tea.MouseWheelMsg) (Model, tea.Cmd, bool)}` (exact signatures follow what `handleMouseClick` and `foldMouseWheel` need today) — ordered `paneSettings, paneUsage, paneInspector, paneThinking, paneBrowser, panePicker, panePrompt, paneDropdown`, i.e. the current arm order. `handleMouseClick` walks the table then falls through to `handleFooterModeClick` and the prompt/transcript rects; `foldMouseWheel` walks the same table. The per-pane funcs (`handleSettingsClick`, `handleReportClick(kind)`, `handleBrowserClick`, `handlePickerClick`, `handleAskClick`+`handleApprovalClick` folded as the `panePrompt` entry, `handleDropdownClick`; `settingsWheel`, `reportWheel(kind)`, `browserWheel`, `pickerWheel`, `promptWheel`, `dropdownWheel`) keep their bodies and their key verdicts (ADR 0053 D3); the table is not on `Model` (ADR 0011). Binding: behaviour is byte-identical — the popup goldens are the guard.

**Regression guard.** the table's order is today's click-chain order — settings, usage, inspector, thinking, browser, picker, prompt, dropdown — NOT transcriptSlotPanes order; rename the test to `TestPointerPanesWalkInTheClickChainOrder` and pin those eight literally (none twice); the paneSettings entry's click func performs the `m.settings.sel = promptSel{}` drop on non-claim exactly as today's chain does, and a test pins it (a click outside the rows after a drag in the value buffer leaves `settings.sel.active` false). The item yields to `mouse.go` :471-474 — the report trio is asked before the modal half and the ask/approval pane after it — which the click-chain order keeps. The fields are spelled `click func(m, pre Model, msg tea.MouseClickMsg) (Model, tea.Cmd, bool)` and `wheel func(Model, tea.MouseWheelMsg) (Model, bool)` (no Cmd exists on the wheel side, `foldMouseWheel` :1809): every click handler takes the live `m` and the pre-click frame `pre` (`handleSettingsClick` :1306, `handleReportClick` reportpane.go:443), so the walk composes `pre := m.withFrameSpans()` once and hands it to every entry — `TestTheClickChainKeepsItsFrameToItself` and `TestClickInsideTheInspectorSurvivesTheReportDismissal` are the guards.

**Files:** `internal/tui/mouse.go`, `internal/tui/usage.go`, `internal/tui/inspector.go`, `internal/tui/thinkingpane.go`, `internal/tui/sessions.go`, `internal/tui/picker.go`, `internal/tui/approval.go`, `internal/tui/autocomplete.go`, `internal/tui/reportpane.go`, `internal/tui/mouse_test.go`.
**Read first:** internal/tui/mouse.go — handleMouseClick, foldMouseWheel, handleSettingsClick, popupPaneHit; internal/tui/reportpane.go — handleReportClick, reportWheel; internal/tui/model.go — framePane, withFrameSpans; internal/tui/mouse_test.go — TestTheClickChainKeepsItsFrameToItself

**Tests:** `TestPointerPanesWalkInTheClickChainOrder` in `mouse_test.go` (the table's panes are, literally, `paneSettings, paneUsage, paneInspector, paneThinking, paneBrowser, panePicker, panePrompt, paneDropdown` — none twice); a test pinning the settings `sel` drop (a click outside the rows after a drag in the value buffer leaves `settings.sel.active` false); every existing `mouse_test.go` test — `TestTheClickChainKeepsItsFrameToItself`, `TestClickInsideTheInspectorSurvivesTheReportDismissal` and `TestTranscriptDragOutlivesASettingsHighlight` named — and `cmd/apogee/e2e_popups_test.go` (goldens `testdata/frames/popup-*.txt`) pass unchanged — never regenerate a golden in this item.

**Acceptance:** `go test -race ./internal/tui/ -run 'Click|Wheel|PointerPanes' && go test -race ./cmd/apogee/ -run 'TestE2EPopup' && git diff --quiet -- cmd/apogee/testdata/frames/`

commit: `refactor(tui): pointer clicks and wheels walk one table keyed by framePane`

## 17. `pickerOffering` folds the picker switches (apogee-agk, picker half) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): the table is declared `var pickerOfferings map[pickerKind]pickerOffering` and filled in `init()` rather than by its declaration — a literal initializer is an initialization cycle (pickerOfferings → startProfileLoad → layout → renderPicker → pickerListContent → pickerHintFor → pickerOfferings), the same loop `settingsSteps` (settings.go) already solves the same way; the doc comment says so.
NOTES (2026-09-17): two named legends added (`pickerChooseHint`, `pickerStopHint`) so the table rows carry constants rather than five copies of one literal, plus a `fixedTitle` helper for the kinds whose title is a constant; the four lookup functions keep their names and carry no missing-key branch (the totality test is the guard).

**What:** Closes apogee-agk. Depends on item 16. In `internal/tui/picker.go` a package-level `var pickerOfferings = map[pickerKind]pickerOffering{…}` with `pickerOffering{title func(Model) string; hint string; rows func(Model) []popupRow; accept func(Model, int) (tea.Model, tea.Cmd)}` (field types follow what the four switches return today) replaces the switches in `pickerHintFor`, `pickerOfferingRows`, `pickerTitle`, `acceptPicker` — each becomes a one-line lookup; every value of the enum, read from the tree, stays. The type's doc comment moves from "one switch away" to "one table row away" and keeps the ADR 0011 rationale (the table is package-level; `Model` holds the kind only).

**Regression guard.** the pickerKind enum has twelve values on the gated tree (the fork wave added `pickerFork`) — the item says "every value of the enum, read from the tree" rather than eleven, and the offering test iterates the enum's range. Rows are `popupRow` (popup.go:167; no `pickerRow` type exists) and `acceptPicker` dispatches on the offering INDEX — `pickerServer` / `pickerSubAgentsServer` re-read their list by that index at accept time, so a `servers:` block that shrank under the open overlay costs the accept and not the process — hence `rows func(Model) []popupRow` and `accept func(Model, int) (tea.Model, tea.Cmd)`; `pickerFork`'s entry is `forkRows(m.transcript.forkPoints())` with accept `acceptFork`; `pickerHintFor(kind)`, `pickerTitle()`, `pickerOfferingRows()`, `acceptPicker()` keep their names as the callers' (mouse.go, fork_test.go, keymigration_test.go, picker_test.go).

**Files:** `internal/tui/picker.go`, `internal/tui/picker_test.go`.
**Read first:** internal/tui/picker.go — pickerKind, pickerHintFor, acceptPicker, pickerTitle, pickerOfferingRows; internal/tui/fork.go — forkRows, acceptFork; internal/tui/popup.go — popupRow

**Tests:** `TestEveryPickerKindHasAnOffering` in `picker_test.go` (iterates the enum's range — `pickerModel` through the last declared constant, `pickerFork` on the gated tree — asserting each has an entry with non-nil `rows` and `accept`); the existing `picker_test.go` and popup goldens pass unchanged.

**Acceptance:** `go test -race ./internal/tui/ -run 'Picker' && go test -race ./cmd/apogee/ -run 'TestE2EPopup' && git diff --quiet -- cmd/apogee/testdata/frames/`

commit: `refactor(tui): pickerOffering folds the four pickerKind switches into one table`

## 18. internal/tui `t.Parallel` sweep — group A (apogee-11s) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): no test proved non-deterministic under `-race -count=3`, so no `// serial:` beyond the two clipboard-seam tests; those two carry the comment inside their doc block, directly above the func line.
NOTES (2026-09-17): ten `t.Run` literals stay serial under a parallel parent per the guard — `TestTranscriptNeverScrollsSideways` (copies the parent's `base` Model), `TestClickOnBottomChromeSelectsNothing`, `TestTheClickChainKeepsItsFrameToItself`, `TestBrowserWheelIsSwallowedByARenameOrAConfirm`, `TestBrowserClickIsSwallowedByARenameOrAConfirm`, `TestClickOnTheFooterModeMarkerOpensTheModePicker` (3), `TestClickOnTheFooterModeMarkerIsRefusedWhereThePickerCannotBeAnswered` (all share a parent Model by value, one unlocked `*paintCache`), and `TestToolSummariesRenderThroughThePresenter` (parent `defer srv.Close()` kept as written).

**What:** First of three sweeps (bead closes at item 21). Depends on item 17. Files: `model_test.go`, `mouse_test.go`, `settings_test.go`, `sessions_test.go`, `keyclaim_test.go`, `commandrun_test.go`, `clearscreen_test.go`, `toolsummary_pin_test.go`, `startupbox_test.go`. Rule: `t.Parallel()` becomes the first statement of every top-level test (and every `t.Run` literal) that does not — itself or through a helper — call `t.Setenv`/`t.Chdir`/`os.Setenv`/`os.Chdir` or assign a package-level var of `internal/tui`. The only seam is `writeSystemClipboard` (swapped by `recordSystemClipboard`): `TestDragCopyAlsoWritesTheSystemClipboard` and `TestSystemClipboardFailureStillConfirmsTheCopy` stay serial. A test that proves non-deterministic under `-count=3` is fixed at its cause (a shared path, an order-dependent fixture) or stays serial with a one-line `// serial: <reason>` comment directly above it — never a `t.Parallel()` that flakes.

**Regression guard.** The `t.Run` clause: a literal gets `t.Parallel()` only when it builds its own fixture and closes over no parent-scope Model/Options/resource the parent defers or mutates; otherwise it stays serial under its parent (no comment needed) — `TestToolSummariesRenderThroughThePresenter` holds `defer srv.Close()` over subtests whose `web_search` case dials `srv.URL`, a deterministic FAIL under parallel literals unless the parent moves `srv.Close` to `t.Cleanup`. The subtest/serial clause names the concrete shared hazard the reviewer found — by-value Model copies sharing one unlocked `*paintCache` (model.go / paintcache.go) — and drops the no-op sentence "tuitest waits keep their defaults" (internal/tui imports nothing from internal/tuitest); the bead's leak-attribution premise does not apply and the item must not cite it.

**Files:** the nine test files above.
**Read first:** internal/tui/model_test.go — newTestModel, step, testOpts; internal/tui/mouse_test.go — recordSystemClipboard, TestDragCopyAlsoWritesTheSystemClipboard; internal/tui/toolsummary_pin_test.go — TestToolSummariesRenderThroughThePresenter; internal/tui/paintcache.go — paintCache, store

**Tests:** none new.

**Acceptance:** `go test -race -count=3 ./internal/tui/ && go test -race -count=1 -parallel 4 ./internal/tui/`

commit: `test(tui): t.Parallel across the model, mouse, settings and sessions driver tests`

## 19. internal/tui `t.Parallel` sweep — group B (apogee-11s) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): no test in the ten files calls `t.Setenv`/`t.Chdir`/`os.Setenv`/`os.Chdir` or assigns a package-level var (directly or through a helper), and none proved non-deterministic under `-race -count=3` or `-parallel 16`, so every top-level test carries `t.Parallel()` and no `// serial:` comment was needed.
NOTES (2026-09-17): three `t.Run` literals stay serial under a parallel parent per the guard — `TestScheduleEventsRenderAsNotes`'s table (steps one parent Model by value, one unlocked `*paintCache`) and `TestReportingCommandsRunWhileRunning`'s table (closes over the parent's `Options`); the two one-line literals of `TestScrollWhileRunningViaPgKeysAndWheel` were expanded to three lines so `t.Parallel()` could be their first statement (`scrolled` builds its own Model per call).
NOTES (2026-09-17): literals closing over only immutable parent values — a const window, an escape string, a theme, a `popupSpec` by value, a `[]string` row, a known-skills func — or a builder closure that returns a fresh transcript/Model per call (`fixture`, `seed`, `openOn`) got `t.Parallel()`, matching item 18's treatment of `raise`, `blobFor` and `clickLine`.

**What:** Depends on item 18; same rule and same serial-exception wording as item 18. Files: `picker_test.go`, `transcript_test.go`, `interject_test.go`, `minilang_test.go`, `schedule_test.go`, `skill_test.go`, `mdtable_test.go`, `inspector_test.go`, `popup_test.go`, `command_test.go`.

**Files:** the ten test files above.
**Read first:** internal/tui/picker_test.go — wireRebind, wireHeartbeat, typeCommand; internal/tui/minilang_test.go — newTestModelEng; internal/tui/model_test.go — newTestModel, step; internal/tui/transcript_test.go — TestForkPointsSkipFoldedPrompts (subtests share an outer `tr` read-only); internal/tui/schedule_test.go — the one existing t.Parallel

**Tests:** none new.

**Acceptance:** `go test -race -count=3 ./internal/tui/ && go test -race -count=1 -parallel 4 ./internal/tui/`

commit: `test(tui): t.Parallel across the picker, transcript, interjection, schedule and skill driver tests`

## 20. internal/tui `t.Parallel` sweep — group C (apogee-11s) — ✅ DONE (2026-09-17)

NOTES (2026-09-17): the sweep covered every `internal/tui/*_test.go` outside items 18–19 that still held a serial test — the item's named list plus `actuation_test.go`, `lineeditor_test.go`, `presenter_test.go` and `transcriptbridge_test.go`, whose serial residue was one or two literals each; no test in them calls `t.Setenv`/`t.Chdir`/`os.Setenv`/`os.Chdir` or assigns a package-level var (directly or through a helper), and none proved non-deterministic under `-race -count=3`, `-parallel 4` or `-parallel 16`.
NOTES (2026-09-17): four top-level tests stay serial with a `// serial:` comment directly above the func line — `TestE2ELiveModel`, `TestSmokeLiveProfileSeam` (env-gated live runs against one shared model server) and `TestConPTYPaintsTheIntendedFrame`, `TestConPTYChildProcess` (`conptyAttachConsole` swaps the process-wide `os.Stdin`/`os.Stdout`); `TestConPTYPaintsTheIntendedFrame`'s off-Windows twin in `conpty_other_test.go` is a bare skip and got `t.Parallel()`.
NOTES (2026-09-17): 29 `t.Run` literals stay serial under a parallel parent per the guard — those repainting a parent-scope Model (one unlocked `*paintCache`): `TestBlockCursorKeysReturnToThePrompt` (3, `base`), `TestActuationLatchRefusesEveryMoveWhileHeld`, `TestAutocompleteSelectionStaysOnScreenAtEveryBudget`, `TestFooterDropsSegmentsBeforeTheModeMarker`, `TestReportPaneBreathes` (2), `TestUsageKeysScrollTheReport` (3) and the 13 reportpane literals over `reportCases`/`followCases`' `tc.model`; those closing over the parent's `Options`: `TestSuggestionMenuSkipsAHintTheCatalogNoLongerHolds` (2, assign `opts.Skills` and share `rec`), `TestSuggestBandPrecision`, `TestPaintedTabBearingStartupCardKeepsItsBorder`; and `TestBreadcrumbTrailNamesTheWayBackUp` (one shared `tr := &transcript{}`). The three literals of `TestSmokeLiveProfileSeam` stay serial under their serial parent.
NOTES (2026-09-17): literals closing over only immutable parent values (a const width, a string, a theme, a `[]string` row, a `schedule.Event` by value, a `paintMethods` case) or a builder/helper closure that returns or takes a fresh `*transcript`/Model per call (`build`, `paintedRow`, `assertSlot`, `rename`, `started`, `nilConfig`, `header`, `lockstep`, `resolves`) got `t.Parallel()`, matching items 18–19; `TestFiringBlockHeaderNeverBlinks`'s two cases paint distinct transcripts, and `TestTranscriptCodecClosesEveryInterruptedToolCall`'s literals read the decoded `got` only.

**What:** Depends on item 19; same rule and same serial-exception wording as item 18. Files: every `internal/tui/*_test.go` not named in items 18–19 that still holds a serial test (`grep -L 't.Parallel()' internal/tui/*_test.go` plus the partially-swept files: toolpresent, subagentblock, paint, toolbranch, markdown, activity, confine, toolshape, suggestband, keymigration, autocomplete, runview, toolblock, render, sessionsave, prebound, undo, diagnostics, colorscheme, usage, userblock, thinkingpane, inputaccent, chromelayout, approval, skillscmd, reportpane, prompteditor, paintcache, wrap, blocktarget, mode, contextfiles, mousereassert, spinner, tui, filecache, listsurface, fold, toolleader, logo). `live_test.go`, `smoke_live_test.go` (env-gated live tests) and `conpty_windows_test.go` stay serial with the comment.

**Regression guard.** Item 18's subtest clause carries here through "same rule as item 18", and it names the reason: a Model copied from a parent scope shares its paint cache (one unlocked `*paintCache`; `c.rows[head] = …` in `paintcache.go`, no lock), transcript backing arrays and textarea `value [][]rune`, so a `live := m` / `step(t, base, …)` literal is never parallel — `TestBlockCursorKeysReturnToThePrompt`'s three literals repaint one `base` (concurrent map writes under `-race`), and `TestSuggestionMenuSkipsAHintTheCatalogNoLongerHolds`'s subtests assign the parent's `opts.Skills` and share `rec` through `gatedSuggest(&rec)`; both stay serial under their parents.

**Files:** the test files the grep names.
**Read first:** internal/tui/autocomplete_test.go — TestSuggestionMenuSkipsAHintTheCatalogNoLongerHolds, gatedSuggest; internal/tui/blockcursor_test.go — TestBlockCursorKeysReturnToThePrompt; internal/tui/conpty_windows_test.go — TestConPTYChildProcess (swaps os.Stdout/os.Stdin, the item's serial exception); internal/tui/sink_test.go — TestTeaSinkEmitsEventsInOrder (1 ms coalescing window, already parallel); internal/tui/paintcache.go — paintBlock, store; internal/tui/model.go — refreshViewport

**Tests:** none new.

**Acceptance:** `go test -race -count=3 ./internal/tui/ && go test -race -count=1 -parallel 4 ./internal/tui/`

commit: `test(tui): t.Parallel across the remaining driver tests`

## 21. The tui seam guard, and the shards script and manual say so (apogee-11s)

**What:** Closes apogee-11s. Depends on item 20. Mirror `cmd/apogee/seams_guard_test.go` into `internal/tui/seams_guard_test.go`: `TestNoParallelTestSwapsAPackageSeam` derives the seam set from the package's top-level `var`s (asserting `writeSystemClipboard` is in it) and fails for any parallel test or `t.Run` literal that assigns one, plus the fixture-driven `…Bites` self-proof — copy the AST helpers rather than exporting them (two packages, two guards; the helpers are test code). Then rewrite the two prose sites: `scripts/test-shards.sh`'s header no longer says internal/tui's driver tests are serial (it says both heavy packages are parallel inside and sharding balances ACROSS packages; re-measure `HEAVY_WEIGHT` with `go test -json` timings and note the numbers in the comment), and `docs/manual/building.md`'s paragraph says `internal/tui` is parallel too. Rule for finding every site: `grep -rn 'still serial\|driver tests are serial' scripts docs Makefile`.

**Regression guard.** The mirrored detector walks only `Test*` bodies and `t.Run` literals (`parallelSeamWrites`, `seamWritesInScope`), and internal/tui's one seam is assigned inside the named helper `recordSystemClipboard` (mouse_test.go:284-287), so a `t.Parallel()` on `TestDragCopyAlsoWritesTheSystemClipboard` would pass it: the tui guard resolves calls to package-local `_test.go` functions transitively (a helper whose body assigns a seam makes every parallel caller a finding, reported at the call), the Bites fixture carries a `TestParallelViaHelper` case, and the `writeSystemClipboard` assertion stays. `Makefile` :178-179 ("internal/tui's driver tests are still serial") is a third prose site: it is in Files and the acceptance grep.

**Files:** `internal/tui/seams_guard_test.go`, `scripts/test-shards.sh`, `docs/manual/building.md`, `Makefile`.
**Read first:** cmd/apogee/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam, packageSeams, parallelSeamWrites, seamWritesInScope, callsParallel; internal/tui/mouse_test.go — recordSystemClipboard; scripts/test-shards.sh — HEAVY_WEIGHT; Makefile — test target comment

**Tests:** the guard and its Bites proof (with the `TestParallelViaHelper` fixture case).

**Acceptance:** `go test -race ./internal/tui/ -run 'TestNoParallelTestSwapsAPackageSeam' && ! grep -rn 'still serial' scripts docs/manual Makefile && bash -n scripts/test-shards.sh`

commit: `test(tui): a guard pins that no parallel test swaps a package seam; shards and manual updated`
