# Safety floors and subprocess funnel closure

**Goal:** every program apogee spawns goes through the one funnel that fences, scrubs, hardens
and tears down — no bare `exec.Command` is left outside it — and the three floors the operator
is told to rely on (confinement capability honesty, the dangerous-action guard's everyday
idiom, document extraction) hold as written.

**Date:** 2026-08-26 · **Status:** unexecuted · **sized for:** ~200k-context host

**Evidence (security audit 2026-08-25 `docs/skill-runs/security-audit/2026-08-25/report.md`,
code audit `docs/reviews/code-audit-2026-08-25.md`, merged in
`docs/handoffs/2026-08-26 - 00 - merged-audit-findings.md` §3.1/§3.5/§3.8):** the subprocess
funnel (`refuseExecFromWritablePath`, `subprocessEnv`, `safeGitEnv`, `gitHardeningOptions`,
`runSubprocess`'s pgroup/Job-Object teardown) is bypassed at eight spawn sites on a stock install.
Worst: `internal/agent/treesnapshot.go:99-106` runs a bare `git` — unfenced, unhardened, full
environment including `APOGEE_API_KEY`, no teardown — twice around **every** subprocess tool call
in **every** mode, Bypass included (F-05, four audit families independently). The terminal tool's
own shell argv[0] (`internal/tools/terminal.go:134`) and the `pipefail` probe
(`internal/platform/host.go:521-522`, a `sync.OnceValue` fired BEFORE `runSubprocess` scrubs or
confines) resolve a bare `sh` over apogee's inherited PATH (F-01/F-02 = C-01, independently
verified). `move_file`/`delete_file` spawn git with no confinement handle in every mode (F-03);
git hardening leaves `core.fsmonitor`, a repo-named program git runs on nearly every call (F-10);
the settings editor discards the absolute path `LookPath` returned (F-07); the keystore probe and
stdio MCP launch are unfenced, the latter without a process group (F-35, F-36, F-42). Floors:
landlock ABI 1–2 (Ubuntu 22.04, Debian 12, RHEL 9) leaves `truncate(2)` unfenced while
`Capabilities().FSWrite` reports true — **live-reproduced** (C-06); `rm -rf -- /`,
`rm --recursive --force /`, `rm -rf "/etc"` and `curl x | /bin/bash` all walk past the guard
(C-10); a 594-byte PDF declaring `/Count 10000000` cost 51 s and ~142 GiB of churn, and under a
memory ceiling dies in a runtime OOM `recover()` cannot catch (C-07, measured; F-25 `/Size`
allocation, F-26 unbounded reference cycles).

**Authoritative sources:**
- `internal/security/execsafety.go:40` (`RefuseExecFromWritablePath`), `:65` (`execFences`);
  `internal/security/doc.go:110-112` (si-34: every exec site is fenced);
  `internal/tools/path_safety.go:40,47` (`refuseExecFromWritablePath`, `confinementBox`).
- `internal/tools/exec_common.go:102` (`subprocessEnv`), `:128` (`subprocessEnvScopedPath`),
  `:301` (`runSubprocess`), `:450` (`RunHookSubprocess`), `:555` (`shellHost`);
  `internal/tools/exec_teardown.go` (`processTeardown`, `runWithTeardown`),
  `internal/tools/exec_pgroup_unix.go`, `internal/tools/exec_pgroup_other.go` (Windows Job Object).
- `internal/tools/terminal.go:130-134`; `internal/tools/console_open.go:185-196` (`consoleArgv`,
  the fence shape to reuse); `internal/platform/host.go:521-546` (`failFastPreambleOnce`,
  `FailFastPreamble`, `composeFailFastPreamble`); `internal/platform/platform.go:17` (`Shell`).
- `internal/tools/git.go:84` (`safeGitEnv`), `:102` (`gitHardeningOptions`), `:118`
  (`gitHardeningEnv`), `:142` (`gitProgram`), `:170` (`runGit`), `:184` (`runGitUnchecked`),
  `:207` (`gitFilterConfigName`), `:271` (`repoLocalFilterDrivers`), `:302` (`gitFilterRefusal`),
  `:588` (commit argv); `internal/tools/git_stage.go:52-83` (`stageGitPaths`).
- `internal/agent/treesnapshot.go:99-106`; `internal/agent/dispatch.go:808` (handle install),
  `:821,845,850` (floor calls), `:498` (`executeRun`), `:511` (`executeGate`), `:528`
  (`executeConfine`), `:545` (`executeConfineFallback`), `:611` (`guardRefusalMessage`).
- `internal/agent/resolution.go:150` (`box`), `:155` (`fallback`), `:205` (input `box`), `:304`
  (`classifyTool`), `:388` (`resolveLadderAuto`), `:433` (`applyOverlays`), `:468`
  (`finishGate`), `:586` (`finishConfine`), `:600` (`confineFallback`);
  `internal/agent/resolution_test.go:442-500` (`TestResolve_GuardTier2ForcesGate`, subtest at `:470`).
- `internal/security/rules.go:27` (`homeAnchor`), `:36` (`deleteTargetAnchor`), `:54,61` (the two
  `rm` rules), `:131` (`write-apogee-control-plane`, `TierHardRefuse`), `:155`
  (`remote-pipe-to-shell`); `internal/security/dangerous.go:294` (`normalize`);
  `internal/security/dangerous_test.go:30,70,95` (the three tier tables); `ISSUES.md:525-527`
  (the L2 parenthetical).
- `internal/platform/landlock_linux.go:79` (`landlockABITruncate = 3`), `:120` (`accessMaskForABI`),
  `:183` (`Capabilities`); `internal/domain/confinement.go:54` (`ConfinementCaps`);
  `internal/probe/confinement.go:44` (`CapabilityLine`), `:70` (`DegradedNotice`);
  `internal/tui/confine.go:101,127,152,165`; `cmd/apogee/wire_options.go:158,253`,
  `cmd/apogee/headless.go:290`, `cmd/apogee/daemon.go:548` (the notice's gate callers);
  `internal/platform/confinetest/confinetest.go:69` (`Probe`), `lines_other.go`.
- `internal/doctext/pdf.go:78-119` (`ExtractPDF`); the parser `github.com/ledongthuc/pdf`
  (`read.go:233,392` allocate `make([]xref, size)` from `/Size`; `page.go:25-56` `Page()` walks
  `Kids` with no visit bound; `read.go:55` "no value cache"); `internal/agent/loop.go:999-1027`
  (`resolveFileRefs`), `internal/agent/dispatch.go:1051` (`clampToBound`);
  `internal/tools/read_file.go:134-143` (`readableText`).
- `cmd/apogee/settingsedit.go:363-374` (`resolveEditor`), `internal/present/opener.go:228-241`
  (`resolveProgram`, the treatment to copy); `internal/keystore/keystore.go:81-108` (`Probe`,
  `probe`), `cmd/apogee/keymigrate.go:43`; `internal/mcp/transport.go:114-123`
  (`buildStdioTransport`), `internal/mcp/client.go:38,58,82,149`, `cmd/apogee/wire_live.go:41,51`;
  the SDK's `CommandTransport` (`mcp/cmd.go`: `Connect` starts the cmd, `pipeRWC.Close` signals
  the LEADER alone).
- ADR 0012 (guard tiers, tighten-only; `confine-to-workspace`), ADR 0049 §4 (`~/.apogee` = a
  Tier-2 forced look; approval is final), ADR 0056 decision 4 (the tracked-file floor's 2 s
  best-effort contract), ADR 0020 (Windows token backend), ADR 0019 §5 (opener runs host-side),
  ADR 0006 (structural floors), ADR 0031 (Driver parity).
- `docs/design/confinement-execution-contract.md` §2.4 (teardown), §3 (`workspaceScopedWriter`),
  §4 (Resolution: forced gate at `:499-500`, D4 fallback at `:610-614`), §5 (capability honesty),
  §6.2 (the escape battery, 11 rows); `docs/design/mcp-client.md:46` (the stdio bullet).
- `CONTEXT.md` **Dangerous-action guard** (`:771`), **Resolution** (`:786`), **Confinement**
  (`:544`).

**Ratified design calls (owner, 2026-08-26, via AskUserQuestion):**
1. C-06 = **disclose as residual, keep Auto**: `Capabilities` gains a residual (the list of
   unfenced accesses), `probe.DegradedNotice`'s family names it, the contract §6.2 gets a
   `truncate(2)` row and a battery case; `FSWrite` stays true on landlock ABI 1–2.
2. C-10 is fixed for **everyday idiom** (`--`, long flags, quoted absolute targets, absolute shell
   paths after a pipe); the guard remains "not a security boundary" — the `doc.go` stance is kept
   and deliberate obfuscation stays out of scope.
3. The `~/.apogee` write rule: **code follows ADR 0049** → a Tier-2 forced look
   (`TierForceApproval`), Hint and WritesOnly kept.
4. An approved Tier-2 forced gate **keeps the Confine box**: approval decides whether the call may
   run, confinement decides where; the guard is tighten-only, so a forced look never loosens the
   fence Auto would have applied.
5. Exec fence: **one resolver, both consumers** — a single resolver (`LookPath` +
   `refuseExecFromWritablePath`, absolute path) is used by the terminal tool's `shellHost.Command`
   consumer, the `FailFastPreamble` probe, `RunHookSubprocess` and `console_open` (which already
   fences — its shape is reused). The probe additionally runs with the scrubbed environment from
   `exec_common.go` and through the Confiner path where one exists.
6. Accepted-risk candidates: none denied (`ISSUES.md` L3/L4/L5/L6 stand; only the L2
   parenthetical is reworded, item 10).
7. (Author — open call, recommended) The resolver is `security.ResolveProgram` in
   `internal/security/execsafety.go` — the fence's own home, and the only package every consumer
   (`tools`, `platform`, `present`, `mcp`, `keystore`, `cmd/apogee`) can import without pulling
   `internal/platform`'s OS surface; the bare shell NAME comes from a new `platform.Shell.Shell()`
   rule-table method. The five sites that already pair `LookPath` with the fence
   (`git.go:142`, `python_exec.go:239`, `run_tests.go:249`, `diagnostics.go:200`, `opener.go:228`)
   are NOT migrated in this plan.
8. (Author — open call, recommended) F-03: the `Subprocess` marker is NOT declared on
   `move_file`/`delete_file` — `classifyTool` would then class them as subprocess and GATE every
   in-workspace rename in Allow-Edits, undoing the 2026-08-22 git-aware file-ops feature. Instead
   the Run verdict of a workspace-scoped writer in the confining cell (Auto · confine-to-workspace
   · fs caps) carries the box and executes with the confinement handle installed, so its git child
   confines exactly as a git tool's would; elsewhere the child's bound is its hardened argv (item 4).
9. (Author, no user-visible alternative) The process-tree teardown seam (`processTeardown`,
   `setProcessGroupTeardown`, `runWithTeardown`, the Job Object) moves from `internal/tools` to
   `internal/platform` as an exported facility, so `internal/mcp` and any future spawner share
   one implementation instead of a copy.
10. (owner, 2026-08-26) A stdio MCP server's cwd **stays the workspace**: with the program
    resolved to a fenced absolute path the relative-lookup concern is closed, and filesystem-style
    MCP servers expect the workspace; its environment is unchanged (`ISSUES.md` L4 stands).
11. (Author — open call, recommended) The `.git/hooks|config|modules` rule stays
    `TierHardRefuse`; ADR 0049 §4's parenthetical, which names `.git/` beside `~/.apogee` as Tier-2,
    is narrowed to `~/.apogee` with a dated note.
12. (Author, no user-visible alternative) PDF bounds are package constants: the walk covers at
    most 2 000 pages, stops after 25 consecutive pages that yield neither text nor an error, the
    parser's reads are budgeted at 200 000 `ReadAt` calls and honour a ctx, and a declared `/Size`
    larger than the file's byte length is refused before `pdf.NewReader` allocates.
13. (owner, 2026-08-26) **The pipefail probe is removed; the preamble self-detects.**
    `FailFastPreamble` becomes the constant `set -e\n(set -o pipefail) 2>/dev/null && set -o pipefail\n`
    — no boot spawn, no `sync.OnceValue` memo, no `sh -c` probe anywhere. This supersedes the
    "probe … through the Confiner path" clause of call 5; the resolver and `RunHookSubprocess`
    halves of call 5 stand.

**Standing requirements:**
- `skills: coding-standards`
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see closing note).
- Every item's Acceptance is targeted; `make check` runs once at closeout.
- After item 1 no production code outside `runSubprocess`'s funnel, `security.ResolveProgram`
  and the five already-fenced sites named in call 7 may call `exec.Command`/`exec.LookPath` on a
  bare name; each later item that spawns states which funnel entry it uses.
- Nothing in this plan is a Mechanism: every change is a structural floor or a security seam
  (ADR 0006 class), so the Bypass invariant is untouched by construction — no item may add a
  config key that turns any of it off.
- Windows halves are written to the same contract as the POSIX halves and compile on every
  target (`GOOS=windows go build ./...` is part of each such item's Acceptance); native Windows
  verification is the owner's CI/box, not a gate on the commit.

**Out of scope:**
- The untrusted-text → seam batch (C-08/C-09/C-11/C-12/C-17, F-16/F-19/F-30/F-32), approval
  integrity (F-11/F-12/F-17/F-22), surfaces that lie (F-14/F-15/F-20/F-23/F-24/F-28/F-29/F-31),
  read fence + egress (F-13/F-18/F-21/F-40/F-41), posture drift (F-04/F-06/F-08/F-09), consoles +
  delegation (F-37/F-38/F-43), exhaustion + CI (F-27/F-33/F-34) — each is its own wave plan per the
  handoff §5; nothing here pre-empts them.
- An env-allowlist scrub for stdio MCP servers (`ISSUES.md` L4, deliberate), read-confinement or
  default-deny egress (L3), a switch for the exec fence (L5), the backgrounded-process reap (L6).
- Migrating the five already-fenced exec sites onto `security.ResolveProgram` (call 7) — an
  architecture-pass candidate, recorded in `ISSUES.md` by item 1.
- Changing the guard's normalisation (quoting, `$'…'`, `eval`, variable expansion): the
  obfuscation chase the guard declines by design (call 2).
- A truncate-capable landlock fallback on ABI 1–2 (there is none; call 1 discloses instead).
- `docs/manual` coverage of `mcp-servers:` (R-undoc) — item 7 amends `docs/design/mcp-client.md`,
  which is where the stdio contract lives today.

---

## 1. `security.ResolveProgram` — the one exec resolver; the terminal tool's shell goes through it — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `internal/tools/terminal_test.go` was not in the item's Files list but had to be
touched: `TestTerminal_ScopesTheWorkspaceOffTheChildPATH` builds a synthetic PATH with no shell on
it, which the tool now resolves before it builds the spec — the fixture appends the host shell's own
directory (the assertions are about which entries survive the scrub, not about a host without `sh`).
NOTES (2026-08-26): `prependPATH` in `exec_fence_test.go` fires `platform.FailFastPreamble()` before
planting a shell: the probe memoizes for the life of the process, and a planted `sh` that exits 0 for
anything would teach it the host accepts `set -o pipefail` and break every later terminal test. Item
2 deletes that probe outright.
NOTES (2026-08-26): `console_open`'s lookup-failure wording moves from `shell not available: …` to
the resolver's `sh not available: …` — the fence refusal the existing test pins (`resolves inside`,
naming the resolved path) is byte-identical, and both consumers now surface one sentence.

**What:** the fence gains its complete form (resolve + fence), the platform names its shell, and
the two shell consumers use both.
- `internal/security/execsafety.go`: add
  `func ResolveProgram(look func(string) (string, error), name, root string, box *domain.ConfinementBox) (string, error)`.
  `look == nil` ⇒ `exec.LookPath`. Steps, in order: `look(name)`; a lookup error is returned
  wrapped (`%w`) so callers can tell "absent" from "refused"; a result that is not
  `filepath.IsAbs` (a relative PATH entry, Go's `exec.ErrDot`) is refused with
  `ErrExecFromWritablePath` wrapped and the sentence "resolves to a relative program path" —
  the same rule `present/opener.go:228-241` applies; then `RefuseExecFromWritablePath(resolved,
  root, box)`; return the absolute path. Doc comment: this is the fence's complete form and the
  entry every new exec site takes; the empty-fence rule of `RefuseExecFromWritablePath` (no root,
  no box ⇒ refuses nothing) carries over unchanged. `internal/security/doc.go:110-112` gains one
  sentence naming `ResolveProgram` beside `RefuseExecFromWritablePath`.
- `internal/platform/platform.go:17` `Shell` interface gains `Shell() string` — "the bare program
  `Command` wraps a line in: `sh` on POSIX, `cmd` on Windows; a NAME, resolved by the caller,
  never a path". `internal/platform/host.go` `hostRules` implements it from the same rule table
  `Command` (`:86`) reads, so the two cannot disagree.
- `internal/tools/exec_common.go`: add `func resolveShell(ctx context.Context, root string) (string, error)`
  = `security.ResolveProgram(nil, shellHost.Shell(), root, confinementBox(ctx))`, and
  `func shellArgv(ctx context.Context, root, command string) ([]string, error)` = `shellHost.Command(command)`
  with `argv[0]` replaced by the resolved shell. Binding: `shellHost.Command`'s bare `argv[0]` is
  never handed to `runSubprocess` again — both consumers below build their argv through `shellArgv`.
- `internal/tools/terminal.go:134`: `argv, err := shellArgv(ctx, t.root, command)`; an error
  returns `errorResult(call.ID, err.Error())` — the fence's own sentence, which names the resolved
  path (the operator reads which PATH entry to fix; the model reads a refusal, not "not available").
  The Windows raw-command-line path (`spec.cmdline`) is unchanged: `argv[0]` is now the absolute
  `cmd.exe`, which `exec_cmdline_other.go` launches with the verbatim line as before.
- `internal/tools/console_open.go:185-196` `consoleArgv`: its own `exec.LookPath` + fence block is
  replaced by one `shellArgv` call; the refusal wording stays byte-identical to today's (the
  existing test pins it).
- Layering (binding): `internal/security` resolves and fences; `internal/platform` only names the
  shell; `internal/tools` composes. No package below `tools` learns about confinement handles.
- `ISSUES.md`: add a short "Improvements / Ideas" line — the five sites of call 7 still pair
  `LookPath` with the fence by hand; fold them onto `ResolveProgram` in an architecture pass.

**Files:** `internal/security/execsafety.go`, `internal/security/doc.go`,
`internal/security/execsafety_test.go`, `internal/platform/platform.go`,
`internal/platform/host.go`, `internal/platform/host_test.go`, `internal/tools/exec_common.go`,
`internal/tools/terminal.go`, `internal/tools/console_open.go`, `internal/tools/exec_fence_test.go`,
`ISSUES.md`

**Tests:** `execsafety_test.go` — `TestResolveProgram` table: an absolute program outside the
fence resolves to itself; a name whose `look` lands inside `root` is refused and the error names
the resolved path and wraps `ErrExecFromWritablePath`; a `look` returning a relative path is
refused with the "relative program path" sentence; a `look` error is returned wrapped and is NOT
`ErrExecFromWritablePath`; a nil `look` uses PATH (resolve `go`'s own test binary name via
`os.Executable`'s dir on PATH). `host_test.go` — `Shell()` is `sh` for the POSIX rules and `cmd`
for the Windows rules, and equals `Command("")[0]`. `exec_fence_test.go`
`TestEveryExecSiteRefusesAProgramInsideTheWorkspace` gains two rows, `terminal` and
`console_open`: plant `node_modules/.bin/sh` inside the workspace (`plantExecutable`), prepend its
directory to PATH with `t.Setenv`, run the tool, assert an error result naming the planted path
(the rows skip on Windows, whose shell is `cmd`); `TestConsoleOpen_RefusesAShellResolvingInsideTheWorkspace`
stays green unchanged. A terminal row also asserts the SUCCESS shape: with a normal PATH,
`captured.argv[0]` (via `withCapturedTerminalRun`) is absolute.

**Acceptance:** `go build ./... && GOOS=windows go build ./... && go test ./internal/security/ ./internal/platform/ ./internal/tools/`

**Commit:** `fix(security): one exec resolver; the terminal and console shells resolve through the fence`

---

## 2. The `pipefail` probe is deleted — the preamble self-detects; `RunHookSubprocess` resolves its argv[0] — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `internal/tools/exec_fence_test.go` is not in the item's Files list but had to be
touched. Item 1 added a `platform.FailFastPreamble()` call plus an explaining paragraph to
`prependPATH`, purely to fire the one-shot probe against the ambient PATH before a fake `sh` was
planted. This item deletes that probe, so the call is inert and the paragraph describes a mechanism
that no longer exists; both are removed, along with the file's now-unused `platform` import.
NOTES (2026-08-26): `internal/tools/doc.go` is not in the item's Files list either — its
`RunHookSubprocess` sentence enumerates what the door gives a hook ("the same scrub, teardown, cap
and clamp"), and the exec fence is now one of them, so "exec fence on its argv[0]" joins the list.
NOTES (2026-08-26): `runExternalFormatter` gained a `workspaceRoot string` parameter to carry
`deps.WritableBox.WorkspaceRoot` to the call at `autofix.go:322` — `deps` is not in scope inside
that function, only in `newAutofix`, where the closure that builds the external rung now passes it.
NOTES (2026-08-26): the Tests section names four existing `TestRunHookSubprocess*` tests to update;
they were updated and `TestRunHookSubprocessRefusesAProgramInsideTheWorkspace` added as specified,
plus one extra — `TestRunHookSubprocessResolvesABareProgramNameToAnAbsolutePath` — pinning the
success half (a bare name still resolves and runs, and argv[0] becomes absolute), because the
refusal test alone would pass against a door that refused everything.

Depends on item 1.

**What:** call 13 closes F-02/C-01 by removing the spawn: the only `exec.Command` outside the
funnel is deleted, not fenced; the hook door's unfenced argv[0] goes through the resolver.
- `internal/platform/host.go:516-546`: delete `failFastPreambleOnce` (the `exec.Command("sh", "-c",
  "set -o pipefail")` probe, the `sync.OnceValue`) and `composeFailFastPreamble`.
  `FailFastPreamble()` returns the package constant
  `failFastPreamble = "set -e\n(set -o pipefail) 2>/dev/null && set -o pipefail\n"` — the same
  bytes on every host, no per-process state. Its doc comment states WHY the line is correct on
  every POSIX `sh`: the subshell tries the option; on a shell without it (dash < 0.5.13) `set`
  fails INSIDE the subshell, its diagnostic goes to `/dev/null`, and the AND-list skips the real
  `set -o pipefail`; POSIX exempts every command of an AND-OR list but the last from `set -e`, and
  the failing command is the FIRST, so the script continues; on bash / zsh / ksh / newer dash the
  subshell succeeds and the option is set for the script. The existing paragraph on the AND-OR
  exemption and the kill-on-denial watch stays. `os/exec` and `sync` leave `host.go`'s imports
  (`:5,7`) if nothing else in the file uses them — `platform` then spawns nothing at all.
- `internal/tools/terminal.go:130` keeps calling `platform.FailFastPreamble()` — no change; the
  battery drivers (`internal/platform/confiner_windows_test.go:45` and the `confinetest.go:55-69`
  doc) keep passing it — no change; contract §6.2's 2026-08-22 note — no change.
- `internal/tools/exec_common.go:450` `RunHookSubprocess` gains a `workspaceRoot string` parameter
  after `dir`; before building the spec it resolves `argv[0]` through
  `security.ResolveProgram(nil, argv[0], workspaceRoot, box)` where `box` is the ctx handle's
  `Box` when a handle is present (`domain.ConfinementFromContext`), nil otherwise; the resolved
  absolute path replaces `argv[0]`; a refusal is returned as the funnel's error. Doc comment
  amended: the funnel fences its own argv[0]; a caller's earlier fence is belt, this is braces.
  `internal/mechanisms/autofix.go:322` passes `deps.WritableBox.WorkspaceRoot`.
- `internal/platform/host_test.go:473-500`: `TestComposeFailFastPreamble` and
  `TestFailFastPreambleMatchesTheHostProbe` are deleted (their subjects are gone); the
  replacements are in Tests below. `internal/tools/terminal_test.go:486`
  `TestTerminal_PipefailFailsAPipelineWhereSupported`'s skip predicate no longer reads the
  preamble (it always names pipefail now): it probes the host `sh` in the TEST
  (`exec.Command("sh", "-c", "set -o pipefail").Run() == nil` — test code may spawn; production
  does not) and skips when unsupported; `:529` is unchanged.

**Files:** `internal/platform/host.go`, `internal/platform/host_test.go`,
`internal/tools/terminal_test.go`, `internal/tools/exec_common.go`,
`internal/tools/exec_common_test.go`, `internal/mechanisms/autofix.go`, `CHANGELOG.md`

**Tests:** `host_test.go` — `TestFailFastPreambleIsTheSelfDetectingConstant` asserts the literal
`"set -e\n(set -o pipefail) 2>/dev/null && set -o pipefail\n"`;
`TestFailFastPreambleRunsUnderEveryHostShell` (skips on Windows) is a table over `sh`, `bash`,
`dash`, `zsh`, `ksh` — each row skips when `exec.LookPath` finds no such shell — asserting three
things per shell with `<shell> -c "<preamble><line>"`: (a) `echo ok` exits 0 and prints exactly
`ok` (the preamble is silent and never aborts a supporting or a non-supporting shell); (b)
`false | cat` exits non-zero exactly when `<shell> -c "set -o pipefail"` exits 0 (pipefail is
honoured precisely where the shell supports it, and its absence never breaks the line); (c)
`false; echo reached` exits non-zero and prints nothing (`set -e` is intact after the AND-list).
`terminal_test.go` — `TestTerminal_PrependsFailFastPreambleToThePOSIXLine` passes unchanged
(`want = platform.FailFastPreamble() + "echo hi"`); `TestTerminal_PipefailFailsAPipelineWhereSupported`
with the new skip predicate. `exec_common_test.go` —
`TestRunHookSubprocessRefusesAProgramInsideTheWorkspace` (planted hook program under
`workspaceRoot` ⇒ error wraps `ErrExecFromWritablePath`); the four existing
`TestRunHookSubprocess*` tests pass the new argument.

**Acceptance:** `go build ./... && GOOS=windows go build ./... && go test ./internal/platform/ ./internal/tools/ ./internal/mechanisms/`

**Commit:** `fix(platform): the fail-fast preamble self-detects pipefail — no host probe; hook subprocesses resolve argv[0] through the fence`

---

## 3. The tracked-file mutation floor runs its git through the tools funnel — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item's text has `RunGitQuery` call `gitProgram`, whose refusal is a
STRING; returning `errors.New(refusal)` would have dropped the `ErrExecFromWritablePath` sentinel
the item's own `TestRunGitQuery_RefusesAPlantedGit` asserts. `gitProgram`'s body was therefore
extracted into `resolveGit` — the identical resolve-and-fence, returning the error instead of its
rendering — and `gitProgram` is now that function plus the render, byte-identical for every
existing caller.
NOTES (2026-08-26): `internal/tools/doc.go` is not in the item's Files list but had to be touched:
the package map's `git.go` line enumerates what the file holds, and the file now also holds
`RunGitQuery`, the package's one exported non-tool entry — the repo's own convention is that the
map covers every file's role, so a new exported facility that the map does not mention is exactly
the rot the map exists to prevent.

**What:** F-05. `treesnapshot.go`'s three raw `exec.CommandContext(ctx, "git", …)` runs become
calls into `internal/tools`' git funnel — fence, `-c` hardening, `GIT_CONFIG_NOSYSTEM`,
`safeGitEnv` allowlist with workspace-scoped PATH, credential scrub, repo-local command-config
refusal, and the §2.4 teardown — while ADR 0056 decision 4's contract (2 s per run, workspace
root as cwd, any failure = silent skip) is kept to the letter.
- `internal/tools/git.go`: extract the spec builder out of `runGitUnchecked` (`:184-198`) into
  `func gitRunSpec(gitPath, root string, timeout time.Duration, gitArgs ...string) subprocessSpec`
  (hardening options and env applied there — `runGitUnchecked` becomes the one-liner that runs it,
  behaviour byte-identical). Add the exported
  `func RunGitQuery(ctx context.Context, root string, timeout time.Duration, args ...string) (stdout string, err error)`:
  `gitProgram(ctx, root)` (absent git or fence refusal ⇒ error), `probeFilterDrivers` (a refused
  repository ⇒ error), then `runSubprocess` over `gitRunSpec(...)` with `splitStdout: true`; a
  timeout, a wedged drain or a non-zero exit is an error too. Doc: the engine's own read-side git
  — every failure is one error, because its only caller treats every failure as "skip"; it is NOT
  for tool results (those keep `runGit`'s captured outcome).
- `internal/agent/treesnapshot.go:99-106`: `git(ctx, args...)` calls
  `tools.RunGitQuery(ctx, t.root, treeSnapshotTimeout, args...)` — the `context.WithTimeout` stays
  as the outer bound. `active`, `beforeCall` and `mutationWarning` take a `ctx`.
- `internal/agent/dispatch.go`: capture `floorCtx := ctx` immediately BEFORE the confinement
  handle is installed (`:808`) and pass it at `:821`, `:845`, `:850`. Binding: the floor's git
  never runs inside the call's box — a confined run would pay the re-exec wrapper twice per call
  and, on Windows, two extra label walks (ADR 0020) — and it never trips the D4 demote. The
  call's cancellation still reaches it (a cancelled Turn skips the check, per contract).
- `docs/adr/0056-terminal-fail-fast-and-session-scratch.md` decision 4: one dated sentence — the
  snapshot runs through the same hardened git funnel as the git tools (`tools.RunGitQuery`); the
  2 s / silent-skip contract is unchanged.

**Files:** `internal/tools/git.go`, `internal/tools/git_test.go`,
`internal/agent/treesnapshot.go`, `internal/agent/treesnapshot_test.go`,
`internal/agent/dispatch.go`, `docs/adr/0056-terminal-fail-fast-and-session-scratch.md`,
`CHANGELOG.md`

**Tests:** `git_test.go` — `TestRunGitQuery_ReturnsStdoutAloneAndAppliesHardening` (fake git on a
temp PATH dir outside the workspace records argv/env: `-c core.hooksPath=` precedes the
subcommand, `GIT_CONFIG_NOSYSTEM=1` is set, stdout comes back without the fake's stderr line);
`TestRunGitQuery_RefusesAPlantedGit` (fake git INSIDE root on PATH ⇒ error wraps
`ErrExecFromWritablePath`); a non-zero exit is an error. `treesnapshot_test.go` —
`TestTreeSnapshot_GitRunsThroughTheFunnel`: `t.Setenv("APOGEE_API_KEY", "secret")`, a recording
fake git first on PATH; after one subprocess call the record shows the hardening option and no
`APOGEE_API_KEY`; `TestTreeSnapshot_PlantedGitTurnsTheFloorOff`: the fake git lives inside the
workspace ⇒ no warning, no record written; `TestTreeSnapshot_ConfinedCallSnapshotsOutsideTheBox`:
a dispatch with a fake Confiner and a Confine verdict — the Confiner's `Confine` is called once
(the tool), never for the snapshot. The eight existing `TestTreeSnapshot_*` tests pass unchanged.

**Acceptance:** `go build ./... && go test ./internal/tools/ ./internal/agent/`

**Commit:** `fix(agent): the tracked-file mutation floor runs git through the hardened tools funnel`

---

## 4. git hardening: neutralise `core.fsmonitor`, sign nothing, refuse every command-valued repo-local key — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item's Files list names `internal/tools/git.go` and `git_test.go` only;
`internal/agent/treesnapshot_test.go` also changed — its two argv assertions pin the hardening
options verbatim and had to gain `-c core.fsmonitor=false` (test-only, no production change).
NOTES (2026-08-26): `TestGitDiffRange_DoesNotRunRepoSuppliedDiffDriver` was restructured rather
than left as-is: the widened refusal now refuses its repo-local `diff.hostile.textconv` /
`diff.hostile.command` cases before git runs, so those subtests assert the refusal, and a third
subtest puts the same driver in the OPERATOR's global config to keep `gitDiffHardeningArgs`
(`--no-textconv`/`--no-ext-diff`) pinned — it was the only test covering them.
NOTES (2026-08-26): `gitFilterConfigScopes` keeps its name per the item's "the memo, scopes and
maxNamedFilterDrivers (renamed maxNamedCommandKeys) are unchanged"; only its doc comment was
reworded to the widened rule.

**What:** F-10. Two switches and a widened refusal, by one stated principle: where git offers a
per-invocation switch that neutralises a key, apply the switch; every remaining repo-local key
whose VALUE is a program git executes is refused by name, the way filter drivers are today.
- `internal/tools/git.go:102` `gitHardeningOptions` = `{"-c", "core.hooksPath=", "-c", "core.fsmonitor=false"}`
  (`core.fsmonitor` accepts a command; `false` disables both the hook and the builtin daemon, so a
  repo that set `true` — the everyday builtin-fsmonitor case — is neutralised, not refused).
- `:588` commit argv gains `--no-gpg-sign` after `--no-verify` (a repo-local `commit.gpgsign=true`
  + `gpg.program=<path>` would run that program on commit); doc comment beside `--no-verify`.
- Widen the refusal: `gitFilterConfigName` (`:207`) becomes `gitCommandConfigName`, one regexp
  whose source is POSIX-ERE-compatible (plain `( )` groups, no `(?:` — git's `--get-regexp` compiles
  the same string; this constraint is a comment on the var):
  `^(core\.(sshcommand|editor|pager|askpass|gitproxy|alternaterefscommand)|sequence\.editor|diff\.external|diff\..*\.(command|textconv)|merge\..*\.driver|mergetool\..*\.cmd|difftool\..*\.cmd|filter\..*\.(clean|smudge|process)|credential\.helper|credential\..*\.helper|gpg\.program|gpg\..*\.program|uploadpack\.packobjectshook|remote\..*\.proxy|pager\..*)$`.
  Deliberately absent, each with its reason in the comment: `core.hookspath` and `core.fsmonitor`
  (neutralised by `-c`), `alias.*` (cannot shadow the builtin subcommands the tools invoke).
  `repoLocalFilterDrivers` → `repoLocalCommandConfig`, `probeFilterDrivers` → `probeCommandConfig`,
  `filterDriverProbes` → `commandConfigProbes`, `gitFilterRefusal` → `gitCommandConfigRefusal`
  with the sentence "this repository's own config names a program git would execute (%s%s).
  Repo-local command-valued keys are refused for every git tool; the operator's global git config
  is untouched and still applies." The memo, scopes and `maxNamedFilterDrivers` (renamed
  `maxNamedCommandKeys`) are unchanged. The `gitHardeningEnv` comment (`:105-118`) and the file
  header (`:37-42`) are rewritten to describe the widened rule.

**Files:** `internal/tools/git.go`, `internal/tools/git_test.go`, `CHANGELOG.md`

**Tests:** `TestRunGit_AppliesHardeningToEveryInvocation` expects
`argv: -c core.hooksPath= -c core.fsmonitor=false status`; `TestGitCommit_PassesNoGpgSign` (fake
git records `--no-gpg-sign` beside `--no-verify`); `TestGit_RefusesRepoLocalCommandConfig` — table
over real repositories (`git init` in a temp dir, `git config --local <key> <value>`) for
`core.sshCommand`, `core.editor`, `credential.helper`, `gpg.program`, `diff.foo.textconv`,
`merge.foo.driver`, `pager.status`, `remote.origin.proxy`: `git_status` returns the refusal naming
the key; `TestGit_RepoLocalFsmonitorHookNeverRuns` — `core.fsmonitor` set to a script that writes
a marker file; `git_status` succeeds and the marker is absent; `TestGit_RepoLocalFsmonitorTrueIsNotRefused`
(`core.fsmonitor=true` ⇒ status runs normally); `TestGit_RepoLocalAliasIsNotRefused`. The existing
filter-driver tests (`:1223-1340`) follow the renames and stay green.

**Acceptance:** `go build ./... && go test ./internal/tools/`

**Commit:** `fix(tools): git neutralises core.fsmonitor and gpg signing, refuses every command-valued repo-local key`

---

## 5. Host-side launches keep the resolved path and the fence: settings editor, keystore probe — ✅ DONE (2026-08-26)

NOTES (2026-08-26): both fences are seeded from the RESOLVED root, `stateRoots.workspace`, not from `opts.Workspace` as the item's text spelled it — the option may be empty (meaning the cwd) or relative, and neither is a root an absolute resolved program path can be compared against. `newExternalEdit` therefore takes the root as its own parameter (`newExternalEdit(opts, workspace, getenv)`, passed `w.roots.workspace` at `wire_live.go:232`) and `prepareKeyMigration` probes with `w.roots.workspace` — the same input `internal/present`'s opener is given at `wire_boot.go:96`, so the editor, the store tool and the desktop opener cannot disagree about which bytes the model can write.
NOTES (2026-08-26): the editor's fence refusal wraps with `%w` where the item's text spelled `%v` — the item's own test asks for an error that wraps `security.ErrExecFromWritablePath`, which `%v` would not preserve.
NOTES (2026-08-26): the fence root reaches the probe as a parameter — `probeKeyStore(workspaceRoot string) (secretStore, error)` and `prepareKeyMigration(probe func(string) (secretStore, error), …)` — so the composition-root call site stays `w.prepareKeyMigration(probeKeyStore, os.Stderr)`. The item named only the return type.
NOTES (2026-08-26): required test fallout in `settingsedit_test.go` — `editorAlwaysFound` now answers with an absolute path under `os.TempDir()` (the internal/present opener idiom) and the argv expectations go through a new `resolvedEditorArgv` helper, because `ResolveProgram` refuses a relative resolution and the old name-returning fake would have refused every ladder row. The path-spelled-$EDITOR row moved from `/usr/bin/nvim` to that same directory, which is absolute on Windows too.
NOTES (2026-08-26): `cmd/apogee/configwatch_apply_test.go` and `internal/keystore/live_test.go` are not in the item's Files list but had to be touched — the first constructs an `externalEdit` through `newExternalEdit` and the second calls `keystore.Probe`, so neither package would compile against the new signatures.

Depends on item 1.

**What:** F-07 and F-35 — two programs launched on apogee's own behalf, both resolved by
`LookPath` and then run by BARE NAME (the editor) or unfenced (the keystore tool). Both take
`security.ResolveProgram`.
- `cmd/apogee/settingsedit.go:363-374` `resolveEditor`: `externalEdit` gains `workspace string`,
  seeded in `newExternalEdit` (`:128`) from `opts.Workspace`;
  `resolved, err := security.ResolveProgram(look, cmd.argv[0], e.workspace, nil)`; on success
  `cmd.argv[0] = resolved` (the absolute path is what the pane suspends into — the treatment
  `present/opener.go:228-241` gives the identical problem). Two DIFFERENT errors: a lookup failure
  keeps today's three-ways-to-set-an-editor sentence; a fence refusal (`errors.Is
  ErrExecFromWritablePath`) says "refusing to run editor %q: %v" with the fence's own sentence,
  which names the resolved path — the operator must see which PATH entry points into the
  workspace, not go looking for an install. `editorName` (`:380`) already takes a path.
- `internal/keystore/keystore.go:81-108`: `Probe(workspaceRoot string) (Store, error)` and
  `probe(goos, lookPath, run, workspaceRoot)`. A sentinel `ErrNoStore` replaces the `false`
  answer ("this platform has no store apogee can drive" / "no keyring answered"); after `lookPath`
  the program goes through `security.RefuseExecFromWritablePath(program, workspaceRoot, nil)` and
  a refusal is returned as its own error (wrapping `ErrExecFromWritablePath`, naming the path).
  `internal/keystore` gains its one import, `internal/security` (which imports only `domain`).
- `cmd/apogee/keymigrate.go:39-47` `probeKeyStore` returns `(secretStore, error)`;
  `prepareKeyMigration` renders `ErrNoStore` exactly as today's "no store" notice and renders any
  OTHER error as the notice's sentence verbatim (the refusal reaches the operator instead of
  masquerading as "no keyring here"). The package doc comment on `Probe` says the fence is measured
  against the workspace root because the probe runs at startup, before any box exists.

**Files:** `cmd/apogee/settingsedit.go`, `cmd/apogee/settingsedit_test.go`,
`internal/keystore/keystore.go`, `internal/keystore/keystore_test.go`, `cmd/apogee/keymigrate.go`,
`cmd/apogee/keymigrate_test.go`, `CHANGELOG.md`

**Tests:** `settingsedit_test.go` — `TestExternalEditSpecCarriesTheResolvedEditorPath` (`look`
returns `/opt/bin/vim`; the spec's argv[0] is that path); `TestExternalEditSpecRefusesAnEditorInsideTheWorkspace`
(`look` returns a path under `opts.Workspace`; the error names it and wraps
`ErrExecFromWritablePath`); `TestExternalEditSpecRefusesAnEditorThisMachineCannotRun` keeps its
sentence. `keystore_test.go` — the two `probe(...)` call sites (`:451`, `:516`) pass a temp
workspace; `TestProbeRefusesAStoreProgramInsideTheWorkspace` (fake `secret-tool`/`security` planted
under the workspace and first on PATH ⇒ error wraps `ErrExecFromWritablePath`, no Store);
`TestProbeReportsWhatThePlatformCanDo` asserts `ErrNoStore` where it asserted `false`.
`keymigrate_test.go` — a probe error that is not `ErrNoStore` lands in the notice text.

**Acceptance:** `go build ./... && go test ./internal/keystore/ ./cmd/apogee/`

**Commit:** `fix(cmd,keystore): the settings editor and the keystore probe launch a fenced absolute path`

---

## 6. The process-tree teardown seam moves to `internal/platform` — ✅ DONE (2026-08-26)

NOTES (2026-08-26): `TestTeardownDoesNotReachASetsidEscapee` moved to `internal/platform` as the item says, but it could not keep driving `runSubprocess` there (`platform` cannot import `tools`), so it now drives the seam directly — `NewProcessTeardown` + `RunWithTeardown` around one `/bin/sh -c` — and carries its own POSIX PID helpers instead of `tools`' `waitForPIDFile`/`pidAlive`/`killPID`. What it pins is unchanged: the escapee detached itself and survived both the clean-exit reap and `cmd.Cancel`.
NOTES (2026-08-26): `ProcessWaitDelay` was declared twice before the move (an identical `5 * time.Second` in each of the two build-tagged files); the item lists it under `teardown.go`, so it is now declared once there and both backends read it.
NOTES (2026-08-26): two files outside the item's list named what moved and are updated by it — `internal/tools/doc.go` (its file map described the three moved files; it now points at the platform facility and keeps only the `newProcessTeardown` seam) and `internal/mechanisms/autofix.go:70` (a comment naming "processWaitDelay (internal/tools)" → `platform.ProcessWaitDelay`).
NOTES (2026-08-26): the `ISSUES.md` job-object-breakaway bullet's two line references were re-pointed to the moved files' current lines (`teardown_unix.go:62`, `teardown_windows.go:23`), not only to the new paths.

**What:** call 9 — a pure move with exported names, so the next spawner (item 7) reuses the
§2.4 teardown instead of copying it. Behaviour is byte-identical; `git mv` keeps each file's
history.
- `internal/tools/exec_teardown.go` → `internal/platform/teardown.go`:
  `type ProcessTeardown interface { Contain(*exec.Cmd); Reap(*exec.Cmd); Release() }`,
  `func NewProcessTeardown(cmd *exec.Cmd) ProcessTeardown` (the per-OS constructor; wires
  `cmd.Cancel` and `cmd.WaitDelay` as `setProcessGroupTeardown` does today),
  `func RunWithTeardown(cmd *exec.Cmd, td ProcessTeardown) error`, `var ProcessWaitDelay`
  (the test-shrinkable drain bound), `type NoTeardown struct{}` (exported: `tools` tests fake the
  seam with it). `planTreeKill`, `treeKillAction`, `killProcessGroup`, `pgroupTeardown`,
  `jobTeardown`, `newTreeJob`, `setJobLimits` stay unexported inside `platform`.
- `internal/tools/exec_pgroup_unix.go` → `internal/platform/teardown_unix.go`;
  `internal/tools/exec_pgroup_other.go` (build tag `windows`) → `internal/platform/teardown_windows.go`.
  Their tests move beside them: `exec_teardown_test.go` → `platform/teardown_test.go`
  (`TestPlanTreeKill`, `TestNoTeardownIsInert`), `exec_teardown_unix_test.go` →
  `platform/teardown_unix_test.go` (`TestTeardownDoesNotReachASetsidEscapee`).
  `TestRunSubprocessReleasesTheTeardownOnEveryExitPath` (`exec_teardown_test.go:123`) stays in
  `tools` (it drives `runSubprocess`) and fakes the seam through the exported interface.
- `internal/tools/exec_common.go`: `var newProcessTeardown = platform.NewProcessTeardown` keeps
  the package's test seam; `runWithTeardown` → `platform.RunWithTeardown`; `processWaitDelay`
  readers → `platform.ProcessWaitDelay` (tests that shrink it, `grep -n processWaitDelay
  internal/tools`, follow). The `runSubprocess` comment at `:343-349` is unchanged in substance.
- `internal/platform/doc.go:42-60`: the file map gains the teardown group ("twenty-five files, in
  five groups") with one line per file, and the sentence that the teardown is a process-lifecycle
  facility, never a fence (ADR 0020), moves here from the old file headers.
- `docs/design/confinement-execution-contract.md` §2.4 and `ISSUES.md:627-628`: the two file
  references (`exec_pgroup_unix.go`, `exec_pgroup_other.go`) become the new paths.

**Files:** `internal/platform/teardown.go`, `internal/platform/teardown_unix.go`,
`internal/platform/teardown_windows.go`, `internal/platform/teardown_test.go`,
`internal/platform/teardown_unix_test.go`, `internal/platform/doc.go`,
`internal/tools/exec_common.go`, `internal/tools/exec_common_test.go`,
`internal/tools/exec_teardown_test.go`, `docs/design/confinement-execution-contract.md`, `ISSUES.md`
(the three old `internal/tools/exec_teardown.go`, `exec_pgroup_unix.go`, `exec_pgroup_other.go`
and the two moved test files are deleted by the move)

**Tests:** the moved tests pass under their new package unchanged in substance;
`TestRunSubprocessReleasesTheTeardownOnEveryExitPath` and `TestRunSubprocessReapsTheProcessGroupOnACleanExit`
pass against the exported seam; `GOOS=windows go build ./...` and `GOOS=windows go vet ./internal/platform/`
compile the Windows half.

**Acceptance:** `go build ./... && GOOS=windows go build ./... && GOOS=windows go vet ./internal/platform/ ./internal/tools/ && go test ./internal/platform/ ./internal/tools/`

**Commit:** `refactor(platform): the process-tree teardown seam is a platform facility, exported for every spawner`

---

## 7. Stdio MCP servers: fenced absolute program, process-group teardown — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the item spells `cmd := exec.Command(program, cfg.Args...)`, which does not work:
`platform.NewProcessTeardown` wires `cmd.Cancel`, and `exec.Cmd.Start` refuses a non-nil `Cancel` on a
Cmd built without a context (`os/exec/exec.go:710`) — every stdio server would have failed to start.
The Cmd is therefore built with `exec.CommandContext(context.Background(), …)`. `Background` and not
the connect ctx deliberately: a stdio server's lifetime is the SESSION, and binding it to the sweep
that dialled it would kill every server the moment `Connect` returned. `Background.Done()` is nil, so
os/exec starts no watchdog and the wired `Cancel`/`WaitDelay` stay inert; teardown is driven
explicitly by `Close`.
NOTES (2026-08-26): the fence refusal wraps with `%w` where the item's text spelled `%v` — the item's
own test asks for an error that wraps `security.ErrExecFromWritablePath`, which `%v` would not
preserve (the same correction item 5 recorded).
NOTES (2026-08-26): the fence root passed at `wire_live.go:41,51` is `w.roots.workspace`, not
`w.opts.Workspace` as the item's text spelled it — the option may be empty (meaning the cwd) or
relative, and neither is a root an absolute resolved program path can be compared against. This is
the same input `registryWithMCP` is already given two lines below, so the tool registry and the MCP
launch cannot disagree about which bytes the model can write (item 5 recorded the identical
correction for the editor and the keystore probe).
NOTES (2026-08-26): the item's `TestClose_ReapsTheStdioServersDescendants` says the fixture starts
`sleep 60` "in its own group". Implemented as a plain child inheriting the SERVER'S group: a
descendant that made a group of its own (setsid) is precisely the documented ESCAPE from
`platform.NewProcessTeardown`, so the literal reading would have pinned the residual instead of the
reap. Verified as a real test by a negative control — with `td.Reap` removed it fails
("the descendant outlived Close") and passes with it.
NOTES (2026-08-26): `connectOne` reaps `(cmd, td)` when `client.Connect` FAILS. The item spells the
teardown order for `Close` and says "rollback in Connect runs the same path", but a handshake that
failed never yields a session to record, so `Close`'s rollback cannot reach that process or the
teardown's own Job Object handle; the shared `reapProcess` helper is called on that path too.
NOTES (2026-08-26): `internal/mcp/doc.go` is not in the item's Files list but had to be touched — its
trust-boundary bullet states the stdio trust model ("the host chose the command, so no URL check
applies"), which is now only half the story; one sentence names the exec fence and the reaped process
tree, matching the design-doc wording the item does prescribe.
NOTES (2026-08-26): `docs/design/mcp-client.md` §3's lifecycle signature line and its "no orphaned
stdio process leaks" bullet were updated alongside the §2 stdio bullet the item names — `Connect`
gained a parameter, so leaving §3 spelling the old three-argument form would have made the doc wrong
in the same edit that made §2 right.
NOTES (2026-08-26): `cmd/apogee/wire_test.go` is in the item's Files list but needed no change —
`wireSession`'s own signature is unchanged and the workspace is threaded from `w.roots.workspace`
inside it, so both tests that drive it (`wire_test.go:184`, `title_test.go:596`) compile and pass
untouched.

Depends on items 1 and 6.

**What:** F-36 and F-42.
- `internal/mcp/client.go:58` `Connect(ctx, servers, guard, workspaceRoot string)`;
  `connectOne` and `buildTransport` carry `workspaceRoot`. `cmd/apogee/wire_live.go:41,51` pass
  `w.opts.Workspace`.
- `internal/mcp/transport.go:114-123` `buildStdioTransport(cfg, workspaceRoot)`:
  `program, err := security.ResolveProgram(nil, cfg.Command, workspaceRoot, nil)` — an absent
  program or a fence refusal is a connect-time error `mcp: stdio server %q: %v` (Connect is
  all-or-nothing, so the operator sees it at startup, as today's misconfigurations are seen);
  `cmd := exec.Command(program, cfg.Args...)`; `cmd.Dir` is NOT set — the server keeps starting in
  apogee's cwd, the workspace (call 10: with the program a fenced absolute path the
  relative-lookup concern is closed, and filesystem-style servers expect the workspace); env
  unchanged (the TRUST NOTE stays, gaining one sentence: the program is resolved on PATH through
  the exec fence, so a server binary inside the workspace is refused at connect time).
  `td := platform.NewProcessTeardown(cmd)` before the transport is built (POSIX: `Setpgid`, the
  fork-time group; Windows: the Job Object). The function returns the SDK transport plus the
  `(cmd, td)` pair.
- `client.go:38` `sessions []*mcpsdk.ClientSession` → `sessions []liveSession` with
  `{session *mcpsdk.ClientSession; cmd *exec.Cmd; td platform.ProcessTeardown}` (nil cmd/td for the
  HTTP transports). `connectOne` calls `td.Contain(cmd)` right after `client.Connect` returns (the
  process exists from then; the documented sub-millisecond Windows window applies, same as the
  tools funnel). `Close()` (`:149`): per session, `session.Close()` first (the SDK's spec-shaped
  shutdown — stdin close, wait, SIGTERM, SIGKILL of the LEADER), then `td.Reap(cmd)` (the group /
  job — every descendant the server spawned) and `td.Release()`; rollback in `Connect` runs the
  same path. Doc comment on `Close` states the order and why.
- `docs/design/mcp-client.md:46` stdio bullet: the command is resolved on PATH through the exec
  fence (a program inside the workspace is refused), it starts in the workspace as before, and it
  is held in a process group / Job Object reaped at `Close`.

**Files:** `internal/mcp/client.go`, `internal/mcp/transport.go`, `internal/mcp/mcp_test.go`,
`internal/mcp/transport_test.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_test.go`,
`docs/design/mcp-client.md`, `CHANGELOG.md`

**Tests:** `mcp_test.go` — the fixture (`stdioServerConfig`, the fork-and-exec test binary) passes
`t.TempDir()` as the workspace; `TestConnect_RefusesAStdioServerInsideTheWorkspace` (copy the test
binary under the workspace root, name it as `Command` ⇒ `Connect` errors naming the path and
wrapping `ErrExecFromWritablePath`); `TestClose_ReapsTheStdioServersDescendants` (POSIX only: the fixture server, on a `spawn` tool
call, starts `sleep 60` in its own group and returns the pid; after `Close` the pid is gone —
`syscall.Kill(pid, 0)` answers `ESRCH` within 2 s); `TestClose_TearsDownSessions` stays green.
`transport_test.go` — `TestBuildTransportHTTPKinds` passes the new argument. `wire_test.go` —
whichever test drives `wireSession` passes with the workspace threaded through.

**Acceptance:** `go build ./... && GOOS=windows go build ./... && go test ./internal/mcp/ ./cmd/apogee/`

**Commit:** `fix(mcp): a stdio server is a fenced absolute program reaped as a process tree`

---

## 8. A workspace-scoped writer's git child gets the box in the confining cell — ✅ DONE (2026-08-26)

NOTES (2026-08-26): the plan's dispatch test asks for "a fake `workspaceScopedWriter` tool"; the marker's method is unexported by design (contract §3.2), so no fake in `internal/agent` can carry it — `TestDisposition_WorkspaceWriteChildGetsTheBox` drives the REAL `delete_file` and proves the handle through the recording Confiner (Confine is reached only through the ctx handle) in three cells: Auto·confine (confined, box = the workspace), Allow-Edits and Auto·no-confine (never confined).
NOTES (2026-08-26): `TestStageGitPaths_UnconfinableChildIsANote` needs a Confiner that refuses the box for the staging `add` ALONE — a Confiner refusing every call fails at the trackedness probe first, which is the earlier SILENT skip, not the note. Both paths are asserted, as two subtests.

**What:** F-03, per call 8. The Run verdict of `move_file`/`delete_file` (and every other
`workspaceScopedWriter`) in the cell where a subprocess would be Confined carries the box, and the
executor installs the confinement handle for it, so `stageGitPaths`' git child confines exactly as
a `git_status` call's would; nothing about classification, gating or the lower modes changes.
- `internal/agent/resolution.go`: `resolution` gains `confineChildren bool` — doc: "a Run whose
  in-process tool may spawn a subprocess of apogee's own (the workspace-scoped writers' git
  staging, `tools/git_stage.go`) executes with the box installed, so that child confines as a git
  tool's would; the demote logic (D4) stays Confine-only — an unconfinable child here is the
  tool's own best-effort skip, never a demote, because the file operation has already happened".
  The `box` field doc (`:149-150`) becomes "Confine, and a Run with confineChildren". In
  `applyOverlays` (`:433`) default branch: when `classifyTool(in.tool) == classWorkspaceWrite &&
  in.mode == domain.ModeAuto && in.confineToWorkspace && in.fsConfineAvailable`, set
  `leaf.box = in.box; leaf.confineChildren = true`. Binding reasoning, written as the comment: in
  Allow-Edits and the gate cell the child's bound is its hardened argv (item 4) — apogee's own
  fixed `git add -A -- :(literal)<path>`, not a model-chosen program — which is the blast radius
  `classWorkspaceWrite` already declares; ADR 0012 D5's "no Confine in the lower modes" is kept.
- `internal/agent/dispatch.go:498` `executeRun`: when `verdict.confineChildren`, wrap the ctx
  with `domain.WithConfinement(ctx, domain.Confinement{Confiner: a.cfg.Confiner, Box: verdict.box})`
  before `executeTool`; the `box` parameter stays nil (the D4 translation at `:832-836` keys on
  it and must not fire). One sentence in `executeRun`'s doc.
- `internal/tools/git_stage.go:20-30` doc: a paragraph on the box — in Auto with confinement the
  handle rides the call and `runGit` confines the child; `ErrConfinementUnavailable` is a
  "(git staging skipped: …)" note (already the code's behaviour at `:73-79`); elsewhere the child's
  bound is the hardened argv.
- `docs/design/confinement-execution-contract.md` §3.3 ("Who carries it"): a dated paragraph
  stating the rule above.

**Files:** `internal/agent/resolution.go`, `internal/agent/resolution_test.go`,
`internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`, `internal/tools/git_stage.go`,
`internal/tools/git_stage_test.go`, `docs/design/confinement-execution-contract.md`, `CHANGELOG.md`

**Tests:** `resolution_test.go` — `TestResolve_WorkspaceWriteRunCarriesTheBoxInTheConfiningCell`:
`write_file` (the `wsw` fixture at `:44`) in Auto · confine · caps ⇒ Run with `confineChildren`
and `box == in.box`; Allow-Edits ⇒ Run without; Auto with `fsConfineAvailable=false` ⇒ without;
Auto with `confineToWorkspace=false` ⇒ without; an out-of-workspace target still Gates without.
`dispatch_test.go` — a fake `workspaceScopedWriter` tool that records
`domain.ConfinementFromContext(ctx)`: in the confining cell the handle is present and carries
the Agent's Confiner; in Allow-Edits it is absent. `git_stage_test.go` —
`TestStageGitPaths_ConfinesTheGitChild` (ctx with a recording fake Confiner: `Confine` is called
for the `ls-files` probe and the `add`); `TestStageGitPaths_UnconfinableChildIsANote` (Confiner
returns `ErrConfinementUnavailable` ⇒ result carries "(git staging skipped: …)", no Go error).

**Acceptance:** `go build ./... && go test ./internal/agent/ ./internal/tools/`

**Commit:** `fix(agent): a workspace write's git child runs inside the box where Auto would confine it`

---

## 9. Landlock ABI 1–2: `truncate(2)` is disclosed as a residual, in the caps, the notices, the contract and the battery

**What:** C-06, per call 1.
- `internal/domain/confinement.go:54` `ConfinementCaps` gains `Residuals []string` — doc:
  "write-class accesses this backend knowingly cannot fence on this host while `FSWrite` is true,
  each named by its syscall; empty when the fence is complete. Capability honesty (contract §5):
  a backend that leaves an access open says so rather than reporting a fence it does not have."
  `AutoEligible` is unchanged (`FSWrite` only).
- `internal/platform/landlock_linux.go:183` `Capabilities`: when `landlockABIFSWrite <= abi <
  landlockABITruncate`, `Residuals: []string{"truncate(2)"}`. `accessMaskForABI` (`:120`) is
  unchanged (it already carries `TRUNCATE` unconditionally from ABI 3); the ABI-1 bullet in its
  comment gains "disclosed through Capabilities().Residuals".
- `internal/probe/confinement.go`: `CapabilityLine` (`:44`) appends ` · unfenced: <a, b>` when
  `Residuals` is non-empty (so `/confine status`, `apogee probe` and the boot line all carry it —
  `internal/tui/confine.go:165` reads this one function). Add
  `func ResidualNotice(backendName string, caps domain.ConfinementCaps, mode domain.Mode, confineToWorkspace bool) string`
  — "" unless `mode == Auto && confineToWorkspace && caps.FSWrite && len(caps.Residuals) > 0`;
  otherwise: `apogee: auto mode confines terminal commands, but the %s backend on this kernel
  cannot fence %s —\n  a confined command can still empty an existing file outside the workspace
  (landlock ABI 1–2, kernel < 6.2).\n  A kernel ≥ 6.2 closes it; until then treat auto's fence as
  create-and-write only.` `DegradedNotice` (`:70`) is UNCHANGED — it is the gate `headless.go:290`
  and `daemon.go:548` refuse Auto on, and a residual is not a refusal. `ResidualNotice` is printed
  beside it at the one announce site `cmd/apogee/wire_options.go:253` (TUI boot), and by
  `headless.go`/`daemon.go` as a stderr line where they print their own confinement line — never
  as a blocker.
- `internal/platform/confinetest/confinetest.go` + `lines_other.go`: battery row **#12**
  `truncate_outside_box`: create `<sibling-temp>/truncate.txt` with content from the PARENT, then
  the confined child runs the shell line `truncate -s 0 <path>` (coreutils; the row skips when
  `truncate` is not on PATH — macOS — and under `cmd.exe`). Assertion keyed on the backend's own
  disclosure: `Residuals` empty ⇒ **denied** and the file keeps its bytes; `Residuals` names
  `truncate(2)` ⇒ the truncate **succeeds** and the file is empty — the behaviour and the
  disclosure are asserted together, so neither can drift silently.
- `docs/design/confinement-execution-contract.md` §5 Linux bullet: "ABI 1–2 ⇒ `FSWrite = true`
  with `Residuals = [truncate(2)]` (the ruleset cannot handle `LANDLOCK_ACCESS_FS_TRUNCATE` before
  ABI 3)"; §6.2 gains row #12 and a dated note (C-06, live-reproduced 2026-08-25).
- `docs/manual/probe.md:21` example line gains the `unfenced:` form in a following sentence;
  `docs/manual/configuration.md` §"Auto mode's blast radius" (`:613`) gains one sentence on the
  ABI 1–2 residual and the kernel that closes it.

**Files:** `internal/domain/confinement.go`, `internal/platform/landlock_linux.go`,
`internal/platform/landlock_linux_test.go`, `internal/probe/confinement.go`,
`internal/probe/confinement_test.go`, `internal/platform/confinetest/confinetest.go`,
`internal/platform/confinetest/lines_other.go`, `internal/platform/confinetest/lines_windows.go`,
`cmd/apogee/wire_options.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemon.go`,
`docs/design/confinement-execution-contract.md`, `docs/manual/probe.md`,
`docs/manual/configuration.md`, `CHANGELOG.md`

**Tests:** `landlock_linux_test.go` `TestLandlockCapabilitiesHonest` gains `wantResiduals`: ABI 1
and 2 ⇒ `[truncate(2)]`, ABI ≥ 3 and no-landlock ⇒ empty; `TestAccessMaskForABI` pins TRUNCATE
present at 3, absent at 2 (already the shape; add the explicit row). `confinement_test.go` —
`TestCapabilityLine` gains the residual row; `TestResidualNotice` table: fires only in the one
cell, names the backend and the residual, is "" whenever `DegradedNotice` would fire (the two are
mutually exclusive by construction). The battery drivers (landlock, seatbelt, windows) pick up row
#12 through `Probe`; the landlock driver asserts the row's branch matches the host ABI it runs on.
`cmd/apogee` — the boot announce test that pins `DegradedNotice` gains a residual case.

**Acceptance:** `go build ./... && GOOS=windows go build ./... && go test ./internal/domain/ ./internal/platform/... ./internal/probe/ ./internal/tui/ ./cmd/apogee/`

**Commit:** `fix(platform): landlock ABI 1–2 discloses truncate(2) as a residual in caps, notices, contract and battery`

---

## 10. Dangerous-action guard: everyday `rm -rf` and pipe-to-shell spellings are covered

**What:** C-10, per call 2. The rules are data (`internal/security/rules.go`); the shape change is
three shared fragments plus one alternation, and the stance stays: a footgun-guard, not a boundary.
- `rules.go`: add the consts `rmFlag = (?:--?[a-z][a-z-]*)` (one short or long flag token; `--`
  alone is NOT a flag), `rmRecursive = (?:-[a-z]*r[a-z]*|--recursive)`,
  `rmForce = (?:-[a-z]*f[a-z]*|--force)`, `rmEndOfOptions = (?:--\s+)?`, `quoteOpen = ["']?`.
  `rm-rf-root-home-system` (`:54`) Pattern becomes
  `\brm\s+(?:` + rmFlag + `\s+)*(?:-[a-z]*r[a-z]*f[a-z]*|` + rmRecursive + `\s+(?:` + rmFlag + `\s+)*` + rmForce + `)\s+(?:` + rmFlag + `\s+)*` + rmEndOfOptions + quoteOpen + deleteTargetAnchor;
  `rm-fr-root-home-system` (`:61`) is the mirror with `f…r` and `rmForce … rmRecursive`. Covered
  by construction (and pinned below): `rm -rf -- /`, `rm --recursive --force /`, `rm -r -f /var`,
  `rm -f -r /var`, `rm -rf "/etc"`, `rm -rf '/'`, `rm -rf -- "$HOME"`, `rm -v --recursive -f /boot`.
  Still out (the precision boundary, unchanged): every relative target, quoted or not.
- `remote-pipe-to-shell` (`:155`) Pattern becomes
  `\b(?:curl|wget|fetch)\b[^|]*\|\s*(?:sudo\s+)?(?:/[a-z0-9_./-]*/)?(?:ba|z|d|fi|k|a)?sh\b` — an
  optional absolute directory before the shell name (`| /bin/bash`, `| /usr/bin/sh`, `| sudo
  /bin/dash`, `| /usr/local/bin/zsh`); `\b` after `sh` keeps `shellcheck` out.
- The comment block above the rules (`:1-40`) gains the sentence: "Everyday idiom is covered —
  end-of-options `--`, long flags, a quoted absolute target, an absolute shell path after the
  pipe; deliberate obfuscation (`eval`, variable expansion, `$'…'`, base64) is not, and that is the
  boundary `doc.go` states."
- `ISSUES.md:525-527`: the L2 parenthetical is REWRITTEN to:
  "(L2 — the dangerous-action guard normalises only whitespace, case and `\`→`/` and is evaded by
  deliberate OBFUSCATION — `eval`, variable expansion, `$'…'` quoting, encoded payloads — needs no
  entry: it is ADR-0012 by-design, and `internal/security/doc.go` states the guard is "NOT a
  security boundary." Everyday idiom — `--`, long flags, a quoted absolute target, an absolute
  shell path after a pipe — IS covered (2026-08-26, code audit C-10); what stays out of scope is
  the obfuscation chase, not the ordinary spelling.)"

**Files:** `internal/security/rules.go`, `internal/security/dangerous_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** `dangerous_test.go` — `TestDangerousActionGuard_Tier1HardRefuse` (`:30`) gains the
eight spellings listed above plus `rm -rf -- /home/alice` and `rm -rf "/Users/alice"`;
`TestDangerousActionGuard_PrecisionNearMissesNotBlocked` (`:95`) gains `rm -rf -- ./build`,
`rm -rf "./build"`, `rm --recursive --force node_modules`, `rm -rf -- node_modules`,
`rm -rf build/`; `TestDangerousActionGuard_Tier2ForceApproval` (`:70`) gains `curl x | /bin/bash`,
`wget -qO- x | /usr/bin/sh`, `curl x | sudo /bin/dash`, `fetch x | /usr/local/bin/zsh`; near-misses
gain `curl x | /usr/bin/grep foo` and `curl x | shellcheck -`. `TestDangerousActionGuard_WhitespaceNormalized`
and `TestDangerousActionGuard_HardRefuseBeatsForceApproval` stay green.

**Acceptance:** `go build ./... && go test ./internal/security/`

**Commit:** `fix(security): the guard's rm -rf and pipe-to-shell rules cover --, long flags, quoted targets and absolute shells`

---

## 11. An approved Tier-2 forced gate keeps the Confine box

Depends on item 8.

**What:** call 4. A forced look on a call Auto would have confined executes, once allowed, as the
Confine would have — box and D4 fallback included. Today (`resolution.go:436`) the upgrade builds a
bare Gate and `executeGate` runs the allow unconfined; `resolution_test.go:470` pins that as
"(unconfined, no fallback)".
- `internal/agent/resolution.go`: `resolution` gains `confineOnAllow bool` — doc: "Gate only: the
  allow-continuation executes as a Confine — approval decides WHETHER the call runs, confinement
  WHERE; the guard is tighten-only (ADR 0012), so a forced look never loosens the fence the ladder
  chose". `applyOverlays` (`:433-437`): when the guard forces and `leaf.kind == resolveConfine`,
  the upgraded gate is `resolution{kind: resolveGate, force: true, reason: forceApprovalReason,
  box: leaf.box, confineOnAllow: true, fallback: confineFallback(in)}`; for a Run or Gate leaf the
  upgrade is unchanged. `finishGate` (`:468`) preserves `box`, `confineOnAllow` and `fallback`
  (its nil-Approver branch builds a fresh Refuse — that stays). The `box` field doc adds "and a
  forced Gate upgraded from a Confine"; the `fallback` doc drops "Confine only".
- `internal/agent/dispatch.go:511` `executeGate`: on allow, `if verdict.confineOnAllow { return
  a.executeConfine(ctx, turn, tool, call, verdict) }` — the box is installed, and a run-time
  `ErrConfinementUnavailable` follows the verdict's D4 fallback exactly as a plain Confine does
  (the human is asked a second time, by the demote gate, whether to run UNCONFINED — two prompts
  in the rare failure case is the honest shape). Doc comment: "runs it unconfined" → "runs it —
  confined when the leaf it upgraded was a Confine".
- `internal/agent/resolution.go:10-30` header and `applyOverlays` doc: the D4 sentence gains "a
  Tier-2 force keeps a Confine leaf's box".
- `docs/design/confinement-execution-contract.md` §4 item 1 (`:499-500`): append "A Confine leaf
  upgraded this way KEEPS its box and its D4 fallback: the allow executes as the Confine would
  have (amendment 2026-08-26)". `CONTEXT.md:771` **Dangerous-action guard**: after "forcing the
  Approver even in Auto" add "; a forced look on a call Auto would have confined stays confined
  once allowed — approval decides whether, confinement where". ADR 0012 and ADR 0049 were grepped
  for a contrary statement (`unconfined`, `forced`): neither says a forced gate runs unconfined, so
  no ADR edit.

**Files:** `internal/agent/resolution.go`, `internal/agent/resolution_test.go`,
`internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`,
`docs/design/confinement-execution-contract.md`, `CONTEXT.md`, `CHANGELOG.md`

**Tests:** `resolution_test.go` — the `:470` subtest is renamed "Confine leaf upgrades to a
forced gate that keeps its box (confined on allow, demote fallback carried)" and asserts
`confineOnAllow`, `box == in.box`, and a non-nil `fallback` that is a forced gate with
`confineDemoteGateReason` (same shape `TestResolve_ConfineFallbackShape` pins); the Run and Gate
leaf subtests assert `confineOnAllow == false` and a nil fallback. `dispatch_test.go` —
`TestDispatch_ApprovedForcedGateRunsConfined`: a `subprocTool` in Auto · confine · caps, a Tier-2
guard, an approving Approver, a recording fake Confiner ⇒ the tool saw a confinement handle and
`Confine` was called once; `TestDispatch_ApprovedForcedGateFallsBackOnUnconfinableBox`: the
Confiner returns `ErrConfinementUnavailable` ⇒ the Approver is consulted a second time with
`confineDemoteGateReason`, and on allow the tool runs without a handle; a denied first prompt
never reaches the Confiner.

**Acceptance:** `go build ./... && go test ./internal/agent/`

**Commit:** `fix(agent): an approved Tier-2 forced gate keeps the Confine box — approval decides whether, confinement where`

---

## 12. `~/.apogee` writes are a Tier-2 forced look (code follows ADR 0049 §4); the Hint rides the prompt

Depends on item 11.

**What:** call 3 and call 11.
- `internal/security/rules.go:131` `write-apogee-control-plane`: `Tier: TierForceApproval`;
  Reason, Hint, Pattern, WritesOnly unchanged. The comment block above it (`:115-130`) is rewritten:
  ADR 0049 §4 — the floor here is a forced LOOK, never a boundary; the human's informed yes runs
  the write, `~/.apogee` included; the Hint is now the prompt's remedy line and the deny result's
  tail (below), which is where a small model reads it. The tier sentence in the file header
  (`:16-17`) gains `~/.apogee` as a Tier-2 example; `internal/security/doc.go:33-35` names no
  rule and needs no change.
- `internal/agent/resolution.go` `applyOverlays` (item 11's forced-gate construction): the forced
  gate carries `remedy: in.guard.Hint` (empty for rules without one — today's prompt) and a new
  `hint string` field (Gate only; the guard's model-facing way out). `finishGate` preserves both.
- `internal/agent/dispatch.go:511-525` `executeGate`: the deny result for a gate carrying `hint`
  reads `tool call denied by approver — <hint>`; without a hint the sentence is unchanged.
  `guardRefusalMessage` (`:611`) is untouched (Tier-1 rules may still carry a Hint).
- `docs/adr/0049-…-permit-pinned-to-the-disclosed-target.md` §4 parenthetical "(`.git/`,
  `~/.apogee`)": narrow to `~/.apogee` and add a dated note (2026-08-26): the `.git/hooks|config|
  modules` rule stays Tier-1 — a write there is delayed code execution outside every confinement,
  the shell-rc class — and only `~/.apogee` is the Tier-2 forced look this decision describes;
  the code was reconciled to it on this date (security audit lead, §3.5).

**Files:** `internal/security/rules.go`, `internal/security/doc.go`,
`internal/security/dangerous_test.go`, `internal/agent/resolution.go`,
`internal/agent/resolution_test.go`, `internal/agent/dispatch.go`, `internal/agent/dispatch_test.go`,
`docs/adr/0049-an-approved-write-escape-executes-through-a-permit-pinned-to-the-disclosed-target.md`,
`CHANGELOG.md`

**Tests:** `dangerous_test.go` — every assertion that a `~/.apogee` write is `TierHardRefuse`
(`TestWritesOnlyRulesJudgeTheWriteTargetNotADeclaredReadSource` `:412-440`'s `evil/SKILL.md`
destination, and any other — `grep -n apogee dangerous_test.go`) becomes `TierForceApproval` with
the Hint still on the Decision; `TestDangerousActionGuard_Tier2ForceApproval` gains a
`write_file` to `~/.apogee/config.yaml` and a `terminal` line `echo x > ~/.apogee/config.yaml`;
`TestWritesOnlyRulesSkipADeclaredReadOnlyTool` (`:360`) stays green (WritesOnly kept).
`resolution_test.go` — the forced gate's `remedy` and `hint` equal the guard's Hint; a guard
without a Hint yields an empty remedy. `dispatch_test.go` — a denied forced gate with a hint
returns the appended sentence; without one, today's sentence.

**Acceptance:** `go build ./... && go test ./internal/security/ ./internal/agent/`

**Commit:** `fix(security): the ~/.apogee write rule is a Tier-2 forced look per ADR 0049, its hint on the prompt`

---

## 13. PDF extraction is bounded: `/Count` is a hint, `/Size` is checked, cycles and output have budgets

**What:** C-07, F-25, F-26, per call 12. The parser cannot be trusted with the document's own
numbers; every allocation and every walk it drives is bounded from outside it.
- `internal/doctext/pdf.go`: `ExtractPDF(ctx context.Context, data []byte, maxTextBytes int) (text string, pages int, failMessage string)`
  (`maxTextBytes <= 0` = unbounded). Constants with doc comments: `pdfMaxPages = 2000`,
  `pdfPhantomRun = 25`, `pdfMaxReads = 200_000`.
  1. `/Size` guard, BEFORE `pdf.NewReader` (`:85`): scan `data` with `regexp` `/Size\s+(\d+)` over
     every occurrence (the trailer AND xref-stream dictionaries — `read.go:233,392` both
     `make([]xref, size)` from it, ~40 bytes per entry, before reading anything); any value greater
     than `len(data)` ⇒ `fmt.Sprintf(pdfUnreadableFormat, "declares N objects in M bytes")` — a
     real object costs more than one byte, so the bound refuses only impossible documents.
  2. A `budgetedReaderAt` wrapping `bytes.NewReader(data)`: every `ReadAt` checks `ctx.Err()` and
     a call counter; past `pdfMaxReads` or on a done ctx it returns an error. The library keeps no
     value cache (`read.go:55`), so every reference resolution in `Page()`'s `Kids` walk
     (`page.go:29-52` — a `Pages` kid referencing itself loops forever today) reads through it;
     the error surfaces as the parser's own error or panic, which the existing `recover` turns
     into `pdfUnreadableFormat` with the cause "extraction budget exhausted: the document's object
     graph does not terminate" (ctx: "cancelled").
  3. The walk (`:90-115`): `declared := reader.NumPage()`; `walk := min(declared, pdfMaxPages)`;
     `blocks := make([]string, 0, min(walk, 64))`; per page check `ctx.Err()`; count consecutive
     pages with `pageErr == nil && strings.TrimSpace(pageText) == ""` and stop when the run
     reaches `pdfPhantomRun`, dropping that run's blocks; stop after the page that pushes the
     accumulated text past `maxTextBytes`. When the walk stopped before `declared`, append one
     final block `[Pages N+1–M not extracted: <page cap|content budget|no text on 25 consecutive
     pages>]` (N = last kept page, M = declared). `pages` is N; `PDFAnnotation` is unchanged.
  4. `pdfNoTextMessage` is returned only when no kept page had text (unchanged rule).
- `internal/tools/read_file.go:139`: `doctext.ExtractPDF(ctx, content, maxFileReadBytes)` — the
  same ceiling `path_read.go:49-53` bounds the raw read by; `readableText` (`:134`) gains `ctx`,
  passed from `Execute` (`:104`).
- `internal/agent/loop.go:999-1027` `resolveFileRefs` gains `ctx context.Context` (its two
  callers, `loop.go:98` and `interject.go:63`, thread the step's ctx) and calls
  `doctext.ExtractPDF(ctx, data, 2*int(float64(bound)*a.budget().CharsPerToken))` — twice the
  clamp's char budget, so head and tail of the elision are real content; the doc comment says
  extraction is bounded by the clamp budget, not by the raw 10 MiB read (`:963-966` note).
- `pdf.go:16-20` header: the contract sentence ("a tool that crashes the agent on a corrupt
  download is not an option") gains "and neither is one that lets a document size the agent's
  memory or its walk".

**Files:** `internal/doctext/pdf.go`, `internal/doctext/pdf_test.go`,
`internal/tools/read_file.go`, `internal/tools/read_file_test.go`, `internal/agent/loop.go`,
`internal/agent/interject.go`, `internal/agent/filerefs_test.go`, `CHANGELOG.md`

**Tests:** `pdf_test.go` gains a fixture builder `hostilePDF(t, objects ...string) []byte` that
writes numbered objects, a correct xref table and trailer (so the parser accepts the file); then:
`TestExtractPDF_CapsAPhantomPageCount` — `/Type /Pages /Count 10000000 /Kids []` (the 594-byte
shape): returns `pdfNoTextMessage` within 2 s (`time.Since`), never allocating for 10 M pages
(assert via `testing.AllocsPerRun` staying under 1 000); `TestExtractPDF_RefusesAnAbsurdXrefSize`
— `/Size 4000000000` ⇒ failMessage names objects and bytes, returns within 100 ms;
`TestExtractPDF_BoundsAReferenceCycle` — a `Pages` node whose `Kids` references itself with
`/Count 1` ⇒ failMessage with "does not terminate" within 2 s;
`TestExtractPDF_HonoursACancelledContext` — a pre-cancelled ctx on the existing three-page fixture
⇒ failMessage "cancelled"; `TestExtractPDF_StopsAtTheTextBudget` — the existing multi-page fixture
with `maxTextBytes` smaller than page 1's text ⇒ `pages == 1` and the "content budget" marker;
`TestExtractPDF_CapsThePageWalk` — a fixture with `pdfMaxPages+1` real (tiny) pages ⇒ `pages ==
pdfMaxPages` and the "page cap" marker. The four existing `TestExtractPDF_*` tests pass with the
new signature. `read_file_test.go` — a phantom-count PDF returns the no-text error promptly.
`filerefs_test.go` — `TestResolveFileRefs_BoundsExtractionByTheClampBudget`: with a small
allocation, the block handed to the clamp for a many-page PDF is at most 2× the char budget plus
the marker line (observable through the rendered block's length); the eight existing
`TestResolveFileRefs_*`/`TestClampToBound_*` tests pass.

**Acceptance:** `go build ./... && go test ./internal/doctext/ ./internal/tools/ ./internal/agent/`

**Commit:** `fix(doctext): PDF extraction bounds page count, xref size, reference walks and output`

---

**Suggested version bump (not performed):** minor — `0.18.0`. Items 1–8 change what is spawned
and how (fenced absolute programs, a spawn-free fail-fast preamble, hardened git on every path, a
stdio MCP server's fence and teardown); items 9–12 change user-visible verdicts and notices (a
residual line in `/confine status`, guard coverage, `~/.apogee` writes prompting instead of
refusing, a forced gate that runs confined); item 13 changes `ExtractPDF`'s signature. The bump
is the owner's call, after the run.
