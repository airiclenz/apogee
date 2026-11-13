# Urgent defects and doc truth — plan

**Goal:** Fix the open P2 Windows junction bug and the Terminal.app click hint, close three defects the 2026-11-13 refocus confirmed against the code (a git signing-command denylist gap, the adoption pane's silence about unpinned launcher reads, a fixed 30 s staged-binary check), make MCP discovery truncation visible, and correct four verified manual mismatches.
**Date:** 2026-11-13
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0b3788ca

**Sources:**
- `.capcom/issues/junction-project-root.md`, `.capcom/issues/terminal-app-click-hint.md`
- `docs/reviews/code-audit-2026-11-10.md` (§git command-config denylist; §adoption pin vs adopted launcher)
- ADR 0096 (Project config, Adoption), ADR 0104 (adoption pin), AGENTS.md (slow-box rule)
- `docs/skill-runs/refocus/2026-11-13/briefing.md` (doc fixes 1–4)

**Ratified design calls** (owner, 2026-11-13):
- **Junctions:** resolved like symlinks through one shared resolver (`EvalSymlinks` first; on Windows, when it fails and the path exists, the final-path-name fallback); never treated as opaque.
- **Click hint:** three-way by terminal — known linkifier (Zed, VS Code, iTerm2, WezTerm, kitty) keeps `cmd+click to open`; `TERM_PROGRAM=Apple_Terminal` gets `drag to select and copy`; unknown (most SSH sessions) gets `cmd+click to open · drag to copy`. No GOOS split.
- **Git denylist:** add `gpg.ssh.defaultKeyCommand` to the denylist only; the sync rebase keeps signing.
- **Adoption pane:** a dim disclosure line `not pinned: anything it reads or runs beyond these`; manifest pinning and tree hashing rejected (recorded in an ADR 0104 amendment).
- **MCP caps:** documented in the manual AND announced as a notice when discovery is cut short.
- **Staged-binary check:** a 5-minute backstop with a distinct timeout message (no owner call needed; slow-box rule).

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi box: never `go test -race` over a whole heavy package; narrow with `-run`, prefix `GOMEMLIMIT=2GiB`.
- Durations in tests go through `internal/testtime`.
- Bubble Tea v2 names only (`tea.KeyPressMsg`).

**Out of scope:**
- Remote-helper (`remote.<n>.vcs`, `foo::`) and `GIT_ALLOW_PROTOCOL` hardening.
- Pinning launcher manifests or hashing the root tree.
- The ~20 unverified doc inconsistencies the refocus listed.
- The v1 placeholder-retraction plan.

**Regression check (2026-11-13, 0b3788ca):**
- 1: guard folded
- 2: guard folded (supersedes internal/projectroot/doc.go:18, "a leaf — it imports only the standard library")
- 3: guard folded
- 4: guard folded
- 5: guard folded; yields to internal/security/rules.go:289-290 (the security package reads no disk; resolution stays in agent/construct.go)
- 6: guard folded
- 7: guard folded; zero-value gesture fixed by writer decision (unset is the unknown case, `cmd+click to open · drag to copy`)
- 8: guard folded
- 9: guard folded
- 10: guard folded
- 11: guard folded (supersedes ADR 0109 decision 8's fixed note set and its 2026-11-07 amendment; docs/design/mcp-client.md:129-131 "returns the capped list silently")
- 12: guard folded (writer decision; reviewer verdict SAFE)

## 1. A junction-crossing real-path resolver in internal/platform — ✅ DONE (2026-11-13)

NOTES (2026-11-13): the fallback seam `realPathFallback` is declared per OS (realpath_windows.go = finalPathName, which already opens the path itself; realpath_other.go = nil) rather than in realpath.go, per the item's regression guard; RealPath consults it only when evalSymlinks fails AND os.Lstat finds the path, otherwise it returns evalSymlinks' error unchanged.

NOTES (2026-11-13): doc.go's file count updated "Thirty" -> "Thirty-three"; the three realpath files are mapped inside the existing Host-abstraction group (no new group).

NOTES (2026-11-13): the portable symlink case skips on Windows only when os.Symlink is refused (no Developer Mode/elevation); the junction case in realpath_windows_test.go needs no privilege.

NOTES (2026-11-13): no CHANGELOG entry — the resolver has no caller until items 2–4 wire it in; those items carry the user-visible entry.

**What:**
**Goal:** `internal/platform` exports one resolver, `RealPath(p string) (string, error)`, that returns the same result as `filepath.EvalSymlinks` whenever that succeeds, and on Windows resolves a path that crosses an NTFS junction to its final path (no `\\?\` prefix) when `EvalSymlinks` fails and the path exists; on POSIX it is `EvalSymlinks` alone.
**Approach (assumed at the header base):** a portable `RealPath` calls a package seam var (`evalSymlinks`) first; a `_windows.go` fallback opens the path and reuses `finalPathName` + `stripLongPathPrefix` (platform_windows.go, host.go). The seam lets Linux tests force the fallback branch. No other package changes in this item.
**Regression guard.** Add `internal/platform/doc.go` to Files and name the three realpath files in its file map beside host.go/platform_windows.go (`TestDocMapNamesEveryFile` fails otherwise). The Windows junction test builds its expected target from the long-name `tempDir(t)` helper (confiner_windows_test.go) or compares with `filepath.EvalSymlinks(target)` / `os.SameFile` — CI's `t.TempDir()` is the 8.3 `RUNNER~1` spelling. The fallback resolver is a second package var beside `evalSymlinks` (per-OS default; nil on POSIX) — the single `evalSymlinks` seam the Approach names cannot reach a fallback from Linux; config tests use config's own resolver seam (item 3), not these.
**Files:** internal/platform/realpath.go, internal/platform/realpath_windows.go, internal/platform/realpath_other.go, internal/platform/realpath_test.go, internal/platform/realpath_windows_test.go, internal/platform/doc.go
**Read first:** internal/platform/platform_windows.go — finalPathName; internal/platform/host.go — stripLongPathPrefix; internal/platform/confiner_windows_test.go — makeJunction, tempDir; internal/platform/doc.go — file map; internal/platform/docmap_test.go — TestDocMapNamesEveryFile; internal/config/configsplice.go — evalSymlinks, resolveWriteTarget (prior-art seam + fallback)
**Tests:** table test: plain dir, symlinked dir, missing path (error is `fs.ErrNotExist`-classed); a Linux case swaps both package vars to force the fallback branch; Windows test builds a `mklink /J` junction (reuse the `makeJunction` pattern from confiner_windows_test.go) and asserts `RealPath` returns the target spelling, the expected path built from `tempDir(t)`.
**Acceptance:**
- `go build ./internal/platform/ && GOOS=windows go vet ./internal/platform/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestRealPath ./internal/platform/`
**Commit:** `feat(platform): add a junction-crossing RealPath resolver`

## 2. Project root and adoption key resolve through junctions — ✅ DONE (2026-11-13)

NOTES (2026-11-13): projectroot_test.go and store_test.go left unchanged — the plan's Tests ask only that TestResolveSymlinkedWorkspace and TestRecordIsNamedByTheFullDigestOfTheResolvedRoot still pass unchanged, and they do; the two _windows_test.go files are new (they did not exist at the base).

NOTES (2026-11-13): each new Windows test file carries its own makeJunction/longTempDir helpers (platform's makeJunction/tempDir are unexported test helpers of another package); longTempDir resolves t.TempDir through platform.RealPath as the item's regression guard asks.

NOTES (2026-11-13): the adoption Windows test also asserts the junction spelling and the root's own spelling share one record and one config lock (EvalSymlinks upper-cases the drive letter, as GetFinalPathNameByHandle reports it) — the same claim the existing symlink test makes; it has not run on Windows yet (item 6 wires the CI leg).

NOTES (2026-11-13): CLOSES lists junction-project-root because the item's Goal says it fixes it; items 3 and 4 complete the issue's remaining paths (projectwrite, layer reads), so it should close only once those land.

**What:**
**Goal:** `projectroot.Resolve` finds a root above a workspace that sits under a junction, and the adoption store opens and names a record for such a root, with every existing adoption key byte-identical for non-junction roots. Fixes `junction-project-root` (project layer silently lost; adopt hard-errors `adoption store: resolve Project root`).
**Approach (assumed at the header base):** replace the `filepath.EvalSymlinks` calls in `projectroot.realPath` and `adoption` `key` (store.go) with `platform.RealPath`. Keep `Resolve`'s spelling rule (root == workspace keeps the workspace spelling).
**Regression guard.** Windows tests build expected paths in long-name form (resolve the temp dir through platform.RealPath first); Acceptance runs one package per `go test -race` line (Pi rule) — split the projectroot and adoption runs. Add `internal/projectroot/doc.go` to Files and reword its leaf sentence (standard library plus `internal/platform`, which imports only `internal/domain` and `winlabel` — no cycle with config); this supersedes doc.go:18, "a leaf — it imports only the standard library".
**Files:** internal/projectroot/projectroot.go, internal/projectroot/projectroot_test.go, internal/projectroot/projectroot_windows_test.go, internal/projectroot/doc.go, internal/adoption/store.go, internal/adoption/store_test.go, internal/adoption/store_windows_test.go
**Read first:** internal/projectroot/projectroot.go — Resolve, realPath; internal/adoption/store.go — key, New, ConfigLockPath; internal/projectroot/projectroot_test.go — realTempDir; internal/adoption/store_test.go — TestRecordIsNamedByTheFullDigestOfTheResolvedRoot; internal/projectroot/doc.go — package doc
**Tests:** Windows `mklink /J` tests: `Resolve` walks up to a `.apogee` root above a workspace reached through a junction; `adoption.New` succeeds and `ConfigLockPath` is stable for that root; every expected path is built in long-name form (temp dir through `platform.RealPath`), never a raw `t.TempDir()` join. Linux: existing `TestResolveSymlinkedWorkspace`, `TestRecordIsNamedByTheFullDigestOfTheResolvedRoot` still pass unchanged.
**Acceptance:**
- `GOOS=windows go vet ./internal/projectroot/ ./internal/adoption/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/projectroot/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/adoption/`
**Commit:** `fix(projectroot): resolve the Project root and adoption key across junctions`
Depends on item 1.

## 3. Project config writes work under a junction — ✅ DONE (2026-11-13)

NOTES (2026-11-13): the config-side seam is `projectRealPath` (projectwrite.go, wraps platform.RealPath); `evalSymlinks` stays resolveWriteTarget's alone. The Linux seam test (TestProjectWriteRefusesARootTheResolverCannotResolve) swaps it inline, non-parallel with t.Cleanup, as forceResolveFailure does — forceResolveFailure itself swaps the other seam.

NOTES (2026-11-13): the Windows tests carry their own longTempDir/junctionOrSkip helpers (the outside-root case junctions `.apogee/` to a sibling folder and checks both insideRoot directly and editProject end to end); they have not run on Windows yet — item 6 wires the CI leg.

NOTES (2026-11-13): CLOSES repeats junction-project-root — item 4 completes the issue's layer-read path, so it should close only once that lands.

NOTES (2026-11-13): execpin.go's realPathOrSelf still calls filepath.EvalSymlinks; left alone as outside this item's Files (the layer-read/guard items own the read side).

**What:**
**Goal:** saving a project setting and adopting under a junctioned Project root succeeds, and the containment check refuses a path outside the root exactly as before. Fixes the write half of `junction-project-root`.
**Approach (assumed at the header base):** in `internal/config/projectwrite.go`, `prepareProjectFile`, `insideRoot` and `isHome` resolve both sides of every comparison through `platform.RealPath` (never one side `EvalSymlinks`, the other final-path), so `filepath.Rel` containment stays sound.
**Regression guard.** item 3 defines the single config-side resolver seam (an unexported package var in internal/config wrapping platform.RealPath, swappable in tests); its Windows tests build expected paths in long-name form (resolve the temp dir through the resolver first — GitHub runner TEMP is the 8.3 short name RUNNER~1). A test swaps the seam non-parallel with `t.Cleanup`, as `forceResolveFailure` (configsplice_test.go) does; `evalSymlinks` (configsplice.go, "nothing else assigns it") stays `resolveWriteTarget`'s alone.
**Files:** internal/config/projectwrite.go, internal/config/projectwrite_test.go, internal/config/projectwrite_windows_test.go
**Read first:** internal/config/projectwrite.go — prepareProjectFile, insideRoot, isHome; internal/config/configsplice.go — evalSymlinks; internal/config/projectwrite_test.go — projectWriteFixture, TestProjectWriteRefusesASymlink; internal/config/configsplice_windows_test.go — junctionConfigOrSkip; internal/config/configsplice_test.go — forceResolveFailure
**Tests:** Windows: a project setting written through a junctioned root lands in the target's `.apogee/config.yaml`; a path outside the root is still refused; expected paths in long-name form. Linux: `TestProjectWriteRefusesASymlink`, `TestProjectWriteRefusesAMissingRoot` unchanged; a forced resolve failure through the config seam refuses with the existing wording.
**Acceptance:**
- `GOOS=windows go vet ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestProjectWrite' ./internal/config/`
**Commit:** `fix(config): write Project config under a junctioned root`
Depends on item 1.

## 4. Project layer reads under a junction, and says so when it cannot

**What:**
**Goal:** the project layer loads when `.apogee/config.yaml` sits under a junction; a layer file that exists but cannot be resolved produces the existing `projectSkippedNotice` instead of a silent skip; a truly missing file stays silent; identity checks recognise a junctioned `~/.apogee` as the global home.
**Approach (assumed at the header base):** `readProjectLayer` (layer.go) stats the path first and resolves only what exists, via `platform.RealPath`; `sameConfigFile` (layer.go) and `samePath` (config.go) compare with `os.Stat` + `os.SameFile`, as `agent/construct.go` `sameFile` already does.
**Regression guard.** item 4 depends on item 3 and reuses item 3's config resolver seam, defining no second one; its Windows tests build expected paths in long-name form as in item 3. `samePath` keeps its `filepath.Clean` equality short-circuit and falls back to `os.Stat` + `os.SameFile` only after it (a not-yet-created own home stays refused). `readProjectLayer` stats with `os.Stat`, never `Lstat`; only `errors.Is(err, fs.ErrNotExist)` is silent, every other stat error (EACCES, ENOTDIR) keeps `projectSkippedNotice`. Acceptance adds `GOOS=windows go vet ./internal/config/` and a `-run` covering `TestLayeredConfig|TestApplyConfigSessionsWatch`; new tests are named `TestReadProjectLayer…` / `TestSameConfigFile…`.
**Files:** internal/config/layer.go, internal/config/config.go, internal/config/layer_test.go, internal/config/layer_windows_test.go
**Read first:** internal/config/layer.go — readProjectLayer, sameConfigFile, projectSkippedNotice; internal/config/config.go — samePath; internal/agent/construct.go — sameFile; internal/config/layer_test.go — TestLayeredConfigWithoutAProjectLayerIsTheGlobalFileAlone; internal/config/config_test.go — TestApplyConfigSessionsWatchRefusals; internal/config/configsplice_windows_test.go — junctionConfigOrSkip
**Tests:** Windows: layer read through a junctioned root; a project `.apogee` that is the junctioned global home is not layered over itself; expected paths in long-name form. Linux: an existing-but-unresolvable file (item 3's seam forced) yields the skipped notice; a stat error other than not-exist (unreadable `.apogee/`, non-directory component) yields the skipped notice; a missing file yields none; `TestLayeredConfigWithoutAProjectLayerIsTheGlobalFileAlone` and `TestApplyConfigSessionsWatchRefusals` pass unchanged.
**Acceptance:**
- `GOOS=windows go vet ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestResolveOptions|TestReadProjectLayer|TestSameConfigFile|TestLayeredConfig|TestApplyConfigSessionsWatch' ./internal/config/`
**Commit:** `fix(config): read the project layer under a junction and announce an unreadable one`
Depends on items 1, 3.

## 5. Tier-1 project-config guard covers both root spellings

**What:**
**Goal:** a write to `.apogee/config.yaml` through the workspace's own (link or junction) spelling of a Project root above the workspace is refused by the Tier-1 project-config rule, as a write through the resolved spelling already is.
**Approach (assumed at the header base):** `security.projectRootSpellings` (rules.go) adds the root spelled through the workspace's unresolved prefix alongside the resolved one; producer is `domain.Config.ProjectRoot` via `agent/construct.go` `projectConfigRule`.
**Regression guard.** Acceptance runs one package per `go test -race` line — split internal/security and internal/agent. Derive the `../` climb from the resolved workspace too (`Rel(RealPath(ws), root)`, `..`-only), not only the absolute workspace-spelled root, so `../.apogee/config.yaml` from a workspace under a link is refused. Keep the workspace-spelled absolute root only when `platform.RealPath(candidate) == ProjectRoot` (else a linked workspace's `~/.apogee/config.yaml` loses its `write-apogee-control-plane` look); resolve in `agent/construct.go` `projectConfigRule` and pass it in — this yields to rules.go:289-290: the security package reads no disk. A changed `ProjectConfigRule` signature updates `projectConfigGuard` and the `ProjectConfigRule("", …)` call in rules_test.go.
**Files:** internal/security/rules.go, internal/security/rules_test.go, internal/agent/construct.go
**Read first:** internal/security/rules.go — projectRootSpellings, ProjectConfigRule; internal/agent/construct.go — projectConfigRule; cmd/apogee/wire.go — resolveRoots; internal/security/rules_test.go — projectConfigGuard, TestProjectConfigControlPlaneSpellsTheRootFromTheWorkspace, TestProjectConfigControlPlaneLeavesTheRestAlone; internal/agent/dispatch_test.go — TestProjectConfigControlPlaneJudgesTheRealTools
**Tests:** POSIX symlinked-workspace case: the rule matches both the resolved and the workspace-spelled `.apogee/config.yaml`, and `../.apogee/config.yaml` / `./../.apogee/config.yaml`, driven with the exact path string the write tool receives; a workspace that is itself a link keeps "the home config keeps its look" (`~/.apogee/config.yaml` → `write-apogee-control-plane`); the existing `TestProjectConfigControlPlane*` tests pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ProjectConfig|ProjectRootSpellings' ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ProjectConfig|ProjectRootSpellings' ./internal/agent/`
**Commit:** `fix(security): guard both spellings of a linked Project root`
Depends on item 2.

## 6. Windows CI runs the junction tests

**What:**
**Goal:** the `test-windows` job in `.github/workflows/ci.yml` runs `internal/platform`, `internal/projectroot`, `internal/adoption`, and the junction tests of `internal/config` (`-run`-filtered like the existing `internal/session` step).
**Approach (assumed at the header base):** add the packages to the job's test step; a separate `-run 'Junction|ProjectWrite|ReadProjectLayer'` step for `internal/config`.
**Regression guard.** Update every sentence that lists or counts the test-windows trees (`grep -n 'five trees\|three light trees\|windows-latest. job runs' .github/workflows/ci.yml docs/manual/building.md` — ci.yml's step comments and building.md's "runs the tests of five trees only" paragraph); add docs/manual/building.md to Files. Every `^func Test` in internal/config/*_windows_test.go matches the internal/config step's `-run` filter.
**Files:** .github/workflows/ci.yml, docs/manual/building.md
**Read first:** .github/workflows/ci.yml — test-windows job ("go test (race, remaining trees)", "go test (race, session Windows tests)"); docs/manual/building.md — windows-latest job paragraph; internal/config/configsplice_windows_test.go — TestWriteConfigAtomicallyWritesThroughAJunction; internal/adoption/store_test.go — TestRecordIsNamedByTheFullDigestOfTheResolvedRoot, TestRecordFileModesAreOwnerOnly; internal/projectroot/projectroot_test.go — TestResolveSymlinkedWorkspace, realTempDir
**Tests:** none (CI config).
**Acceptance:**
- `grep -n 'internal/projectroot' .github/workflows/ci.yml && grep -n 'internal/adoption' .github/workflows/ci.yml`
- `grep -n 'internal/config/' .github/workflows/ci.yml`
- every `grep -h '^func Test' internal/config/*_windows_test.go` name matches the internal/config step's `-run` filter
**Commit:** `ci: run the junction tests on Windows`
Depends on items 2, 3, 4.
**Closes:** junction-project-root

## 7. The open-link hint tells the truth per terminal

**What:**
**Goal:** the presented-document status line reads `cmd+click to open` under a known linkifying terminal (`TERM_PROGRAM` of `zed`, vscode, iTerm.app, WezTerm, or kitty via `TERM=xterm-kitty`), `drag to select and copy` under `TERM_PROGRAM=Apple_Terminal`, and `cmd+click to open · drag to copy` otherwise; the opened and degraded wordings are unchanged; the manual's §Showing a finished document says what Terminal.app users get.
**Approach (assumed at the header base):** a `LinkGesture(env func(string) string)` fact in `internal/present/detect.go` beside `Locality`/`HasDesktop`; computed once in `newModel` (like `notifyTmux`) and threaded to `presentedStatus` (transcript.go) via `renderPresentedBlock` (startupbox.go). Update the comments that quote the hint.
**Regression guard.** the manual paragraph also states that inside tmux (TERM_PROGRAM=tmux) and over plain SSH (TERM_PROGRAM not forwarded) the unknown wording `cmd+click to open · drag to copy` shows, whatever the outer terminal. Match the exact values — `TERM_PROGRAM` `zed` (Zed sets lowercase, ADR 0019 context), `vscode`, `iTerm.app`, `WezTerm`, or `TERM=xterm-kitty`. The gesture is a `transcript` field set in `newModel` beside `m.transcript.ws` and preserved across `reset` (never on the theme, which a scheme switch rebuilds); the gesture's zero value (unset — not computed, other Drivers, tests that don't set it) is the unknown case and renders `cmd+click to open · drag to copy`; no implementer choice remains. Every comment quoting the hint follows (`grep -rn 'cmd+click' internal docs/manual`); internal/config/defaults/config.yaml joins Files.
**Files:** internal/present/detect.go, internal/present/detect_test.go, internal/tui/transcript.go, internal/tui/startupbox.go, internal/tui/model.go, internal/tui/render.go, internal/tui/presenter_test.go, internal/config/defaults/config.yaml, docs/manual/configuration.md
**Read first:** internal/tui/transcript.go — presentedStatus, reset; internal/tui/startupbox.go — renderPresentedBlock; internal/tui/model.go — newModel (notifyTmux, transcript.ws); internal/present/detect.go — Locality, HasDesktop; internal/tui/presenter_test.go — TestPresentedEntryRendering; internal/tui/transcript_test.go — plainRender
**Tests:** `LinkGesture` table over each `TERM_PROGRAM`, a `TERM_PROGRAM=zed` row included; `TestPresentedEntryRendering` gains one case per gesture (each sets the transcript field), pinning the exact rendered status; the existing served case, which sets no field, pins the zero value's `cmd+click to open · drag to copy`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/present/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPresented' ./internal/tui/`
**Commit:** `fix(tui): say a gesture that works in the open-link hint per terminal`
**Closes:** terminal-app-click-hint

## 8. Repo-local gpg.ssh.defaultKeyCommand is refused

**What:**
**Goal:** a repository whose local config sets `gpg.ssh.defaultKeyCommand` is refused by the git command-config denylist like every other command-running key, for both the git tool and the git actions host (the Rebase action's `rebase @{u}` included). Fixes an unconfined command run found in the 2026-11-10 audit follow-up.
**Approach (assumed at the header base):** add the key to `security.GitCommandConfigNameSource` (shellwrites.go); correct the `gitTransportConfig`/`gitFetchArgs` comment in cmd/apogee/gitstatus_host.go that says the refusal does not cover `gitProxy`.
**Regression guard.** Acceptance runs one package per `go test -race` line — split internal/tools, internal/gitexec, internal/security. The git-actions case drives `gitActionsHost.Rebase` (Sync is fetch + `merge --ff-only` then push and makes no commit) after `identifyFixture` + `divergeFromUpstream`, then plants the gpgsign/format/keycommand keys, and asserts Rebase returns the command-config refusal naming `gpg.ssh.defaultkeycommand` as well as that `./x` never ran — gitexec keeps HOME, so without that assertion the bite passes silently on a box with no global signing key.
**Files:** internal/security/shellwrites.go, internal/tools/git_test.go, internal/gitexec/gitexec_test.go, cmd/apogee/gitstatus_host.go, cmd/apogee/gitactions_host_test.go
**Read first:** internal/security/shellwrites.go — GitCommandConfigNameSource; internal/gitexec/gitexec.go — CommandConfigName, safeEnvKeys; cmd/apogee/gitactions_host.go — gitActionsHost.Rebase; cmd/apogee/gitstatus_host.go — gitTransportConfig; cmd/apogee/gitactions_host_test.go — identifyFixture, divergeFromUpstream; internal/tools/git_test.go — TestGit_RefusesRepoLocalCommandConfig
**Tests:** a `TestGit_RefusesRepoLocalCommandConfig` case for the key; a `TestGitActionsHost…` Rebase case (diverged fixture) with a planted `commit.gpgsign=true` + `gpg.format=ssh` + `gpg.ssh.defaultKeyCommand=./x` asserting the refusal names `gpg.ssh.defaultkeycommand` and `./x` never runs.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'RefusesRepoLocalCommandConfig|CommandConfig' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'RefusesRepoLocalCommandConfig|CommandConfig' ./internal/gitexec/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'RefusesRepoLocalCommandConfig|CommandConfig' ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitActionsHost' ./cmd/apogee/`
**Commit:** `fix(security): refuse a repo-local gpg.ssh.defaultKeyCommand`
**Closes:** git-ssh-keycommand-denylist

## 9. The adoption pane says what the pin does not cover

**What:**
**Goal:** every adoption prompt for a repo MCP server or reaction shows the dim line `not pinned: anything it reads or runs beyond these` under `pinned files:`, or on its own when no file is pinned, in the TUI pane and the CLI adopt render; `docs/layout/user-questions-layout.md` §Adoption pane specifies it; ADR 0104 carries a dated amendment recording the disclosure and rejecting manifest pinning and root-tree hashing.
**Approach (assumed at the header base):** extend the existing pane renderer and the CLI adopt render that list `pinned files:`; ADR amendment via `capcom know add --id adr-0104-amend-1113 --parent adr-0104`, named on the record's Status line and reading-order list.
**Regression guard.** Acceptance runs one package per `go test -race` line — split internal/tui and cmd/apogee (two heavy packages on one line are unsupported per AGENTS.md). The line shows on every server and reaction pane (`isStructured`, internal/tui/adoption.go) — "spawns a process" is not checkable, since an `endpoint:` server or a `gate:`-only reaction spawns nothing — and on no Allow rule. Faint comes from a `popupSpec` hook applied after the wrap (the body stays plain for the ANSI-unaware wrap); the CLI prints the line plain, with no SGR. `TestAdoptionPaneShowsARepoServerBody`, `TestProjectCmdAdoptsARepoReaction`, the layout doc's 80/50-column mockups and its sentence "An entry that names no project file draws no such part" are updated to the new run.
**Files:** internal/tui/adoptionbody.go, internal/tui/adoptionbody_test.go, internal/tui/popup.go, internal/tui/picker.go, cmd/apogee/project_cmd.go, cmd/apogee/project_cmd_test.go, docs/layout/user-questions-layout.md, .capcom/knowledge/decisions/adr-0104.md, .capcom/knowledge/decisions/adr-0104/adr-0104-amend-1113.md
**Read first:** internal/tui/adoptionbody.go — proposalBodyLines, AdoptionQuestion; internal/tui/popup.go — popupBodyLines; internal/tui/picker.go — pickerAdoption spec; cmd/apogee/project_cmd.go — printProjectQuestion; internal/tui/adoptionbody_test.go — TestAdoptionPaneShowsARepoServerBody; cmd/apogee/project_cmd_test.go — TestProjectCmdAdoptsARepoReaction; docs/layout/user-questions-layout.md — §Adoption pane The body
**Tests:** pane render test pinning the exact line with and without pinned files, and its faint styling after the wrap; an Allow-rule pane shows no line; CLI adopt output test pinning the line plain (no SGR); `TestAdoptionPaneShowsARepoServerBody` and `TestProjectCmdAdoptsARepoReaction` updated to the new run.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Adopt' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Adopt' ./cmd/apogee/`
- `capcom check`
**Commit:** `fix(tui): disclose what the adoption pin leaves unpinned`
**Closes:** adoption-pane-unpinned-reads

## 10. The staged-binary version check waits on a slow box

**What:**
**Goal:** `apogee update` gives the downloaded binary's `--version` check a 5-minute backstop, reports a timeout as `the downloaded binary did not answer --version within 5m0s; the installed binary is unchanged`, and a held output pipe cannot outlive the deadline. Fixes a slow-box violation (a cold first exec under load failed the update as "did not run").
**Approach (assumed at the header base):** `stagedVersionTimeout` in cmd/apogee/update.go becomes an injectable `updateDeps` field (default 5 min); `verifyStagedVersion` sets `cmd.WaitDelay` and distinguishes `context.DeadlineExceeded`.
**Regression guard.** Set the new timeout field in `archiveUpdateDeps` (or have `verifyStagedVersion` treat zero as the 5-minute default) — a zero `context.WithTimeout` fails the existing archive-install tests at Start. Detect the timeout from the inner ctx's `Err() == context.DeadlineExceeded`, not from exec's error. The new test runs serially like its siblings (ETXTBSY), lowers the timeout with `testtime.Fixed` (never `Budget`), stages a stub that forks a child holding stdout (`sleep 30 & wait`), and asserts the update returns well before that sleep ends.
**Files:** cmd/apogee/update.go, cmd/apogee/update_test.go
**Read first:** cmd/apogee/update.go — verifyStagedVersion, updateDeps, stagedVersionTimeout; cmd/apogee/update_test.go — archiveUpdateDeps, stubReleaseBinary, TestUpdate_StagedVersionMismatch_KeepsInstalledBinary; internal/testtime/testtime.go — Fixed, Budget
**Tests:** a serial test stages a script that forks a stdout-holding child (`sleep 30 & wait`) past a timeout lowered with `testtime.Fixed`, and asserts the exact timeout message, that the update returns well before the sleep ends, and that the installed binary is untouched; existing `TestUpdate_ArchiveWithYes_ReplacesExecutable`, `TestUpdate_TerminalAnswer_DecidesTheUpdate` and `TestUpdate_StagedVersionMismatch_KeepsInstalledBinary` still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestUpdate_' ./cmd/apogee/`
**Commit:** `fix(update): give the staged version check a slow-box backstop`
**Closes:** update-version-check-deadline

## 11. MCP discovery announces a cut-short tool list

**What:**
**Goal:** when tool discovery for one server stops at the page cap or tool cap, or skips tools whose schema exceeds the byte cap, the client reports it and apogee shows one startup/reload notice per server naming the server and each cause (`stopped at 64 pages`, `stopped at 512 tools`, `skipped <n> tools with a schema over 64 KiB`); discovery itself is unchanged.
**Approach (assumed at the header base):** `listServerTools` (internal/mcp/client.go) records truncation; a `Client.Truncated()` report beside `Client.Skipped()`; announced from `connectMCPServers` next to `announceSkippedMCP` (cmd/apogee/mcp_cmd.go); the `wire.go` client interface and its test doubles gain the method. Rewrite the const-block comment that says truncation is silent.
**Regression guard.** Report the tool cap only when another tool remains on the page or `NextCursor` is non-empty, and the page cap only when the 64th page's `NextCursor` is non-empty (an exactly-512-tool server reports nothing). Announce via `w.announce` from `connectMCPServers` only; a reload truncation rides that server's `mcpOutcome` note (a clause on `mcpLanded`), superseding ADR 0109 decision 8's fixed note set (and its 2026-11-07 amendment) — record it in a dated amendment. Rewrite every prose calling discovery truncation silent (`grep -rn -i 'silent' internal/mcp/client.go docs/design/mcp-client.md`; supersedes docs/design/mcp-client.md:129-131). `fakeMCPSession` gains a `truncated` field and the method; `listFromInProcessServer` (or a sibling) returns the report.
**Files:** internal/mcp/client.go, internal/mcp/mcp_test.go, cmd/apogee/mcp_cmd.go, cmd/apogee/wire.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire_helpers_test.go, cmd/apogee/mcp_cmd_test.go, docs/design/mcp-client.md, .capcom/knowledge/decisions/adr-0109.md, .capcom/knowledge/decisions/adr-0109/adr-0109-amend-1113.md
**Read first:** internal/mcp/client.go — listServerTools, maxMCPToolListPages const block; internal/mcp/mcp_test.go — TestListServerTools_CapsPagesAndTools, listFromInProcessServer; cmd/apogee/mcp_cmd.go — connectMCPServers; cmd/apogee/wire.go — mcpSession; cmd/apogee/wire_helpers_test.go — fakeMCPSession; cmd/apogee/wire_mcp.go — mcpLanded
**Tests:** a fake server past each cap yields the matching report entry, asserted in the existing `TestListServerTools_CapsPagesAndTools` subtests; an exactly-512-tool server yields no report; the wiring test asserts the exact startup notice text; a reload truncation's `mcpLanded` note carries the clause.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'MCP' ./cmd/apogee/`
- `capcom check`
**Commit:** `feat(mcp): announce a tool list cut short by a discovery cap`
**Closes:** mcp-discovery-cap-silent

## 12. Manual corrections from the refocus

**What:**
**Goal:** `docs/manual/configuration.md` names nineteen next-start keys wherever it counts them; `docs/manual/commands.md` describes `/settings` as covering some ninety keys; `docs/manual/updating.md` quotes the full `--check` line including `— run: apogee update`; configuration.md's MCP fixed-limits paragraph lists the page, tool and schema caps and says apogee announces a cut-short list.
**Approach (assumed at the header base):** edit configuration.md (~:2165 "seventeen", ~:912 MCP "Fixed limits"), commands.md (~:735 "sixty-odd"), updating.md (~:64). Quote the `--check` line from the code that prints it (cmd/apogee/update.go).
**Regression guard.** the MCP fixed-limits paragraph quotes item 11's notice clauses exactly as the code emits them after item 11 lands (read them from internal/mcp / cmd/apogee/mcp_cmd.go, not from this plan)
**Files:** docs/manual/configuration.md, docs/manual/commands.md, docs/manual/updating.md
**Read first:** docs/manual/configuration.md — "Nineteen /settings keys" next-start list, "seventeen keys" in the tool-call-format paragraph, MCP "Fixed limits" paragraph; docs/manual/commands.md — /settings "sixty-odd keys"; docs/manual/updating.md — `--check` reports sentence; cmd/apogee/update.go — runUpdate --check Fprintf; internal/config/registry.go — KeyRegistry (90 Paths); cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs); any drift test pinning these counts must still pass.
**Acceptance:**
- `! grep -n 'seventeen' docs/manual/configuration.md && ! grep -n 'sixty-odd' docs/manual/commands.md && grep -n 'run: apogee update' docs/manual/updating.md && grep -n '512' docs/manual/configuration.md`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Drift|Doc' ./cmd/apogee/`
**Commit:** `docs(manual): correct stale counts, the --check line and the MCP caps`
Depends on item 11.
**Closes:** manual-count-drift
