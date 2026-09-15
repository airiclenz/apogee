# Host, config and tools deepening (2026-09-14 architecture review, plan B) — plan

**Goal:** land the host-, config- and tool-side candidates of
`docs/reviews/architecture-review-2026-09-14.html` that survived fact-checking, each reduced to
what the code and the ADRs support: one fenced runner for the user's own argv (13r); the
snapshot-sweep bug and the small in-passing defects; facade forwarders so a bench Driver gets the
product's prompt and shape (1r); `Options.StartupEntry` instead of fourteen flattened fields (7r);
one `raise` for every unattended Firing (9); an agent-side Floor-guard table (11r); tool
registration derived from one list (15r); one argument-key fold (16r); the subprocess mirror
deleted (17r). Sibling plan A (`2026-09-15 - 00`) holds the engine half.

**Date:** 2026-09-15
**Status:** unexecuted
**sized for:** ~200k-context host

**Regression check (2026-09-15, b2e3d75d):**
- 1: guard folded — `Result` carries facts (each caller keeps its own wording, `StdoutTruncated` + `maxKeyCommandOutput` stay); prose rule for the copies' cross-references.
- 2: guard folded — sweep sites named (daemon once in `newDaemonWiring`, headless in `runHeadlessBody` after `gcSessions`).
- 3: guard folded — (b) `interruptedSummary` + test + three doc links go; (c) yields to `cmd/apogee/delegation.go`'s `base` comment (kept for stage 2, bead apogee-089); (d) grep phrase corrected to "Firing copies".
- 6: recast — `StartupEntry` stored after the alias fallback with `Name = opts.HostAlias`; `StartupEphemeral` stays; eight-field deletion list; Files = every site the acceptance grep finds.
- 7: recast — the held entry keeps `firingSources`' one overlay (current binding, pin 0, key sources empty) and is refreshed in `followEntry` AND `setServers`.
- 8: recast — `raise` mints the id but calls `onID` before composition/gate; `narrate(recordID, cfg, sink)`; notices returned on refusal; `ref *reactions.ScheduleRef`, no beat parameter; sweep stays before `raise`; drain precedes the summary.
- 9: guard folded — same error/notice contract as 8; only Driver-identical `runOnce` swaps go; Outcome gains `ContextAnomalies`.
- 10: recast — gate on the footer's debounced offline verdict latched host-side (never a probe per Firing); no observation ⇒ proceed; `runOnce`-count acceptance dropped.
- 11: guard folded — one side of the key list exported, set-equality test homed in `cmd/apogee`; `setFloorGuard` keeps its key→field mapping (yields to `floorFromOptions`' "ONE negation seam"); rows carry `gate`/`handler` funcs; `floorguards_test.go` unchanged.
- 12: guard folded — `builtinTools` builds the three delegate tools unconditionally; `HostToolsOf(cfg, seatChoice bool)`; `construct_test.go`/`wire_tools_test.go` recast; acceptance grep excludes `HostTools{}`.
- 13: guard folded — `TestClassifyTool` and its four fakes move; doc locators (`confinement-execution-contract.md`, ADR 0012) get dated pointers.
- 15: guard folded — the three seam-swapping test files follow the new signature; `subprocess.SubprocessResult` spelled; `exec_common.go`'s keep-the-mirror comment superseded by header verdict 17.
- 16: guard folded — the five re-exports named; `lookGit`/`runGit`/`runGitUnchecked`/`RunGitQuery` stay regardless of size after 15; `git_stage*`/`git_windows_test.go` in scope; every inlined `gitexec.Program/Resolve` passes `lookGit`; identifier prose grep.
- 4, 5, 14: SAFE.
- 7 (round 2): guard folded — the held entry's overlay also ZEROES `Description` and `EffortDialect` (today's `firingSources` never builds them), so an in-session Firing's Delegations line and dialect stay as they are; the Driver-parity alternative is named, not taken.
- 8 (round 2): guard folded — `errNotStarted` is `struct{ Stage string; Err error }` (Stage "compose" | "offline", `Error()` = `Err.Error()` verbatim, `Unwrap()`), items 9/10 name it and execute after 8; headless installs `signal.NotifyContext` BEFORE `raise` (one ctx) and owns the offline-worded exit-2 refusal on a Ctrl-C during the beat.
- 9 (round 2): guard folded — names item 8's `errNotStarted` shape: a "compose" refusal is wrapped under the daemon's schedule-named log line, an "offline" one passes bare, the sentence intact either way.
- 10 (round 2): guard folded — the latch is fed by a TUI publish `tui.Options.ReportUpstream(offline bool, failure string)` at `foldBeatFailure`'s crossing, `foldBeat`'s back-online crossing and `foldServerSwitch`'s reset ("extend `liveSettings.observe`" struck — its only caller `rootWiring.rebind` runs after an answered beat); doc edit → `commands.md`'s `/schedule` row mirroring `headless.md`'s offline sentence (`sessions.md` has no Schedule section); the test asserts `err.Error()` from `fire` equals `notice.ServerOffline(endpoint, failure)`.

## Authoritative sources

- `docs/reviews/architecture-review-2026-09-14.html` — fact-checked 2026-09-15; **where an item disagrees with the review, the item wins**.
- ADR 0010 (module order), 0021, 0023 D2, 0031, 0033 §6 (caller composes; `internal/run` runner-agnostic), 0036, 0044 D8 (resolution stays in the composition root), 0046, 0071 D5, 0074, 0076 D11/A1, `docs/design/confinement-execution-contract.md` §3.5.
- `docs/plans/archived/2026-08-24 - 03 - architecture-review-deepening-plan.md` items 6–9 (`firingConfig`, the composer `raise` builds on) and its Out-of-scope line: no Firing composition moves into `internal/run`.

## Review verdicts (fact-check, 2026-09-15; HEAD b2e3d75d)

| # | Candidate | Verdict |
|---|---|---|
| 1 | Per-model bindings leave main | **IN, reduced** — facade forwarders + bench-readiness test; ADR 0044 D8 keeps resolution in `cmd/apogee` (review missed it) |
| 7 | Bound Upstream one value | **IN, reduced** — `Options.StartupEntry`; `liveSettings` untouched |
| 8 | Effort dialect inside Client | **DENIED** — ADR 0060 D9 ratifies the channel; detection happens in the Monitor's Client |
| 9 | Raising a Firing is one act | **IN** — in `cmd/apogee`, never `internal/run` |
| 10 | Boxed pane adapter | **DEFERRED** — 8-day-old owner-designed handlers (plan `2026-09-06 - 00`); bead filed |
| 11 | Floor guard one table | **IN, reduced** — agent-side table; the seven `Disable…` bools stay (ADR 0071 D5, 0076 D11) |
| 12 | Session-record fold | **DENIED** — the `contentArgs` divergence is documented design |
| 13 | One user-argv runner | **IN, reduced** — reactions + keyresolve; keystore is a different posture |
| 15 | Tool registration | **IN, reduced** — derived names, one composer, `Classify` beside the markers; argument-role → bead |
| 16 | Canonicalise once | **IN, reduced** — one fold; canonical bytes would rewrite what the model replays |
| 17 | Spawn core direct | **IN, reduced** — mirror deletion; `execHost` injection → bead `apogee-11s` |
| 18 | Host boot one module | **DENIED** — ~6 truly shared steps, sentences differ in kind; the `gcSnapshotDirs` bug is IN |

## Ratified design calls (owner, 2026-09-15)

- **Scope:** the table above; denied rows recorded here only.
- **Gate:** items 1–3 touch no file plans 02/03/04 rewrite and run first; item 4 gates the rest on plan `2026-09-14 - 04` archiving; items cite symbols, never lines.
- **Offline gate (9):** `raise` owns the liveness gate and the TUI gains it — a `/schedule` Firing raised while the last Beat says offline refuses up front with `notice.ServerOffline`.
- **Bench adoption (1):** the forwarders land here; apogee-sim dialling through them is that repo's work (its baselines will shift).
- **ADR text:** dated in-place amendments.

## Standing requirements

- `skills: coding-standards`.
- Deviations land as a dated `NOTES:` line under the item.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

## Out of scope

- Candidates 8, 10, 12, 18 (denied/deferred); `hostBoot`; the key-resolver fence root (`""` in the daemon and probe is deliberate — a bead records the narrow `api-key-cmd`-in-Auto gap); moving resolution out of `cmd/apogee`; `keystore/run.go`; per-tool argument-role declarations; `execHost` injection; `liveSettings.entry*`.
- Plan A's items.

---

## 1. `internal/userexec`: one fenced, bounded runner for the user's own argv — ✅ DONE (2026-09-15)

NOTES (2026-09-15): `Options` gained a `WorkspaceRoot` field beyond the plan's five — the fence root has to reach `security.ResolveProgram` somehow and both callers hold one; the plan's `Run(ctx, argv, Options{…})` signature is otherwise as written. The exported bounds are spelled `WaitGrace` (the name both copies used), `MaxStderr` and `StderrTailRunes`.
NOTES (2026-09-15): `internal/config/keyresolve_test.go` needed no edit — it reads only `maxKeyCommandOutput`, which stays; its `flood` fixture and every `TestKeyResolver*` case pass unchanged. A failed `api-key-cmd:` now reads `failed: exit status N` composed from `Result.ExitCode` rather than `%w` of the `exec.ExitError` — identical text for an exit status; a signal-ended child (never reached by keyresolve, which runs with no cancellable context) would read `exit status -1` instead of `signal: killed`.
NOTES (2026-09-15): consequential edit — internal/reactions/doc.go: made necessary by deleting command.go's copied exec posture (its package map said "the api-key-cmd exec posture, copied"; it now points at internal/userexec).
NOTES (2026-09-15): the shared fence/cap/timeout table lives in `internal/userexec/userexec_test.go` (exit status + tail, stdout wanted/capped/zero-cap, stdin + env-last, context deadline and `Options.Timeout` each within the WaitGrace bound, SIGPIPE-safe stderr cap, fence refusal + empty-root pass, empty argv / not-on-PATH sentences); the reactions and keyresolve suites stay as caller-wording tests, `TestCommandExecutorReportsTheDeadlineRatherThanWaitingOnASleep` and `…CutsAnOverlongComplaintDownToATail` now read `userexec.WaitGrace` / `userexec.StderrTailRunes`.

**What.** New leaf package `internal/userexec` (imports `internal/security` only) with `Run(ctx, argv, Options{Stdin, WantStdout, StdoutCap, Timeout, Env}) (Result, error)`: `argv[0]` fenced via `security.ResolveProgram`, `WaitDelay` 2s, 4 KiB capped stderr that reports a full write (SIGPIPE-safe), 240-rune head tail, the three-way error sentence. `internal/reactions/command.go` and `internal/config/keyresolve.go` (`runKeyCommand`, `cappedWriter`) become one-line callers; their copies and the header comment declaring unification out of scope are deleted. `internal/keystore/run.go` and `tools.RunHookSubprocess` are NOT touched (different postures).

**Regression guard.** `Result` carries facts, not a sentence — `ExitCode`, `TimedOut`, `StderrTail` (240-rune fold), `Stdout`, `StdoutTruncated bool` — and each caller keeps composing its own wording: reactions `exit N: …` / `timed out after …` (pinned exactly as `exit 3: no such recipient` by `TestCommandExecutorReportsTheExitStatusAndWhatTheCommandSaid`), keyresolve `api-key-cmd: %q failed: … — it said: …` / `did not answer within` / `printed more than` / `printed nothing`; say so in What. `maxKeyCommandOutput` (64 KiB) stays in `keyresolve.go` as the cap keyresolve passes, and it refuses on `StdoutTruncated` (`keyresolve_test.go`'s `flood` fixture keeps reading the constant). Prose rule: every comment naming a copy this item deletes — `grep -rn "keyresolve.go\|keystore/run.go\|command.go" internal/reactions internal/config internal/keystore` — is retargeted to `internal/userexec`; `internal/keystore/run.go` receives comment edits only, its code stays.

**Files:** `internal/userexec/userexec.go`, `internal/userexec/userexec_test.go`, `internal/reactions/command.go`, `internal/reactions/command_test.go`, `internal/config/keyresolve.go`, `internal/config/keyresolve_test.go`, `internal/keystore/run.go` (comments only).
**Read first:** `internal/reactions/command.go` — commandExecutor.Run, resolveProgram, stderrTail, cappedWriter; `internal/config/keyresolve.go` — runKeyCommand, resolveKeyProgram, saidOnStderr, cappedWriter, KeyResolver.Resolve; `internal/security/execsafety.go` — ResolveProgram, ErrExecFromWritablePath; `internal/reactions/command_test.go` — TestCommandExecutorReportsTheExitStatusAndWhatTheCommandSaid, TestCommandExecutorReportsTheDeadlineRatherThanWaitingOnASleep, TestCommandExecutorCutsAnOverlongComplaintDownToATail;
`internal/config/keyresolve_test.go` — TestKeyResolverCommandSource, runKeyFixture, fixtureCommand, TestKeyResolverRefusesACommandProgramInsideTheWorkspace; `internal/keystore/run.go` — waitGrace, maxToolStderr (comments only); `internal/reactions/runner.go` — Runner.runOne

**Tests.** `TestCommandExecutor*` and `TestKeyResolver*Program*` stay green as callers, `TestCommandExecutorReportsTheExitStatusAndWhatTheCommandSaid` and `TestKeyResolverCommandSource`'s "output far past a key's length refuses" case unchanged; the shared fence/cap/timeout table moves to `userexec_test.go`.

**Acceptance.** `go build ./... && go test ./internal/userexec/... ./internal/reactions/... ./internal/config/... -run 'Command|Program|KeyResolver|Run'`; `grep -n "cappedWriter\|WaitDelay" internal/reactions/command.go internal/config/keyresolve.go` prints nothing.

Commit: `refactor(userexec): one fenced bounded runner behind reactions and api-key-cmd`

## 2. Headless and daemon Firings sweep the snapshot dirs — ✅ DONE (2026-09-15)

**What.** Defect: `gcSnapshotDirs` (ADR 0074 sweep) runs only at TUI boot (`cmd/apogee/wire_live.go`); headless and daemon Firings open per-run snapshot stores (`internal/run/run.go`) and never sweep — unbounded growth on a headless/daemon-only host. Call `gcSnapshotDirs(roots.snapshots, sessions, time.Now())` after the session store opens in `cmd/apogee/headless.go` and in the daemon's per-Firing boot in `cmd/apogee/daemonfire.go`.

**Regression guard.** The sites are named, and neither is per Firing: the daemon sweeps ONCE in `newDaemonWiring` right after `gcSessions(store, opts.Sessions)` (its store opens once there, not per Firing — a call in `daemonWiring.fire` would add an `os.ReadDir` plus a full `session.Store.List` decode to every Firing, work no Firing pays today); headless sweeps in `runHeadlessBody` right after `gcSessions(sessions, opts.Sessions)`, passing `sessions` (the always-open store — not the record-writing `store`, which is nil under `--no-save`).

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/daemonfire_test.go`.
**Read first:** `cmd/apogee/wire.go` — gcSnapshotDirs, gcSessions, gcScratchDirs, resolveRoots, stateRoots; `cmd/apogee/headless.go` — runHeadlessBody; `cmd/apogee/daemonfire.go` — newDaemonWiring, daemonWiring.fire; `cmd/apogee/wire_live.go` — rootWiring.wireSession; `cmd/apogee/wire_session_test.go` — snapshotDirAt, saveAt, TestGCSnapshotDirsSweepsWhatNothingCanReach;
`cmd/apogee/headless_test.go` — headlessRunOn, testConfigHome, TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes; `cmd/apogee/daemonfire_test.go` — newDaemonFireHarness, TestDaemonStartupSweepsStaleScratchDirs

**Tests.** A stale snapshot dir seeded under the roots is gone after a headless run (a twin of `TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes`) and after daemon startup — `newDaemonWiring`, a startup twin of `TestDaemonStartupSweepsStaleScratchDirs` (both fail before the item).

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Snapshot|Sweep|Headless|Daemon'`.

Commit: `fix(cmd): headless and daemon Firings sweep stale snapshot dirs`

## 3. Small in-passing defects and doc drift — ✅ DONE (2026-09-15)

NOTES (2026-09-15): (c) is a no-op per the regression guard — `base` is still documented as kept for stage 2 (bead apogee-089); `newDelegationWiring`/`newSubAgentServer` and their callers untouched.
NOTES (2026-09-15): the `wire.go` test-mirror sentence also names `wire_live_test.go` and `wire_verbs_test.go`, which exist and were missing from the list it corrects; the options projection is described as exercised through `wire_boot_test.go`, `wire_settings_test.go` and `settingsrows_test.go` (the files that call `rootWiring.options()`), since no `wire_options_test.go` exists.

**What.** (a) `internal/tui/sink.go` nils `SeamClosedEvent.Value` before forwarding to the Update goroutine (the value is valid only during `Emit`, per `internal/domain/events.go`). (b) `internal/session/transcript.go`: delete the exported `CloseInterruptedCalls` (zero callers; the TUI keeps its own over TUI cards). (c) `cmd/apogee/delegation.go`: stop threading the base `apogee.Config` through its five sites when nothing reads it — verify by grep at implement time; keep any site that reads a field. (d) Docs: `cmd/apogee/wire.go` names a test file that does not exist; `cmd/apogee/wire_boot.go`'s comment claims a Firing copies this Config (stale since `firingConfig`). Rule: every comment naming `wire_options_test.go` or "copies this Config" — `grep -rn "wire_options_test\|copies this Config" cmd/apogee`.

**Regression guard.** (b) `interruptedSummary` (`internal/session/transcript.go`) and `TestCloseInterruptedCallsClosesWhatTheRecordCaughtOpen` go with the function (the `unused` linter in `.golangci.yml`'s standard set would otherwise fail `make check`), and the three prose sites linking `[session.CloseInterruptedCalls]` — `internal/run/transcript.go`, `internal/tui/transcriptbridge.go`, `internal/session/doc.go` — are rewritten; rule: `grep -rn "CloseInterruptedCalls" --include=*.go .` prints nothing but the TUI's lowercase `closeInterruptedCalls`. (c) yields to the documented decision on `cmd/apogee/delegation.go`'s `base` field ("Nothing reads it today … stage 2's per-seat `reactions:` resolver is what needs it back", bead apogee-089): `base` and the `newDelegationWiring`/`newSubAgentServer` signatures stay, so (c) is a no-op and their callers (`wire_live.go`, `wire_firing.go`, `delegation_test.go`, `wire_settings_test.go`, `naming_test.go`) are not touched. (d) the rule's phrase matches nothing — `wire_boot.go` spells it "this Config is what a scheduled Firing copies", `schedule_test.go` "Config a Firing copies"; rule: every comment claiming a Firing copies the session Config — `grep -rn "Firing copies" cmd/apogee` — is reworded to what `firingConfig` does (builds its own Config from `firingInputs`, `wire_firing.go`).

**Files:** `internal/tui/sink.go`, `internal/tui/sink_test.go`, `internal/session/transcript.go`, `internal/session/transcript_test.go`, `internal/session/doc.go`, `internal/run/transcript.go`, `internal/tui/transcriptbridge.go`, `cmd/apogee/wire.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/schedule_test.go`.
**Read first:** `cmd/apogee/delegation.go` — delegationWiring.base, newDelegationWiring, newSubAgentServer; `internal/tui/sink.go` — teaSink.Emit, teaSink.flushLocked; `internal/tui/sink_test.go` — TestTeaSinkEmitsEventsInOrder; `internal/domain/events.go` — SeamClosedEvent; `internal/reactions/runner.go` — Runner.Emit (wraps the sink, matches on its own copy);
`internal/session/transcript.go` — CloseInterruptedCalls, interruptedSummary; `internal/tui/transcriptbridge.go` — closeInterruptedCalls; `cmd/apogee/wire_boot.go` — rootWiring.wireBoot Context/Floor comments, `cmd/apogee/wire.go` — file-split header comment

**Tests.** `TestSinkForwardsSeamClosedWithoutTheLiveValue`; `TestCloseInterruptedCallsClosesWhatTheRecordCaughtOpen` is deleted with the function; existing delegation tests stay green untouched.

**Acceptance.** `go build ./... && go test ./internal/tui/ ./internal/session/... ./cmd/apogee/ -run 'Sink|Seam|Interrupted|Delegation'`; `grep -rn "wire_options_test\|Firing copies" cmd/apogee` prints nothing; `grep -rn "CloseInterruptedCalls" --include=*.go .` prints nothing but the TUI's lowercase `closeInterruptedCalls`.

Commit: `fix(tui,session,cmd): seam value not forwarded live; dead export and stale comments removed`

## 4. Gate: plan 2026-09-14 - 04 is archived — ✅ DONE (2026-09-15)

NOTES (2026-09-15): gate PASSED — `ls "docs/plans/archived/" | grep -c "2026-09-14 - 0[234]"` = 3; `ls docs/plans/ | grep -c "2026-09-14 - 0[234]"` = 0; archived plan 04 is tracked (commit 4de244b4) with all 7 items ✅ done. Working tree clean.
NOTES (2026-09-15): observation only — the archived 02/03/04 plan files still carry `**Status:** unexecuted` at line 6 despite every item being ✅ done and the archive commits landing; header not refreshed by the archive step. Not edited (item says no change).

**What.** Verify `docs/plans/archived/2026-09-14 - 04 - queued-commands-delegation-width-and-accounting-plan.md` exists and `docs/plans/` holds no `2026-09-14 - 02/03/04` file. If not, STOP the run here (resume later). No code change.

**Files:** none.
**Read first:** `docs/plans/archived` — plan files 2026-09-14 - 01..04; `docs/plans` — the unarchived 02/03/04 drafts (git status shows 03 and 04 untracked and 02 on the run branch)

**Tests.** none.

**Acceptance.** `ls "docs/plans/archived/" | grep -c "2026-09-14 - 0[234]"` prints `3`; `ls docs/plans/ | grep -c "2026-09-14 - 0[234]"` prints `0`.

Commit: none (gate item).

## 5. The facade exports the product's prompt and shape; the bench can dial through them — ✅ DONE (2026-09-15)

NOTES (2026-09-15): consequential edit — cmd/apogee/doc.go: made necessary by the new `resolveModelBindings` in modelprofile.go (the package map's half-line role for that file now names it).
NOTES (2026-09-15): folding startup onto the rebind's resolution means a pre-bound start whose shipped/user profile carries roster deltas now prints the `tools: … (profile)` line to stderr beside the built-in-match line (the rebind already emitted both on the notice channel); a cold start names no model and prints nothing, as before.

**What.** `apogee.go` gains two forwarders — `DefaultSystemPrompt() string` (→ `config.DefaultSystemPrompt`) and `ShippedProfile(model string) (ModelProfile, bool)` (→ `profiles.Resolve` over `profiles.Shipped()`); resolution itself stays in `cmd/apogee` (ADR 0044 D8, 0023 D2). `cmd/apogee/wire_boot.go`'s inline prompt+profile resolution calls the same function `rebindSpecFor` uses (`cmd/apogee/wire_settings.go`), so the binary spells it once. `example_test.go`'s facade-completeness guard lists both; `benchreadiness_test.go` asserts a bench-shaped `Config` built through them carries a non-empty prompt and a shipped shape. ADR 0044 gets a dated amendment noting the facade reachability. Depends on item 4.

**Files:** `apogee.go`, `example_test.go`, `benchreadiness_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/modelprofile.go`, `docs/adr/0044-*.md`.
**Read first:** `apogee.go` — ModelProfile alias, forwarding constructors block; `example_test.go` — the "Forwarding constructors" var block; `internal/config/defaults.go` — DefaultSystemPrompt; `internal/profiles/match.go` — Resolve, Decision; `internal/profiles/shipped.go` — Shipped; `cmd/apogee/modelprofile.go` — resolveModelProfile, modelProfileNotice;
`cmd/apogee/wire_boot.go` — rootWiring.resolveConfig; `cmd/apogee/wire_settings.go` — rebindSpecFor

**Tests.** `TestBenchConfigDialsTheProductsPromptAndShape` (fails before the item — the facade has no resolver); `modelprofile_test.go`, `wire_settings_test.go` stay green.

**Acceptance.** `go build ./... && go test . ./cmd/apogee/ -run 'Facade|Bench|ModelProfile|RebindSpec|Example'`.

Commit: `feat(facade): export the default prompt and shipped profile so any Driver dials the product's agent`

## 6. `Options.StartupEntry` replaces the fourteen flattened fields — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the plan's Files list names `cmd/apogee/configmigrate_test.go`, `wire_firing.go`, `upstream.go` and `delegation.go`; the first does not exist in the tree and the other three contain no reader the acceptance grep finds, so none was edited.
NOTES (2026-09-15): consequential edit — 14 further `_test.go` files under `cmd/apogee/` (wire_boot, wire_live, wire_server, wire_settings, root, title, naming, readfence, settingsedit, keymigrate, configwatch_apply, confinement_e2e, daemonfire, schedule): hand-built `config.Options` literals that reached the bind through `runRoot`/`wireSession`/the daemon harness relied on the deleted re-assembler to build the entry from `Endpoint`/`Model`/`HostAlias`, so each now also sets `StartupEntry` to the same values (as `ApplyConfig` would hold it); no assertion changed.
NOTES (2026-09-15): consequential edit — docs/adr/0028-…md: the follow-up paragraph naming `StartupContextWindow` gained a dated parenthetical pointing at `config.Options.StartupEntry`.

**What.** Recast at the regression check (2026-09-15). `config.Options` (`internal/config/options.go`) gains `StartupEntry ServerEntry`; `ApplyConfig` (`internal/config/config.go`) stores the resolved entry once and the fourteen `Startup*` fields plus `StartupEphemeral` are deleted. Producers: `ApplyConfig`. Consumers enumerated at write time and re-pointed: `cmd/apogee/wire_server.go` (`startupEntry` deleted — `bind` reads `opts.StartupEntry`), `cmd/apogee/wire_settings.go` (`rebindInputs`, `liveSettings` seeding), `cmd/apogee/wire_firing.go`, `cmd/apogee/upstream.go`, `cmd/apogee/delegation.go`, plus every `_test.go` that sets a `Startup*` field (`grep -rln "Startup[A-Z]" --include=*_test.go .`). The three `Resolve{ContextWindow,WorkingWindow,ResponseReserve}` ladders are NOT changed. Depends on item 4.

**Regression guard.** Options.StartupEntry is stored AFTER ApplyConfig's alias fallback with Name = opts.HostAlias — exactly what startupEntry builds today — so cfg.ServerName, the orientation seat line, headless run_started server and the Firing seats keep their names; StartupEphemeral STAYS (derived from startup.Name == "" before the alias is written) so upstreamChoices can still read ephemeral. The exact deletion list is StartupLauncher, StartupParallelAgents, StartupMaxOutputTokens, StartupContextWindow, StartupWorkingWindow, StartupResponseReserve, StartupEffortDialect, StartupDescription (eight fields) plus the startupEntry re-assembler; Endpoint, Model, APIKey, APIKeyCmd, APIKeyEnv, HostAlias, StartupServer are flag-bound and STAY. Files line becomes the rule "every site this grep finds" — `grep -rnE "Startup(Launcher|ParallelAgents|MaxOutputTokens|ContextWindow|WorkingWindow|ResponseReserve|EffortDialect|Description)\b|startupEntry\(" --include=*.go .` — with the known readers named: cmd/apogee/wire_boot.go, wire_live.go, daemon.go, daemonfire.go, headless.go, probe.go, probemodel.go, wire_server.go, wire_settings.go, wire_firing.go, upstream.go, delegation.go and the tests upstream_test.go (TestStartupEntryCarriesTheParallelAgentsPin rewritten against ApplyConfig's held entry), keysource_test.go, daemon_test.go, schedule_test.go, wire_settings_test.go, configmigrate_test.go, wire_server_test.go; that grep printing nothing is the Acceptance (replace the Startup[A-Z] grep, which can never be empty).

**Files:** every site the guard's grep finds — `internal/config/options.go`, `internal/config/config.go`, `internal/config/config_test.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/daemon.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/headless.go`, `cmd/apogee/probe.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/wire_server.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/upstream.go`, `cmd/apogee/delegation.go`, `cmd/apogee/upstream_test.go`, `cmd/apogee/keysource_test.go`, `cmd/apogee/daemon_test.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/configmigrate_test.go`, `cmd/apogee/wire_server_test.go`.
**Read first:** internal/config/config.go — ApplyConfig, resolveStartupEntry, aliasFromEndpoint; internal/config/options.go — Options; cmd/apogee/wire_server.go — startupEntry, serverBinder.bind; cmd/apogee/upstream.go — upstreamChoices; cmd/apogee/wire_settings.go — newLiveSettings

**Tests.** `TestRegistryIsBijectionWithFileConfig`, `TestApplyConfigStartupOverrides` ("rented.example" alias) and `TestApplyConfigStartupLauncherComesFromTheSelectedEntry` stay green; `TestApplyConfigHoldsTheStartupEntry` new; `TestStartupEntryCarriesTheParallelAgentsPin` is rewritten against `ApplyConfig`'s held entry; `keysource_test.go`'s direct `startupEntry` calls read `opts.StartupEntry`; `wire_server_test.go` binds from the entry.

**Acceptance.** `go build ./... && go test ./internal/config/... ./cmd/apogee/ -run 'Startup|Bind|Registry|ApplyConfig|Rebind'`; `grep -rnE "Startup(Launcher|ParallelAgents|MaxOutputTokens|ContextWindow|WorkingWindow|ResponseReserve|EffortDialect|Description)\b|startupEntry\(" --include=*.go .` prints nothing.

Commit: `refactor(config): Options holds the startup ServerEntry once instead of fourteen flattened fields`

## 7. `firingSources` is deleted; `firingConfig` reads the held entry — ✅ DONE (2026-09-15)

NOTES (2026-09-15): the replacement accessor is named `firingBinding` (the plan names none); `TestFiringSourcesCarriesTheLiveSubAgentsServer` → `TestFiringBindingCarriesTheLiveSubAgentsServer`, and the new test is `TestFiringBindingHandsOverTheHeldEntry` in `wire_settings_test.go`.
NOTES (2026-09-15): `cmd/apogee/wire_firing.go` and `cmd/apogee/wire_firing_test.go` are listed in Files but needed no edit — neither names `firingSources`, and `firingConfig`'s reads of the entry are untouched.
NOTES (2026-09-15): consequential edit — cmd/apogee/schedule.go: made necessary by the rename (call site and two comments naming `firingSources`).
NOTES (2026-09-15): consequential edit — cmd/apogee/schedule_test.go: made necessary by the rename (two comments naming `firingSources`).
NOTES (2026-09-15): consequential edit — docs/adr/0037-every-settings-edit-applies-to-the-running-session.md: made necessary by the rename (the sentence naming `firingSources` gained a dated pointer; its already-stale "and the validated `mechanisms:` ids" clause — the accessor has returned two values since the retirement wave — was dropped in the same sentence rather than rewritten as true).

**What.** Recast at the regression check (2026-09-15). `cmd/apogee/wire_settings.go`'s `firingSources` re-assembles a `ServerEntry` from `liveSettings.entry*`; after item 6 `liveSettings` holds the entry it was seeded with (updated on rebind) and `firingConfig` (`cmd/apogee/wire_firing.go`) takes it directly. Delete the re-assembler. `liveSettings.entry*` fields stay (they are the live-edited overrides). Depends on item 6.

**Regression guard.** the held entry keeps the ONE overlay firingSources applies today — entry.Endpoint, entry.Model = the holder's CURRENT binding (bound.Endpoint, bound.Model, so a /model pick is honoured), ParallelAgents 0, key-source fields empty — and only the pin re-assembly is deleted; the held entry is refreshed in BOTH followEntry (/server move) and setServers (a servers: edit) under the same lock ("updated on rebind" is wrong — there is no /rebind command). Stay-green set: TestScheduleFiringRunsAgainstTheCurrentBinding, TestScheduleFiringIsBoundedByTheEntryTheSessionMovedOnto, TestScheduleFiringFollowsLiveSettingsEdits; TestFiringSourcesCarriesTheLiveSubAgentsServer is re-pointed at the replacement accessor (the name TestScheduleFiringComposesFromLiveOptions* does not exist — remove it). Round 2: the overlay ALSO zeroes `Description` and `EffortDialect` — today's `firingSources` never builds either, while `firingConfig` reads both (`provider.EffortDialectFor(in.entry.EffortDialect)`, `ServerDescription`) and `describeDelegationSeat` (internal/agent/orientation.go) would print the description on the orientation Delegations line — so an in-session Firing keeps today's prompt bytes and the session's observed dialect keeps outranking the entry's forced one; the Driver-parity alternative (name the seat description, honour the forced dialect as headless/daemon do — ADR 0031) is NOT taken here and the executing agent does not "fix" it either way.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/schedule_test.go`.
**Read first:** cmd/apogee/wire_settings.go — liveSettings, firingSources, followEntry, setServers; cmd/apogee/wire_firing.go — firingInputs, firingConfig; cmd/apogee/schedule.go — scheduleWiring.fire; internal/agent/orientation.go — describeDelegationSeat

**Tests.** `TestScheduleFiringRunsAgainstTheCurrentBinding`, `TestScheduleFiringIsBoundedByTheEntryTheSessionMovedOnto` and `TestScheduleFiringFollowsLiveSettingsEdits` stay green; `TestFiringSourcesCarriesTheLiveSubAgentsServer` is re-pointed at the replacement accessor; one new test asserts the entry handed to `firingConfig` is the bound one after a `/server` move (`followEntry`) and carries the file's pins after a `servers:` edit (`setServers`), and in both cases carries an empty `Description` and `EffortDialect`.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Firing|Rebind|LiveSettings'`; `grep -n "func firingSources" cmd/apogee/*.go` prints nothing.

Commit: `refactor(cmd): firingConfig reads the bound entry; delete firingSources`

## 8. `raise` — one act for every unattended Firing; headless ports first — ✅ DONE (2026-09-15)

NOTES (2026-09-15): `Outcome()` "derived by one method" is not added here — only the daemon builds a `schedule.Outcome`, and a helper with no caller until item 9 ports `daemonWiring.fire` would be dead code; item 9 (which the header's guard says gains `ContextAnomalies` on Outcome) owns that fold.
NOTES (2026-09-15): the sweeps (`gcSessions`, `gcSnapshotDirs`) stay Driver steps BEFORE `raise` as the guard says, which means a composition/offline refusal now sweeps the stores where today the gate refused first — the guard's parenthetical ("a refused run still sweeps nothing, as today") has it inverted; no test asserts either way.
NOTES (2026-09-15): `raise` always builds the Reaction Runner (an empty observe list included), as headless and the daemon both do today; `narrate` is therefore handed the Runner, never nil — `TestRaiseDecoratesTheSinkAfterTheGate` pins it. Two tests beyond the three the plan names: `TestRaiseCallsOnIDBeforeRefusingAndLatchesTheSchedule` and `TestRaiseDecoratesTheSinkAfterTheGate`.
NOTES (2026-09-15): consequential edit — cmd/apogee/doc.go: made necessary by adding `raise` to wire_firing.go (the package map's half-line role for that file).
NOTES (2026-09-15): consequential edit — docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md: made necessary by moving the `firingConfig` failure and heartbeat-gate refusals it names into `raise` (dated in-place amendment, per the ratified ADR-text call).
NOTES (2026-09-15): docs/manual/headless.md gains one sentence on the owned Ctrl-C-during-the-beat consequence (user-facing behaviour change).

**What.** Recast at the regression check (2026-09-15). Add `raise(ctx, in firingInputs, prompt string, ref scheduleRef, store *session.Store, narrate func(domain.EventSink) domain.EventSink, beat heartbeat.Beat) (run.Result, []string, error)` in `cmd/apogee/wire_firing.go` owning, in order: `SplitLanes`, `firingHooks` + deferred `Close`, `session.NewID` (the same id seeds `RecordID` and the scratch dir — enforced by construction), `firingConfig`, route notices, the liveness gate (refuses with `notice.ServerOffline` when `beat` says offline), `cfg.Events` decoration via `narrate`, `runOnce(run.Spec)`, and `Outcome()` derived by one method. A not-started refusal is a typed `errNotStarted` so headless keeps exit 2 vs 1. `runOnce` stays the one injectable package var. Port `cmd/apogee/headless.go` onto it in this item (first caller proves the seam); its `RunStarted` emission moves into `narrate`. Depends on item 7.

**Regression guard.** raise still mints the id (the by-construction guarantee is the point) but calls an `onID func(recordID string)` hook immediately after minting, before composition and the gate, so headless stamps `lines.SetSession(recordID)` and a composition/offline refusal's closing frame carries the session (ADR 0075 d5; TestHeadlessFormatJSONFramesEveryExit stays green). narrate signature is `func(recordID string, cfg apogee.Config, sink domain.EventSink) domain.EventSink` so RunStarted gets Session, Model=cfg.Model, Server=entry.Name. raise returns the []string notices on the refusal path too and the Driver prints them BEFORE reading the error; errNotStarted.Error() returns the wrapped sentence verbatim (the type's shape is the round-2 decision below) so TestHeadlessRefusesAServerThatAnsweredNothing and TestDaemonFireRefusesOnlyAServerThatAnsweredNothing keep comparing exact text. Signature: `ref *reactions.ScheduleRef` (nil for headless); NO beat parameter — the gate reads routing.Beat.Answered off firingInputs as both Drivers do today. gcSessions stays a Driver step BEFORE raise (accepted: a refused run still sweeps nothing, as today). The Reaction drain (deferred Close, hookCloseGrace) now precedes the summary print — owned, stated in What. Round 2 (decision): `errNotStarted` is `struct{ Stage string; Err error }` with `Stage` one of "compose" | "offline", `Error()` returning `Err.Error()` verbatim (no prefix) and `Unwrap()` returning `Err`; headless maps any `errNotStarted` to exit 2, the daemon wraps it under its own log line while the sentence stays intact, and item 10's refusal prints exactly `Error()` — items 9 and 10 name this shape and execute after 8. Round 2 (ctx): headless installs `signal.NotifyContext` BEFORE calling `raise` — one ctx for composition and run (today `firingConfig` composes under `cmd.Context()`, which is `context.Background` from main.go, and the interrupt-aware ctx is installed only after the gate) — and OWNS the consequence: a Ctrl-C during the beat now refuses with the offline-worded sentence ("… context canceled"), exit 2 with a closing frame, where today the process dies by signal; the implementer does not keep two contexts to preserve the die-by-signal path.

**Files:** `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`.
**Read first:** cmd/apogee/headless.go — runHeadlessBody, notStarted, exitCodeFor, runOnce; cmd/apogee/wire_firing.go — firingInputs, firingConfig; cmd/apogee/daemonfire.go — daemonWiring.fire; cmd/apogee/headless_test.go — TestHeadlessExitCodes

**Tests.** `TestRaiseRefusesWhenOffline`, `TestRaiseMintsOneIDForRecordAndScratch` (the `onID` hook sees the id that names the record and the scratch dir), `TestRaiseNotStartedIsTyped` (a composition refusal carries `Stage` "compose", the gate's `Stage` "offline", `Error()` is the wrapped sentence verbatim and `errors.Unwrap` yields it); `TestHeadlessInterruptDuringTheBeatExits2` (the ctx is cancelled while `discoverBeat` runs: exit 2, the offline-worded refusal, a closing frame under `--format json`); `TestHeadlessFormatJSONFramesEveryExit` (composition and offline refusals carry the minted session id) and `TestHeadlessRefusesAServerThatAnsweredNothing` (exact `notice.ServerOffline` text, notices printed first) and the other headless exit-code tests stay green.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Raise|Headless|Firing|Offline'`.

Commit: `refactor(cmd): raise composes and runs an unattended Firing; headless ports onto it`

## 9. The daemon Firing raises through `raise`

**What.** `cmd/apogee/daemonfire.go` replaces its hand-written sequence with `raise`; its `schedule.Outcome` literal goes (the daemon keeps logging `writtenFilesLines`/`undoVerbLine` from the returned Result). Behaviour preserved field for field. Depends on item 8.

**Regression guard.** same error/notice contract as 8. Delete only the `runOnce` swaps asserting fields `raise` sets identically for every Driver (`RecordID` = scratch id, `Sync`, `DelegationTarget`/`Seat`); keep every daemon-input assertion (the `entry.Run.Model` overlay, the entry's own workspace roots, `w.keys`), which `raise`'s tests cannot see; `TestDaemonFireReportsWhatTheRunDid`'s `DeepEqual` Outcome now gains `ContextAnomalies` (nil for an empty report, so green) — a field the daemon log never renders, noted so "field for field" is honest. Round 2: the daemon reads item 8's `errNotStarted{Stage, Err}` — a `Stage` "compose" refusal is wrapped under the daemon's own schedule-named log line (today's "resolve the %q schedule's reactions/bindings: %w" wording, `%w` keeping `Err` reachable), a `Stage` "offline" one is passed bare, and `Error()` is the sentence verbatim in both — so the daemon's log keeps its three lines apart from one typed error; executes after 8.

**Files:** `cmd/apogee/daemonfire.go`, `cmd/apogee/daemonfire_test.go`.
**Read first:** `cmd/apogee/daemonfire.go` — daemonWiring.fire, serverFor, latchWindowUnknown; `cmd/apogee/daemon.go` — daemonOutcome, LookupServer; `cmd/apogee/daemonfire_test.go` — newDaemonFireHarness, daemonFireHarness.raise, TestDaemonFireRefusesOnlyAServerThatAnsweredNothing, TestDaemonFireReportsWhatTheRunDid; `internal/schedule/schedule.go` — Outcome

**Tests.** Existing daemon Firing tests stay green (`TestDaemonFireRefusesOnlyAServerThatAnsweredNothing` compares the exact `notice.ServerOffline` text; `TestDaemonFireReportsWhatTheRunDid` gains `ContextAnomalies` in its wanted Outcome); only the `runOnce` swaps that asserted Driver-identical fields are deleted where `raise`'s own tests cover the field — every daemon-input assertion stays.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Daemon|Raise'`.

Commit: `refactor(cmd): the daemon Firing raises through raise`

## 10. The in-session `/schedule` Firing raises through `raise` and gains the offline gate

**What.** Recast at the regression check (2026-09-15). `cmd/apogee/schedule.go` replaces its sequence with `raise`, passing the session's live `Options`, live `*skills.Provider`, `w.width()` and the Monitor's last Beat. New user-visible behaviour (owner call): a Firing raised while the server is offline refuses up front with `notice.ServerOffline` — the same sentence headless prints — instead of failing at first send; `docs/manual/commands.md` (the `/schedule` row, or a paragraph beside it) states it. Depends on item 8.

**Regression guard.** the TUI's liveness verdict is the footer's OWN debounced one (offlineFailureThreshold consecutive failed IDLE beats; a failed beat during a busy Exchange is ignored), latched host-side — Round 2: the latch is FED BY THE TUI, the way `ReportActivity` feeds the idle gate: `tui.Options.ReportUpstream(offline bool, failure string)` (wired in wire_options.go beside `ReportActivity`) is called at `foldBeatFailure`'s offline crossing, `foldBeat`'s back-online crossing and `foldServerSwitch`'s reset, and the host latches THAT pair for the schedule closure to read and hand to raise; "extend `liveSettings.observe`" is struck (its only production caller is `rootWiring.rebind`, which runs after a beat that ANSWERED and never sees a failure) and no host-side re-derivation from raw Beats in `rootWiring.beat` is written — it would miss the TUI's fold rules (busy Exchange and actuation-in-flight failures ignored; cold start = one failure; `foldServerSwitch` resets to cold and retires in-flight beats by generation) and latch offline through a `/load` restart or a retired server's late beat while the footer says online; no observation yet ⇒ proceed (every existing TestScheduleFiring* builds scheduleWiring with no beat source and must stay green); NEVER a Monitor probe per Firing (TestScheduleFiringTakesNoBeatOfItsOwn stays). Keep w.width() and observedDialect() on the closure. Tests: cite TestScheduleFiring* (schedule_test.go) as the stay-green set, remove the non-existent TestScheduleFiringComposesFromLiveOptions* name, and drop the "runOnce = count is lower" acceptance — name instead the schedule_test.go swaps that go (those asserting only the Spec each Driver built, per item 9's rule). Round 2: the refusal prints exactly item 8's `errNotStarted.Error()` (`Stage` "offline"); the doc edit lands in `docs/manual/commands.md`'s `/schedule` row (or a paragraph beside it) and mirrors `docs/manual/headless.md`'s offline sentence — `sessions.md` has no Schedule section, its one `/schedule` mention is the browser-tag bullet; the test asserts the error `fire` RETURNS (`err.Error()` equals `notice.ServerOffline(endpoint, failure)`, as headless's test does) — the transcript rendering is the scheduler's, not this item's.

**Files:** `cmd/apogee/schedule.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/wire_options.go` (the `ReportUpstream` wiring beside `ReportActivity`), `internal/tui/tui.go` (`Options.ReportUpstream`), `internal/tui/heartbeat.go` (the three call sites), `internal/tui/heartbeat_test.go`, `docs/manual/commands.md`.
**Read first:** cmd/apogee/schedule.go — scheduleWiring.fire, idleGate; cmd/apogee/wire_options.go — ReportActivity wiring; internal/tui/heartbeat.go — foldBeatFailure, foldBeat, foldServerSwitch; internal/tui/tui.go — Options.ReportActivity; internal/notice/serveroffline.go — ServerOffline

**Tests.** `TestScheduleFiringRefusesWhenOffline` latches the (offline, failure) pair host-side through the same seam `ReportUpstream` is wired to, drives `fire` and asserts the returned `err.Error()` equals `notice.ServerOffline(endpoint, failure)`; a `heartbeat_test.go` test asserts `ReportUpstream` is called at exactly the three crossings (offline, back online, server switch) and not on a failed beat during a busy Exchange; `TestScheduleFiring*` (`schedule_test.go`) stay green, `TestScheduleFiringTakesNoBeatOfItsOwn` included; the `schedule_test.go` `runOnce` swaps that go are those asserting only the Spec each Driver built (item 9's rule) — named in the item's NOTES.

**Acceptance.** `go build ./... && go test ./cmd/apogee/ -run 'Schedule|Raise|Offline'`.

Commit: `refactor(cmd): the in-session Schedule Firing raises through raise and refuses when offline`

## 11. One agent-side Floor-guard table

**What.** `internal/agent/builtins.go` declares `floorGuards = []floorGuard{{key, moment, actionLabel, handler}, …}` in ladder order plus one `retryGuard(fn)` adapter replacing the four identical six-line retry adapters; `guardIDs` (`internal/agent/floorguards.go`), `buildBuiltins`, and `cmd/apogee/wire_settings.go`'s `setFloorGuard` cases derive from the table. `domain.FloorConfig`'s seven `Disable…` bools, the config keys, `floorGuardKeys` in `internal/config/reactions.go` and the registry rows stay as they are (ADR 0071 D5, 0076 D11); a test asserts the table's keys equal `floorGuardKeys`. Depends on item 4.

**Regression guard.** `floorGuardKeys` (`internal/config/reactions.go`) is unexported and `internal/agent` imports no `internal/config`, so export one side (`config.FloorGuardKeys()` or a facade `apogee.FloorGuardKeys()`) and home `TestFloorGuardTableMatchesTheConfigKeys` in `cmd/apogee/wire_settings_test.go` (imports both), asserting SET equality — the two lists differ in order (config enforcer-first, `guardIDs` salvage-first). `setFloorGuard` keeps its key→`config.Options`-field mapping in `cmd/apogee` (switch or map) and yields to `floorFromOptions`' documented "ONE negation seam" (`cmd/apogee/wire_settings.go`): derive only the KEY SET from the table (the `applyFloorGuard` rows / the switch's keys equal the exported keys), never route the write around `floorFromOptions`. Each row carries `gate func(domain.FloorConfig) bool` and `handler func(*Agent) domain.Handler` (or method expressions) — a package-level slice cannot hold bound `*Agent` methods — and `retryGuard` takes `func(*domain.Response) (string, bool)` with `repairToolCall` wrapping its `a.registeredToolNames()` argument in a closure (only three of the four adapters are identical). The twenty `TestFloorGuard_*` tests in `floorguards_test.go` are distinct behaviour tests, not a repeated fixture: the file is unchanged.

**Files:** `internal/agent/builtins.go`, `internal/agent/floorguards.go`, `internal/agent/builtins_test.go`, `internal/config/reactions.go` (or `apogee.go` for the facade export), `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`; `internal/agent/floorguards_test.go` unchanged.
**Read first:** `internal/agent/builtins.go` — buildBuiltins, engineBuiltin, classedBuiltin, repairToolCall, breakToolLoop; `internal/agent/floorguards.go` — guardIDs, guardToolCallSalvage…guardToolResultCap, guardActionRetry; `internal/agent/reactions.go` — armReactions; `internal/agent/reactions_test.go` — TestBuiltinReactionsAddTheContextFillNoticeLastWhenItsSwitchIsOn; `cmd/apogee/wire_settings.go` — setFloorGuard, applyFloorGuard, floorFromOptions, optionsFromFloor;
`cmd/apogee/wire_settings_test.go` — TestFloorRowAppliesOneGeneration, TestOptionsFromFloorInvertsFloorFromOptions; `internal/config/reactions.go` — floorGuardKeys; `internal/config/reactions_test.go` — TestFloorGuardKeysAreRegistryKeys

**Tests.** `TestFloorGuardTableMatchesTheConfigKeys` in `cmd/apogee/wire_settings_test.go` (set equality of the table's keys, the exported config keys and `setFloorGuard`'s keys); every existing guard-behaviour test (`TestFloorGuard_*`, `TestFloorRowAppliesOneGeneration`, `TestOptionsFromFloorInvertsFloorFromOptions`, `TestFloorGuardKeysAreRegistryKeys`) stays green.

**Acceptance.** `go build ./... && go test ./internal/agent/ ./cmd/apogee/ -run 'Floor|Guard|Builtin'`.

Commit: `refactor(agent): one Floor-guard table drives ids, builtins and the settings switch`

## 12. `KnownToolNames` is derived; one `HostToolsOf` composer

**What.** `internal/tools/registry.go`: `KnownToolNames` is computed from `builtinTools` with nil delegates — the three hand-appended names go. Add `tools.HostToolsOf(cfg domain.Config, seat *SubAgentSeatChoice) HostTools` and delete the two composers (`internal/agent/construct.go`, `cmd/apogee/wire_tools.go`) and the zero-value third in `cmd/apogee/wire_firing.go`. Depends on item 4.

**Regression guard.** `builtinTools` OMITS load_skill/ask_user/present_document when the delegate is nil, so it must construct the three unconditionally (a nil delegate is a legal constructor input — `KnownToolNames` does it today) and the composed set drops the nil-delegate ones in a separate step; `KnownToolNames` reads the unfiltered build (`TestKnownToolNamesCoversTheComposedSet`, `TestKnownToolNamesListsLoadSkill` stay green; `tools.disabled: [ask_user]` is never announced as a typo). The signature is `HostToolsOf(cfg domain.Config, seatChoice bool) HostTools` — there is no `SubAgentSeatChoice` type, the gate is `HostTools.SubAgentSeatChoice bool`; the engine passes false (no Config field for it, ADR 0031), `cmd/apogee` passes the `sub-agents-choice:` value. `internal/agent/construct_test.go` calls `hostTools` (`TestHostToolsFillsEveryHostField`, `TestHostToolsCarriesSecretEnvVars`, `TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts`) — those three move to `internal/tools/registry_test.go` over `HostToolsOf`, and `TestHostToolsForFillsEveryHostField` (`wire_tools_test.go`, calls the deleted `hostToolsFor`) is recast the same way. `firingWriteTarget`'s zero `HostTools{}` literal (`wire_firing.go`) is a lookup-only roster with no Config in scope: leave it (or point it at `NewDefaultRegistry(workspace)`); `registry.go`'s own `HostTools{}` zero literals stay.

**Files:** `internal/tools/registry.go`, `internal/tools/registry_test.go`, `internal/agent/construct.go`, `internal/agent/construct_test.go`, `cmd/apogee/wire_tools.go`, `cmd/apogee/wire_tools_test.go`, `cmd/apogee/wire_firing.go`.
**Read first:** `internal/tools/registry.go` — HostTools, builtinTools, DefaultToolsWithHost, NewDefaultRegistryWithHost, KnownToolNames, rosterDeltas; `internal/tools/registry_test.go` — TestKnownToolNamesCoversTheComposedSet, TestNewDefaultRegistryWithHost_RegistersAskUserOnlyWithAsker, TestNewDefaultRegistry_HoldsTheBuiltInTools; `internal/tools/load_skill_test.go` — TestKnownToolNamesListsLoadSkill; `internal/agent/construct.go` — hostTools, defaultRoster, composesDefaultRoster;
`internal/agent/construct_test.go` — TestHostToolsFillsEveryHostField, TestHostToolsCarriesSecretEnvVars, TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts; `cmd/apogee/wire_tools.go` — hostToolsFor, registryWithMCP; `cmd/apogee/wire_tools_test.go` — TestHostToolsForFillsEveryHostField; `cmd/apogee/wire_firing.go` — firingWriteTarget

**Tests.** `TestKnownToolNamesCoversTheComposedSet` and `TestKnownToolNamesListsLoadSkill` stay green over the derived list; `TestHostToolsFillsEveryHostField`, `TestHostToolsCarriesSecretEnvVars`, `TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts` move to `internal/tools/registry_test.go` over `HostToolsOf`; `TestHostToolsForFillsEveryHostField` is recast over `HostToolsOf(cfg, true)`.

**Acceptance.** `go build ./... && go test ./internal/tools/... ./internal/agent/ ./cmd/apogee/ -run 'KnownTool|HostTools'`; `grep -rn "HostTools{" --include=*.go internal cmd | grep -v _test | grep -v "HostTools{}"` shows one site.

Commit: `refactor(tools): KnownToolNames is derived and HostTools has one composer`

## 13. `tools.Classify` lives beside the markers it reads

**What.** Move `classifyTool` (`internal/agent/resolution.go`) to `internal/tools/classify.go` as `Classify(tool domain.Tool) ToolClass` with the eight classes exported; the predicate order becomes a documented table in that file; `internal/agent` consumes the class. The unfakeable markers stay unexported (contract §3.5). Depends on item 12.

**Regression guard.** `internal/agent/dispatch_test.go`'s `TestClassifyTool` calls `classifyTool` and names `toolClass`/`classReadOnly`…`classThirdPartyWrite` with fakes defined there (`fakeTool`, `externalTool`, `subprocTool`, `thirdPartyWriter`): either rewrite it in place over `tools.Classify`/`tools.Class…` or move it to `internal/tools/classify_test.go` and recreate the four fakes there (it is the seed of `TestClassifyEveryDefaultTool`). Doc rule: every live doc line naming `classifyTool` gets a dated pointer to `tools.Classify` — `grep -rn classifyTool docs CONTEXT.md | grep -v "docs/plans\|archived"` (today `docs/design/confinement-execution-contract.md` ×4 and ADR 0012 ×1; archived plans and designs stay historical) — dated in-place amendments per the plan's ADR-text call.

**Files:** `internal/tools/classify.go`, `internal/tools/classify_test.go`, `internal/agent/resolution.go`, `internal/agent/resolution_test.go`, `internal/agent/dispatch_test.go`, `docs/design/confinement-execution-contract.md`, `docs/adr/0012-*.md`.
**Read first:** `internal/agent/resolution.go` — classifyTool, toolClass, planAdmits, planOffers, resolveLadder, mcpServerAlias, gateCacheKey, gateReason; `internal/agent/dispatch_test.go` — TestClassifyTool, fakeTool, externalTool, subprocTool, thirdPartyWriter; `internal/agent/resolution_test.go` — TestResolve_LadderTable; `internal/tools/markers` (IsWorkspaceScopedWriter, IsURLFilteredNetworker, IsReadOnlySubprocess);
`internal/domain` — ExternalEffectTool, IsSubprocessTool, IsReadOnly; `internal/agent/loop.go` — toolMenu

**Tests.** `TestClassifyEveryDefaultTool` (a table of every `DefaultTools` name → class); `TestClassifyTool` rewritten or moved with its four fakes; `TestResolve_LadderTable` stays green.

**Acceptance.** `go build ./... && go test ./internal/tools/... ./internal/agent/ -run 'Classify|Ladder|Resolve'`.

Commit: `refactor(tools): Classify sits beside the markers; the ladder consumes the class`

## 14. One argument-key fold

**What.** `internal/security/dangerous.go`'s `foldKey` (strips `_`/`-`) is deleted; `payloadKeys` matching uses `domain.FoldArgumentKey` followed by the separator strip, so the guard and the dispatcher agree on what a key spells. `payloadKeys` itself stays where it is (its home is a deferred bead). Depends on item 4.

**Files:** `internal/security/dangerous.go`, `internal/security/dangerous_test.go`.
**Read first:** `internal/security/dangerous.go` — foldKey, isPayloadKey, payloadKeys, inspectableText (dropKeys fold at the same helper); `internal/domain/tools.go` — FoldArgumentKey, foldArgumentRune; `internal/security/dangerous_test.go` — TestDangerousActionGuard_PayloadKeySpellingVariants, TestDangerousActionGuard_PayloadTextNotInspected, TestWriteShapedViewDropsPromptAndSourceKeysTogether; `internal/tools/tools.go` — canonicalObject

**Tests.** The `~/.ssh`-in-a-document exemption table stays green; one case per separator variant and one Unicode-fold case.

**Acceptance.** `go build ./... && go test ./internal/security/... -run 'Dangerous|Payload|Fold'`; `grep -n "func foldKey" internal/security/*.go` prints nothing.

Commit: `refactor(security): the footgun guard folds argument keys the way dispatch does`

## 15. The subprocess mirror is deleted

**What.** Delete `subprocessSpec`/`subprocessResult` and `core()`/`fromCore()` in `internal/tools/exec_common.go`; the execution tools (`terminal.go`, `python_exec.go`, `run_tests.go`, `diagnostics.go`, `console_open.go`, `git.go`) build `subprocess.SubprocessSpec` and read `subprocess.Result` directly — a pure rename. Package-var test seams are NOT changed. Depends on item 4.

**Regression guard.** The core's result type is `subprocess.SubprocessResult` (not `subprocess.Result`). `terminal_test.go`, `python_exec_test.go` and `run_tests_test.go` build `subprocessSpec`/`subprocessResult` literals and assign fakes of the OLD signature to the `runTerminalSubprocess`/`runPythonSubprocess`/`runTestsSubprocess` seam vars: the seam vars stay but their TYPE follows `runSubprocess`'s new signature — `func(context.Context, subprocess.SubprocessSpec) (subprocess.SubprocessResult, error)` — so those fakes are renamed with them. `console_open.go` holds no mirror use (only `subprocess.ScratchEnv`): no change. `exec_common.go`'s comment recording the mirror as kept on purpose ("keeps its own spec and result shapes and converts at the seam rather than aliasing the core's") is superseded by this plan's header verdict 17 (IN, reduced — mirror deletion) and goes with the mirror.

**Files:** `internal/tools/exec_common.go`, `internal/tools/terminal.go`, `internal/tools/python_exec.go`, `internal/tools/run_tests.go`, `internal/tools/diagnostics.go`, `internal/tools/git.go`, `internal/tools/exec_common_test.go`, `internal/tools/terminal_test.go`, `internal/tools/python_exec_test.go`, `internal/tools/run_tests_test.go`; `internal/tools/console_open.go` no change.
**Read first:** `internal/tools/exec_common.go` — runSubprocess, subprocessSpec, subprocessResult, core, fromCore, RunHookSubprocess; `internal/tools/terminal.go` — runTerminalSubprocess, subprocessToolResult, isFailFastStop; `internal/tools/run_tests.go` — runTestsSubprocess, condenseTestOutput; `internal/tools/python_exec.go` — runPythonSubprocess, pythonVersionSpec; `internal/tools/git.go` — runGit, runGitUnchecked, gitResultText;
`internal/tools/diagnostics.go` — goVetSpec; `internal/tools/terminal_test.go` — withCapturedTerminalRun; `internal/subprocess/subprocess.go` — SubprocessSpec, SubprocessResult, RunSubprocess

**Tests.** Every existing tools test stays green (field-name rename only); the seam fakes in `terminal_test.go`, `python_exec_test.go`, `run_tests_test.go` are renamed to the core types.

**Acceptance.** `go build ./... && go test ./internal/tools/...`; `grep -n "subprocessSpec\|fromCore\|\.core()" internal/tools/*.go` prints nothing.

Commit: `refactor(tools): execution tools use subprocess.SubprocessSpec directly`

## 16. `git.go`'s one-line re-exports go

**What.** The eight `gitexec` re-exports in `internal/tools/git.go` are deleted; callers name `gitexec.*` directly. The two six-line `runGit`/`runGitUnchecked` adapters and the `lookGit` seam stay. Depends on item 15.

**Regression guard.** describe runGit/runGitUnchecked/lookGit by name, never by line count; item 15 executes before 16. The re-exports are the five one-liners `safeGitEnv`, `gitDiffHardeningArgs`, `gitProgram`, `resolveGit`, `gitCommandConfigName`; `lookGit`, `runGit`, `runGitUnchecked` and `RunGitQuery` (the engine funnel `internal/agent/treesnapshot.go` calls) stay regardless of their size after item 15. `gitProgram` is also called from `internal/tools/git_stage.go` (`stageGitPaths`), `safeGitEnv` from `git_stage_test.go` (`gitStatusPorcelain`) and `git_windows_test.go` (`//go:build windows`, unseen by the Linux `go test` and by `make cross`, which compiles no test files) — all in scope. Every inlined `gitexec.Program`/`gitexec.Resolve` call passes `lookGit`, or `withFakeGit` unhooks and the fake-git tests resolve the real PATH git. Prose rule: every comment or design line naming a deleted identifier is repointed to its `gitexec.*` name (`internal/tools/readonly_subprocess.go`, `internal/mcp/transport.go`, `docs/design/confinement-execution-contract.md`, `docs/design/mcp-client.md`, `git.go`/`git_test.go` comments); archived plans and designs stay historical.

**Files:** `internal/tools/git.go`, `internal/tools/git_test.go`, `internal/tools/git_stage.go`, `internal/tools/git_stage_test.go`, `internal/tools/git_windows_test.go`, `internal/tools/readonly_subprocess.go`, `internal/mcp/transport.go`, `docs/design/confinement-execution-contract.md`, `docs/design/mcp-client.md`.
**Read first:** `internal/tools/git.go` — safeGitEnv, gitDiffHardeningArgs, lookGit, gitProgram, resolveGit, runGit, runGitUnchecked, gitCommandConfigName, RunGitQuery; `internal/gitexec/gitexec.go` — SafeEnv, DiffHardeningArgs, LookFunc, LookPath, Program, Resolve, Capture, CaptureUnchecked, CommandConfigName; `internal/tools/git_test.go` — withFakeGit, hasHardeningPair, TestRunGitQuery_ReturnsStdoutAloneAndAppliesHardening; `internal/tools/git_stage.go` — stageGitPaths;
`internal/tools/git_stage_test.go` — gitStatusPorcelain; `internal/tools/git_windows_test.go` — TestSafeGitEnv_WindowsCarriesTheSystemFloor; `internal/tools/readonly_subprocess.go` — readOnlySubprocess; `internal/agent/treesnapshot.go` — RunGitQuery caller

**Tests.** Existing git tool tests stay green, the not-parallel `withFakeGit` tests included; `TestSafeGitEnv_WindowsCarriesTheSystemFloor` is type-checked by the Windows vet below.

**Acceptance.** `go build ./... && go test ./internal/tools/... -run 'Git'`; `GOOS=windows CGO_ENABLED=0 go vet ./internal/tools/`; `grep -n 'gitexec\.\(Program\|Resolve\)(' internal/tools/*.go` shows only sites ending in `, lookGit)`; `grep -rn 'safeGitEnv\|gitDiffHardeningArgs\|gitProgram\|resolveGit\|gitCommandConfigName' internal cmd docs/design docs/adr CONTEXT.md --include=*.go --include=*.md | grep -v archived` prints nothing (plans and archived designs are historical and stay).

Commit: `refactor(tools): drop the gitexec re-exports`
