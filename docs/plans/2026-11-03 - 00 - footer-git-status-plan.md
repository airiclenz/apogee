# The footer states the workspace's git state

**Goal:** In a git workspace the TUI footer's upstream slot gains one segment after the workdir: `<branch> NΔ N↓ N↑`, kept current by a poll chain that never runs git on the render path. A periodic fetch, on by default behind `ui.git-fetch`, lets `↓` see new upstream commits.
**Date:** 2026-11-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** dc2edc0c
**Closes:** footer-git-status
**Sources:**
- `.capcom/issues/footer-git-status.md`
- `layout.md` "## The footer's upstream slot" · ADR 0031 (Driver boundary) · ADR 0054 (nil host = unwired degrade) · ADR 0011 (value-copied Model) · ADR 0030 (one width authority) · ADR 0093 (spend segment)
- AGENTS.md "A slow machine makes apogee slower, never broken"
- `internal/gitexec/doc.go` (the hardened git runner)

**Ratified design calls:**
- **Fetch:** periodic `git fetch`, on by default, behind the config key `ui.git-fetch` (owner, 2026-11-03).
- **Refresh:** a 5 s poll plus one coalesced refresh when a Turn ends (owner, 2026-11-03).
- **Toggle:** no key hides the segment; it gives way only by the priority fit (owner, 2026-11-03).
- **Priority:** drop order on a narrow row is spend → git → effort → workdir → host (owner, 2026-11-03).
- **Tones:** branch and zero counts in the footer's own muted tone; non-zero counts in the existing brighter muted role (`MutedBright`). No new theme role (owner, 2026-11-03).
- **Safe glyphs:** `ui.glyphs: safe` keeps `Δ ↓ ↑` (owner, 2026-11-03).
- **Refused repo:** where `gitexec` refuses the repository (repo-local command-valued key), the segment is absent and no fetch runs, as for the git tools (writer, from `internal/gitexec` posture, 2026-11-03).
- **No-terminal fetch:** the fetch child always starts with no controlling terminal; on Windows a user ssh command skips the fetch (writer, 2026-11-03).
- **Fetch transport:** the fetch argv adds `-c protocol.ext.allow=never -c protocol.git.allow=never --upload-pack=git-upload-pack`, so no repo-local `remote.*.uploadpack`, `ext::` URL or `core.gitProxy` runs a command. Tests: one per vector, each asserting no marker file is written. The item notes that this widens gitexec's documented promise (gitexec/doc.go) for the fetch verb only, and closes it in the host (owner, 2026-11-03).
- **Slow probe:** an incomplete-probe refusal (`CommandConfigIncompleteRefusal`) is an error that keeps the last counts. Only a `CommandConfigRefusal` means not-inside. Tell them apart through an exported predicate that item 2 adds to gitexec, never by matching the sentence in item 3 (owner, 2026-11-03).
- **Big status output:** status output is parsed as a stream, so counts are exact at any size. Item 3 drops "Truncated → error" for status (owner, 2026-11-03).
- **Test fake:** the fake keys on the `status` subcommand after the hardening `-c` pairs and answers the probe and rev-parse calls cleanly. Assert that the fetch spec has NoTerminal=true (owner, 2026-11-03).

**Standing requirements:**
- skills: coding-standards
- Test-run rules (pass to every sub-agent): never `-race` or `-cover` over a whole package or a pattern; a plain whole-package run (`GOMEMLIMIT=2GiB go test -count=1 ./<pkg>/`) only for the light packages `internal/gitstatus`, `internal/config`, `internal/domain`; heavy packages (`internal/tui`, `cmd/apogee`) only with `-run '<pattern>'`; never two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`). Model state stays plain values (ADR 0011).
- Every test duration goes through `internal/testtime`; no fixed deadline in the shipped binary may fail a healthy slow-box run.
- Production `internal/tui` imports neither `internal/gitexec` nor `internal/subprocess`; git runs behind a host seam that `cmd/apogee` backs.
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `status-line-priority-fit` (the status line, not the footer).
- Git state in `apogee headless`, the daemon or the engine.
- Submodule-aware counts, stash counts, a configurable poll or fetch interval.

**Regression check (2026-11-03, dc2edc0c):**
- 1: guard folded — supersedes `internal/gitexec/doc.go:54-56`.
- 2: split into 2 (env, no-terminal) and 3 (host).
- 3: recast — yields to `internal/tools/git.go:875-881` (`--ignore-submodules=dirty`).
- 4–8: guard folded.
- Re-check round:
- 1: recast — `Snapshot` owns its count fields; a streaming counter keeps no paths.
- 2: recast — guard folded (`CaptureOptions`, NoTerminal spec test, both `doc.go`); refusal-kind predicate; `CaptureEnvTo`.
- 3: recast — fetch transport closed (supersedes `internal/gitexec/doc.go:18-20` for fetch); incomplete probe keeps counts (yields to AGENTS.md slow-box rule); status streamed.

## 1. Shared porcelain v2 parser in `internal/gitstatus` — ✅ DONE (2026-11-03)

NOTES (2026-11-03): API shape for later items — `gitstatus.Parse(out string) Snapshot`; the streaming form is `gitstatus.Counter` (zero value ready, `io.Writer`, `Snapshot()` reads the folded counts and branch fields, entry lists nil). Snapshot fields: Branch, Detached, Upstream, HasAheadBehind, Ahead, Behind, Dirty, Staged, Unstaged, Untracked (counts), StagedEntries/UnstagedEntries/UntrackedEntries (Parse only).
NOTES (2026-11-03): `gitStatusReport.changedFiles()` became the tools-local function `changedFiles(gitstatus.Snapshot)` (a method cannot sit on the foreign type); it still derives the full-length triple from the entry lists, so git_test.go:891-892 call `changedFiles(rep)` with every assertion unchanged.

**What:** Recast at the regression check (2026-11-03).
**Goal:** A pure `internal/gitstatus` (no exec imports) parses `git status --porcelain=v2 --branch -z` output into a `Snapshot` of branch fields (branch, detached, upstream) and count fields (`Dirty`: one per changed path). The git status tool uses it and its output is unchanged.
**Approach (assumed at the header base):** Move `parseGitStatus`, `gitStatusReport`, `parseAheadBehind` and `porcelainFields` out of `internal/tools/git.go` as `Parse` / `Snapshot`; no copy stays. `Dirty` counts each `1`, `2`, `u` and `?` entry once (a rename once; a file both staged and unstaged once). Add `doc.go`.
**Regression guard.** `Snapshot` carries its own count fields: Dirty, the per-kind counts, Ahead and Behind. Both the string `Parse` and the streaming form fill them. `Parse` also keeps the entry lists the git_status tool renders, and its output stays byte-identical; `changedFiles` stays the full-length triple (`git.go:955`). The streaming form — an `io.Writer` (or reader-based parse) counting NUL-terminated `-z` records incrementally — keeps no paths and fills only the counts and branch fields. Every comment naming where porcelain parsing lives follows the move (`grep -rn 'porcelain' internal/gitexec/doc.go internal/tools/doc.go internal/tools/git.go`); this supersedes `internal/gitexec/doc.go:54-56`.
**Files:** `internal/gitstatus/gitstatus.go`, `internal/gitstatus/doc.go`, `internal/gitstatus/gitstatus_test.go`, `internal/tools/git.go`, `internal/tools/git_test.go`, `internal/tools/doc.go`, `internal/gitexec/doc.go`
**Read first:** `internal/tools/git.go` — GitStatus.Execute, parseGitStatus, renderGitStatus, changedFiles, porcelainFields; `internal/tools/git_test.go` — TestGitStatus_ParsesPorcelainRecords, TestGitStatus_CapsEachList
**Tests:** table tests in `gitstatus_test.go`: clean, staged+unstaged same file (Dirty 1), rename, unmerged, untracked files in a new dir (each counts), detached HEAD, no upstream (no `branch.ab`), `+4 -0` ahead/behind; `Parse` and the streaming form yield equal counts on the same input; a >256 KiB synthetic stream fed in small chunks, splitting records across writes, gives exact counts. Only `git_test.go`'s direct call sites (:860,886,887,891) are rewritten to `gitstatus.Parse`; every want/deny string and count assertion stays.
**Acceptance:**
- `go build ./internal/gitstatus/ ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/gitstatus/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'GitStatus' ./internal/tools/`
**Commit:** `refactor(gitstatus): move the porcelain v2 status parser out of the git tool`

## 2. gitexec Capture with caller env, and a no-terminal spawn — ✅ DONE (2026-11-03)

NOTES (2026-11-03): NoTerminal reuses startConfinedSession itself (run calls it for `confined || spec.NoTerminal`) rather than a renamed or second helper; its comments in session_unix.go, session_windows.go, subprocess.go and subprocess/doc.go now name the NoTerminal run beside the confined one. The denial watch stays confined-only.
NOTES (2026-11-03): the probe's own git calls run with zero CaptureOptions (no NoTerminal); only the command spec carries opts. The probe is a local `git config`/`rev-parse` listing that launches nothing that could prompt.
NOTES (2026-11-03): CaptureEnv and CaptureEnvTo share an unexported refuseCapture helper (probe under env, refusal as exit-1 outcome); CommandConfigIncompleteRefusal's sentence is unchanged byte for byte, now built from the incompleteRefusalLead const that IsIncompleteRefusal matches by prefix.
NOTES (2026-11-03): gitexec/doc.go now counts seven entry points and corrects the old "All five take an env" (Capture never took one) to "Every one but Capture".
NOTES (2026-11-03): the /dev/tty NoTerminal test skips without a controlling terminal (as the confined twin does); TestRunSubprocessNoTerminalChildLeadsItsOwnSession proves sid == pid from /proc on any Linux run, verified to fail with the NoTerminal branch removed.

**What:** Recast at the regression check (2026-11-03).
**Goal:** `gitexec.Host` has `CaptureEnv(ctx, gitPath, root, env, opts CaptureOptions, timeout, args...)`: Capture with the caller's env appended last through `runSpec` (which also copies `CaptureOptions{NoTerminal bool}` onto the spec) and the probe run under that SAME env; `Capture` is `CaptureEnv` with nil env and zero options. `subprocess.SubprocessSpec` has an opt-in `NoTerminal bool`: on POSIX the child is a new session leader (Setsid in place of Setpgid) and the teardown still works; on Windows a no-op.
**Approach (assumed at the header base):** Reuse `startConfinedSession`'s shape for `NoTerminal`.
**Regression guard.** The confined-run Setsid path and its tests stay unchanged; `Capture`'s argv, env and refusal outcome stay byte-identical for every caller. Every comment that counts the gitexec entry points or scopes Setsid to confined runs follows (`grep -rn 'entry points\|confined POSIX run\|Setsid' internal/gitexec internal/subprocess`).
Add the exported refusal-kind predicate from 3(b) (`IsIncompleteRefusal`). Add a streaming entry point: CaptureEnv's sibling `CaptureEnvTo` takes an `io.Writer` for stdout, built on `spawnTo`/`RunTo`'s path, with the probe and env semantics of CaptureEnv and NoTerminal available. The stream is uncapped, so Truncated cannot occur on it.
**Files:** `internal/gitexec/gitexec.go`, `internal/gitexec/gitexec_test.go`, `internal/gitexec/doc.go`, `internal/subprocess/subprocess.go`, `internal/subprocess/doc.go`, `internal/subprocess/session_unix.go`, `internal/subprocess/session_windows.go`, `internal/subprocess/subprocess_unix_test.go`
**Read first:** `internal/gitexec/gitexec.go` — runSpec, probeCommandConfig; `internal/subprocess/session_unix.go` — startConfinedSession; `internal/subprocess/subprocess_unix_test.go` — withAttrRecorder
**Tests:** CaptureEnv's env reaches both the probe and the child; Capture output is unchanged; a fake-Spawn test records NoTerminal=true on the spec when `CaptureOptions.NoTerminal` is set; `CaptureEnvTo` streams >256 KiB with no Truncated, probes under the same env and writes nothing in a refused repo; `IsIncompleteRefusal` is true for the incomplete sentence, false for `CommandConfigRefusal`; a NoTerminal child on POSIX has no controlling terminal (sid equals pid, or opening `/dev/tty` fails); ctx cancel still kills a NoTerminal child's group.
**Acceptance:**
- `go build ./internal/gitexec/ ./internal/subprocess/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'CaptureEnv|Capture|IncompleteRefusal' ./internal/gitexec/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'NoTerminal|Session|Teardown' ./internal/subprocess/`
**Commit:** `feat(gitexec): let a probed capture carry caller env and start with no terminal`

## 3. Hardened status and fetch runner in `cmd/apogee` — ✅ DONE (2026-11-03)

NOTES (2026-11-03): the BatchMode `GIT_SSH_COMMAND` decision (which costs a `git config --get core.sshCommand` spawn) is made for Fetch only; Status carries the three no-prompt variables and the set ssh pass-through variables but no BatchMode, because status never runs ssh and the extra spawn on every 5 s poll would only cost a slow box.
NOTES (2026-11-03): Status treats a git `Resolve` failure (no git on PATH, or a fenced git) as not-inside with a nil error, the §3a graceful degrade; Fetch returns that failure as its error.
NOTES (2026-11-03): the host is an unexported `gitStatusHost` value type (zero value = system git, real env, runtime.GOOS) with `lookupEnv` and `goos` seams for the fake-spawn tests; item 7 wires it. On error Status returns a zero snapshot with inside=false and the caller keeps its last answer.
NOTES (2026-11-03): the unreachable-fetch test covers three remotes (refused https, refused ssh, and a local httptest server answering 401 with a Basic challenge, the one that would prompt for credentials); each transport-vector test first proves its vector live under a plain `git fetch` (marker written) before asserting the host's fetch writes none.

**What:** Recast at the regression check (2026-11-03). Depends on items 1 and 2.
**Goal:** `cmd/apogee` has a git-status host with `Status(ctx, root) (gitstatus.Snapshot, bool, error)` (the bool is "inside a work tree") and `Fetch(ctx, root) error`, both run through `internal/gitexec`, never prompting and never taking `index.lock`.
**Approach (binding):** New `cmd/apogee/gitstatus_host.go`.
- Status argv: `status --porcelain=v2 --branch --untracked-files=all --ignore-submodules=dirty -z`, streamed through `CaptureEnvTo` into gitstatus's streaming form. Yields to `internal/tools/git.go`'s submodule decision: Δ skips dirt inside submodules.
- Fetch argv: `-c fetch.writeFetchHead=false -c maintenance.auto=false -c gc.auto=0 -c protocol.ext.allow=never -c protocol.git.allow=never fetch --quiet --no-tags --recurse-submodules=no --upload-pack=git-upload-pack`, through `CaptureEnv`.
- TimedOut (or Truncated, fetch only) is an error that keeps the last counts. A `CommandConfigRefusal` is not-inside; an incomplete-probe refusal (`gitexec.IsIncompleteRefusal`) is an error that keeps the last counts. Not-inside is decided by `rev-parse --is-inside-work-tree`, never by stderr text.
- Env for both: `GIT_OPTIONAL_LOCKS=0`, `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=never`; a set `SSH_AUTH_SOCK`, `GIT_SSH_COMMAND` or `GIT_SSH` passes through; `GIT_SSH_COMMAND=ssh -o BatchMode=yes` only when none of `GIT_SSH_COMMAND`, `GIT_SSH` or a configured `core.sshCommand` is present.
- The fetch always runs with `NoTerminal`; on Windows it is skipped whenever a user ssh command is present.
- 10 min backstop per call; gitexec's 15 s probe timeout is unchanged.
**Regression guard.** Never Run/Query; never a zero snapshot from a failed call; no `fetch` or `status` argv in a refused repo; `cmd/apogee/doc.go` names the new file (TestDocMapNamesEveryFile). The transport flags widen the promise of `internal/gitexec/doc.go:18-20` for fetch only, and this host closes it — superseded for fetch. The incomplete-probe rule yields to AGENTS.md's slow-box rule.
**Files:** `cmd/apogee/gitstatus_host.go`, `cmd/apogee/gitstatus_host_test.go`, `cmd/apogee/doc.go`
**Read first:** `internal/gitexec/gitexec.go` — Host.Spawn, probeGit, CommandConfigRefusal, CommandConfigIncompleteRefusal; `internal/security/shellwrites.go` — GitCommandConfigNameSource; `internal/gitexec/gitexec_test.go` — writeFakeGit; `cmd/release/publish.go` — nonInteractiveEnv
**Tests:** real `git` in `t.TempDir()` repos: counts match a known tree (2 modified, 1 staged, a new dir with 3 files → `Dirty` 6); a clone of a bare temp remote reports ahead/behind after commits on each side and Fetch; not-a-repo → not-inside, nil error; a repo-local `core.sshCommand` → not-inside, no recorded `fetch`/`status` argv; the child env carries the four variables plus a set `SSH_AUTH_SOCK`, and no BatchMode `GIT_SSH_COMMAND` beside the user's own (fake spawn); the recorded argvs carry `--ignore-submodules=dirty` (status) and the five `-c` keys, `--recurse-submodules=no`, `--upload-pack=git-upload-pack` (fetch), and the fetch spec NoTerminal=true; a TimedOut status is an error, never a snapshot (fake per the Test fake call); an incomplete-probe refusal keeps the last counts; a >256 KiB status stream gives exact counts; one test per transport vector — `remote.origin.uploadpack` on a local-path remote, `protocol.ext.allow=always` with an `ext::` URL, `core.gitProxy` on a `git://` remote — each asserting no marker file; Fetch against an unreachable URL errors and never blocks on a prompt.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'GitStatusHost|DocMap' ./cmd/apogee/`
**Commit:** `feat(apogee): add a non-prompting git status and fetch host for the footer`

## 4. The `ui.git-fetch` setting — ✅ DONE (2026-11-03)

NOTES (2026-11-03): `ui.git-fetch` sits last in the `ui:` block (after `ui.notify`) in uiKeys, the registry, the template's key lines and its prose; the manual's key count moves from eleven to twelve "more keys" and the new key joins `ui.notify`/`ui.inspector`/`ui.stall-after` as the ones that are not screen looks, with its own paragraph after `ui.inspector`'s.
NOTES (2026-11-03): The TUI applies the key to itself (settingsApplyLocal sets `UI.GitFetch`); nothing reads the field yet — the poll chain that honours it is item 5.

**What:**
**Goal:** `ui.git-fetch` is a bool UI pref, default true, editable on the settings screen, documented in `docs/manual/configuration.md`, and applied live to the TUI's `UIPrefs`.
**Approach (assumed at the header base):** Follow `ui.skill-suggestions` end to end through the Files. Registry desc: periodic `git fetch` so the footer's `↓` sees new upstream commits; the manual entry says it touches the network and writes remote-tracking refs.
**Regression guard.** The registry row, the template line (and its prose paragraph) and the `uiKeys` entry sit at one relative position (`TestRegistryFollowsTheTemplateOrder`, `internal/config/defaults_test.go:437`).
Pinned tables follow the new key: `GitFetch: true` in every `config_test.go` literal that spells the default block (`grep -n 'SkillSuggestions: true' internal/config/config_test.go`) and `GitFetch: boolptr(false)` in `everyKeyFileConfig`; `registry_test.go`'s `owns` → `"UI"` and `documented` → `"false"`; a `uiKeyCases` row (value `"false"`, bad `"maybe"` → `apogee: invalid ui.git-fetch "maybe": want true or false`) and the key at its `uiKeys` position in the pinned list; `"ui.git-fetch": "false"` in `settingsrows_test.go`'s want map; a `{settingTrue, "maybe"}` pair in `settingsapply_test.go`. Every prose count of the `ui:` block's keys follows (`grep -rn -i 'eleven more keys\|twelve keys' docs/manual internal/domain`).
**Files:** `internal/domain/uiprefs.go`, `internal/domain/uiprefs_test.go`, `internal/domain/doc.go`, `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/registry.go`, `internal/config/defaults/config.yaml`, `internal/config/registry_test.go`, `internal/tui/settingsapply.go`, `internal/tui/settingsapply_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/settingsrows_test.go`, `docs/manual/configuration.md`
**Read first:** `internal/domain/uiprefs.go` — uiKeys, DefaultUIPrefs; `internal/config/registry.go` — KeyRegistry; `internal/config/config_test.go` — everyKeyFileConfig
**Tests:** the drift tests cover the row once the pinned tables are extended; a `UIPrefs.Set` round-trip case (the `uiKeyCases` row); a config load test that `ui: {git-fetch: false}` lands false and absent lands true.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/config/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Settings|Manual' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'SettingsApplyLocal' ./internal/tui/`
**Commit:** `feat(config): add the ui.git-fetch setting`

## 5. TUI git poll chain behind a host seam — ✅ DONE (2026-11-03)

NOTES (2026-11-03): consequential edit — internal/tui/watch.go: made necessary by the four new Msgs Model.fold folds (gitStatusMsg, gitStatusTickMsg, gitFetchMsg, gitFetchTickMsg); TestWatchIsolatesRoutesEveryFoldedMsg requires watchRouteOf to name each, and they route to the view (the git state is a Model field outside the swapped session view).
NOTES (2026-11-03): seam shape — `tui.GitStatusHost{Status(ctx, root) (gitstatus.Snapshot, bool, error); Fetch(ctx, root) error}` on `Options.GitStatus`, plus `Options.GitStatusInterval`/`GitFetchInterval`; wired means host non-nil AND `Options.Workspace` non-empty. The footer (item 6) reads `m.gitSnapshot() (gitstatus.Snapshot, bool)`; the Model keeps counts only (entry lists dropped).
NOTES (2026-11-03): the long backoff after a not-inside or failed read is `gitStatusBackoff = time.Minute` (max with 4 × the read's duration); `Options.GitStatusInterval` does not shorten it.
NOTES (2026-11-03): delegate/workflow paths do not count as Turn ends: a delegate's Turns end inside the parent's Exchange, whose own turnSnapshotMsg/cancelledMsg/errMsg re-reads after them, and a background workflow emits none of those Msgs, so its edits show on the next 5 s tick (stated on gitAfterTurn).
NOTES (2026-11-03): the fetch chain fires its first fetch at once when armed (a landed read that names an upstream with the pref on, or the pref switched on at /settings), then every 5 min after each landing; switched off (or upstream gone) it retires at its next step rather than by a generation bump, so a fetch in flight finishes and two fetches never run at once. Only a successful fetch asks for a status read.
NOTES (2026-11-03): the turnSnapshotMsg arm now schedules the save in a statement of its own (`save := m.persist(...)`) before handing m to gitAfterTurn, so the pointer-receiver persist is ordered before the copy (ADR 0011).
NOTES (2026-11-03): settingsApplyLocal's `ui.git-fetch` arm now returns the arming Cmd; its doc ("Two of these keys…") now names git-fetch beside the colour scheme. TestGitStatusImportsNoGitRunner pins that production internal/tui imports neither internal/gitexec nor internal/subprocess.
NOTES (2026-11-03): retry fix — Build now calls cleanup() before the `--tui-diag` error return (as the buildProgramOptions error path does), so the derived program context is cancelled on every error return after it is made.

**What:** Depends on items 1 and 4.
**Goal:** `tui.Options` carries a `GitStatus` host (nil = no segment, no git). The Model holds the last good snapshot as plain values, kept by one status and one fetch chain, each with one live generation; no git call runs in `Update` or `View`; every git child is cancelled at exit.
**Approach (assumed at the header base):** New `internal/tui/gitstatus.go`; a `GitStatusHost` interface mirrors item 3's methods. Like the heartbeat chain: a Cmd runs `Status`; the fold stores the snapshot (an error keeps the last one; not-inside clears it) and re-arms after `max(5s, 4 × the call's elapsed)`, so it never overlaps. Turn end (`turnSnapshotMsg`, `cancelledMsg`, `errMsg`) refreshes now if idle, else sets a dirty flag that re-runs once when the in-flight call lands. Fetch chain: every 5 min while `ui.git-fetch` is on and the snapshot has an upstream; a landed fetch triggers a status refresh; toggling the pref arms or retires it. `Options.GitStatusInterval` / `GitFetchInterval` shorten the cadence (like `BeatInterval`). Started from `Model.Init`; `internal/tui/doc.go` names the file with the one-live-chain invariant.
**Regression guard.** Derive `ctx, cancel := context.WithCancel(ctx)` in `Build` (tests enter there, not `Run`), pass it to `newModel` and `tea.WithContext`, put `cancel` in `closers`, and cancel on Build's error returns. The idle Turn-end refresh bumps the status generation (the `armBeat` shape) so a pending tick never runs beside the new chain. Say whether the delegate/workflow paths count. After not-inside or a git error the chain re-polls only on Turn end or at a long backoff stated in the code, never on the 5 s cadence.
**Files:** `internal/tui/gitstatus.go`, `internal/tui/gitstatus_test.go`, `internal/tui/model.go`, `internal/tui/tui.go`, `internal/tui/settingsapply.go`, `internal/tui/doc.go`
**Read first:** `internal/tui/heartbeat.go` — armBeat, beatCmd, foldBeatMsg; `internal/tui/model.go` — Update; `internal/tui/tui.go` — Build; `internal/tuitest/leak.go` — timerFrames
**Tests:** fake host in `gitstatus_test.go`: first snapshot lands in the Model; an error keeps the last counts; a slow fake (blocks until released) never overlaps a second call and `View` renders while it is blocked; turn end while in flight yields exactly one follow-up call; Turn end while idle, then both ticks land, gives exactly one Status call per interval; after not-inside (and after an error) no Status call runs on the 5 s cadence; fetch runs only with the pref on and an upstream, and its landing triggers a status call; nil host arms nothing; exiting a program made by `Build` cancels a blocked call's ctx.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'GitStatus|DocMap' ./internal/tui/`
**Commit:** `feat(tui): poll git state off the render path for the footer`

## 6. Footer git segment, fit and tones — ✅ DONE (2026-11-03)

NOTES (2026-11-03): `↓`/`↑` show only when the snapshot carries an ahead/behind header (`Snapshot.HasAheadBehind`), not merely when `Upstream` is set: a configured but deleted upstream reports no counts, and a `0↓ 0↑` there would claim the branch is level with an upstream git did not compare it to.
NOTES (2026-11-03): the brighter tone is a new raw-colour field `theme.mutedBright` bound to the scheme's existing `MutedBright` role (the same colour `toolDetailBright` uses), painted onto `footerText` the way `errorFg` paints offline; no scheme role was added. The glyphs are new `glyphSet` fields `gitDirty`/`gitBehind`/`gitAhead`, identical in both sets.
NOTES (2026-11-03): `footerLayout` reports the kept git segment as `git` + `gitAt` (its byte offset in `info`, computed by `footerInput.gitOffset` from the same `footerRun` joiner); `Model.footerLeft` now takes `(lead, footerLayout)` and paints the git span via `Model.footerGit`, which re-words the spans from the snapshot and falls back to plain text if they ever fail to re-join to the measured text. The wording lives in `gitFooterSpans`/`footerSpansText` in footerfit.go.
NOTES (2026-11-03): the model-level tests (exact run, no-upstream, detached, escape-stripped branch, drop before the effort word, bright tones only on non-zero counts) are `TestFooterStatesTheGitSegment` and `TestFooterTonesOnlyNonZeroGitCounts` in mode_test.go, beside `footerLeftRun`; the span wording is also pinned directly in footerfit_test.go (`TestGitFooterSpansWordTheWorkingTree`, `TestGitFooterSpansKeepTheirMarksUnderSafeGlyphs`).

**What:** Depends on item 5.
**Goal:** The footer's left run reads `host ✦ model ✦ effort ✦ workdir ✦ <branch> NΔ N↓ N↑ ✦ spend`; `↓`/`↑` are absent without an upstream, the branch on a detached HEAD, and the segment with its separator when there is no snapshot. Drop order and tones follow the header's ratified calls.
**Approach (assumed at the header base):** `footerInput` gains `git`; `footerFit`'s ladder gains the git rung between the spend and effort rungs, and `floor()` passes `in.git` to its `footerRun`. `footerLayout` reports the kept segments so the painter styles the git segment span by span (the truncating `floor` case paints plain `footerText`); the fill gap counts it. Glyphs `gitDirty`/`gitBehind`/`gitAhead` = `Δ ↓ ↑` in `defaultGlyphs` and `safeGlyphs`. The branch passes through `stripEscapes`. Measure with `m.th.measure` (ADR 0030).
**Regression guard.** The manual's `ui.glyphs` paragraph ("Three things keep their shape") names the footer's `Δ ↓ ↑` among what `safe` keeps. Every comment stating the footer's segment list or drop order follows — `grep -n 'given up first\|six facts\|six plain\|spend first\|effort word, then' internal/tui/*.go`.
**Files:** `internal/tui/footerfit.go`, `internal/tui/footerfit_test.go`, `internal/tui/model.go`, `internal/tui/theme.go`, `internal/tui/model_test.go`, `internal/tui/mode_test.go`, `internal/tui/doc.go`, `docs/manual/configuration.md`
**Read first:** `internal/tui/footerfit.go` — footerFit, floor, footerInput, footerLayout; `internal/tui/model.go` — footerLeft, footerContent; `internal/tui/theme.go` — safeGlyphs
**Tests:** extend `TestFooterFitDropsSegmentsInPriorityOrder` with the git rung; `TestFooterFitLeavesNoDanglingSeparator` / `TestFooterFitSkipsASegmentNothingNamed` with git absent; `TestFooterFitsTheWindowExactly` for widths 0–80 with a git snapshot; new tests: exact left run `main 5Δ 0↓ 4↑`, no-upstream `main 5Δ`, detached `5Δ`, escape-laden branch stripped, styled bytes show `MutedBright` only on non-zero counts.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Footer|SafeGlyph' ./internal/tui/`
**Commit:** `feat(tui): show branch and change counts in the footer`

## 7. Wire the host into the TUI and drive it end to end

**What:** Depends on items 3 and 6.
**Goal:** The TUI Driver passes item 3's host as `tui.Options.GitStatus`; a driven run in a git workspace shows the segment, and a commit pushed to the upstream shows in `↓` within one fetch interval.
**Approach (assumed at the header base):** Set `GitStatus` in `wire_options.go` `options()` for the TUI path only, rooted at `w.roots.workspace`. E2E after `e2e_livestate_test.go`: a temp clone of a bare temp remote, short intervals, assert the footer row, push from a second clone, `testtime.Eventually` for `1↓`. Non-git e2e workspaces render no segment; the control-plane workspace's hand-seeded `.git` (no HEAD) stays not-inside.
**Regression guard.** Drive at `liveStateSize` (W=140) or wider with a short clone path, and assert the whole `<branch> 0Δ 0↓ 0↑` run is seated before pushing (at W=100 git drops first). `e2e_support_test.go` gains a session field and a launcher that set both intervals, shaped like `beat`/`launchTUIOnBeating`. Commits pass `-c user.name=… -c user.email=…` (as `secretsRepoWorkspace` does); the branch is pinned (`init -b`) or read back.
**Files:** `cmd/apogee/wire_options.go`, `cmd/apogee/e2e_gitstatus_test.go`, `cmd/apogee/e2e_support_test.go`
**Read first:** `cmd/apogee/e2e_support_test.go` — beat, launchTUIOnBeating; `cmd/apogee/e2e_smoke_test.go` — footerRow
**Tests:** the new e2e; `e2e_livestate_test.go`, `TestE2EGuardSecrets*` and `TestE2EGuardControlPlane` pass unchanged.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'E2EGitStatus|LiveState|E2EGuardSecrets|E2EGuardControlPlane' ./cmd/apogee/`
**Commit:** `feat(apogee): wire the git status host into the TUI footer`

## 8. layout.md describes the git segment

**What:** Depends on item 6.
**Goal:** layout.md's "## The footer's upstream slot" states the git segment: place, the three counts, absence rules (no upstream, detached, outside git, refused repo), tones, cadence, `ui.git-fetch`, and the drop order with renumbered priorities.
**Approach (assumed at the header base):** Add a "**The git state.**" paragraph after "**The spend.**", linking `docs/manual/configuration.md` for `ui.git-fetch`.
**Regression guard.** Every sentence there naming the segment order, drop order or last segment follows, including "**What it is.**", "**The workdir the slot ends on.**" and the `✦ offline` sentence ("after the workdir and"); grep the section for `workdir|spend|goes first|last segment|after the workdir|priority|host ✦ model`.
**Files:** `layout.md`
**Read first:** `layout.md` — "## The footer's upstream slot"; `internal/tui/footerfit.go` — footerFit
**Tests:** none (prose); `grep -n 'git-fetch\|The git state' layout.md`.
**Acceptance:**
- `grep -q 'The git state' layout.md && grep -q 'ui.git-fetch' layout.md`
**Commit:** `docs(layout): describe the footer's git segment`
