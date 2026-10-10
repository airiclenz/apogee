# Windows package channels, boot update notice and `apogee update` — plan

**Goal:** Windows users install apogee with `scoop` or `winget`, published by repo scripts that `/cut-release` calls. The interactive TUI checks for a newer release asynchronously on every boot and names the right upgrade command for the install method on the startup box's version row. `apogee update` self-replaces raw-archive installs and refuses everything else.
**Date:** 2026-10-10
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** da4d6a43

**Sources:**
- docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md
- docs/plans/archived/2026-07-23 - 05 - startup-box-release-version-plan.md
- docs/manual/building.md (Releasing), scripts/release-smoke.sh, Makefile (`dist`)
- bead apogee-3p1 (unsigned binaries — context, not closed here)

**Ratified design calls** (owner, 2026-10-10):
- **No silent auto-update:** a notice + an explicit `apogee update`; nothing replaces the binary in the background.
- **Default:** the check is on; `update-check: false` or `APOGEE_NO_UPDATE_CHECK` (any non-empty value) disables it; offline fails silently.
- **Source:** `HEAD https://github.com/airiclenz/apogee/releases/latest`, tag read from the redirect `Location` (published releases only; no API rate limit).
- **Frequency:** every boot, async, never delays boot; no cache file.
- **Notice:** inline on the version row — `v0.24.11 → v0.25.0 · brew upgrade apogee`.
- **Drivers:** interactive TUI only; headless, daemon, bench and probe never check.
- **`apogee update` on managed installs:** refuse, print the channel's command, exit non-zero; never run a package manager.
- **`apogee update` on archive installs:** prompt `[y/N]`, `--yes` skips; verify against the release's SHA256SUMS; run the new binary's `--version` before the swap; keep the old binary until success.
- **Automation:** repo scripts + make targets; `/cut-release` (a personal skill outside the repo) calls them.
- **Names:** Scoop bucket `airiclenz/scoop-bucket`; winget id `AiricLenz.Apogee`; winget PRs submitted with `komac`.
- **Install detection (writer's call):** `make dist` stamps release binaries; an unstamped local build is "source" and is told `git pull && make install`.
- **/settings override:** an APOGEE_NO_UPDATE_CHECK override shows as false (env) in /settings. (owner, 2026-10-10)

**Standing requirements:**
- skills: coding-standards
- The engine stays wire-silent (ADR 0031): all network code lives in `internal/update`, reached only from `cmd/apogee`.
- `internal/tui` never imports `internal/update` or the root package; it sees a host interface on `tui.Options`.

**Regression check (2026-10-10, da4d6a43):**
- 3: guard folded (Acceptance stamp check under `-trimpath`; Scoop segment match; machine-scope winget path).
- 4: recast (owner decision: the hand-coded `APOGEE_NO_UPDATE_CHECK` override shows in /settings as false with the env marker) + guard folded (Environment overrides section Eight→Nine; per-key fixture tables; hand-coded env override, no `EnvVar`).
- 5: guard folded (result lives on `m.opts`, reached by all six `newStartupView` callers).
- 6: guard folded (writer decision: suite-wide `APOGEE_NO_UPDATE_CHECK=1`, one detection-input seam; plus verified reviewer guards: `ptyEnv`, pure gate function).
- 8: guard folded (writer decision: reuse item 6's seam; plus verified reviewer guards: deps struct, sweep after `maybeDispatchConfinedExec`).
- 9: guard folded (writer decision: exec bit + LF + index-mode assertion; plus verified reviewer guards: offline `DRY_RUN`, GitHub homepage).
- 10: guard folded (writer decision: exec bit + LF + index-mode assertion; plus verified reviewer guard: `DRY_RUN` skips komac and token lookup).
- 11: guard folded (writer decision: README Documentation row; plus verified reviewer guards: smoke on non-latest tags, README channel count, release-smoke prose sites).
- 4 (re-check): guard folded (marker recorded in `overrideSources`/`ApplyConfig` + `settingSource` names `EnvNoUpdateCheck`; fixture-driven settingsrows test; counting/"three groups" prose sites incl. e2e_newcomer_test.go; template-order and default-config gates) + yields to ADR 0043 (exception named in the override's comment).
- 6 (re-check): guard folded (writer decision: extend `headlessRunUnderEnv` and demorig's `ambientApogeeEnv` lists; injected getenv only in settings rows).

**Out of scope:**
- Code signing (apogee-3p1); a custom MSI/EXE installer; CI-cut releases.
- Editing `~/.claude/skills/cut-release` (owner follow-up: call `make release-scoop` and `make release-winget` after the tap step).
- The one-time manual setup: create `airiclenz/scoop-bucket`, fork `microsoft/winget-pkgs`, the first `komac new` submission (documented in item 11, never run by an agent).
- Caching the check result; an in-chat `/update` command.

## 1. ADR: the TUI's one first-party request, and the install channels — ✅ DONE (2026-10-10)

NOTES (2026-10-10): re-derived from the ADR number 0096 — that number was taken by `0096-project-config-grants-are-live-only-by-adoption.md` (commit 9d16189c) after the plan was written, so the ADR is `docs/adr/0097-the-tui-checks-for-a-newer-release-and-names-the-upgrade-command.md`; the item's Acceptance `test -f docs/adr/0096-*.md` passes only on the old ADR, so the verifier should check `test -f docs/adr/0097-*.md`, and later items citing "ADR 0096" for this decision mean 0097.

NOTES (2026-10-10): README links `docs/manual/configuration.md#update-check`, an anchor that exists only once item 4 adds the `### update-check` heading (as the plan's Read first line anticipates).

**What:**
**Goal:** `docs/adr/0096-*.md` records every ratified call above (update check, `apogee update` scope, channel matrix, detection rule). The README's "works offline" sentence states that the TUI makes one release check to github.com, which can be disabled.
**Approach (assumed at the header base):** new ADR in the house front-matter shape (`Status: accepted`), sections Context / Decision / Consequences. It states that the check is a Driver concern that ADR 0031's wire-silent engine permits. Amend the README sentence that contains "works offline" in place and link `docs/manual/configuration.md#update-check`.
**Files:** docs/adr/0096-the-tui-checks-for-a-newer-release-and-names-the-upgrade-command.md; README.md
**Read first:** README.md — intro paragraph ("no API key, no cloud, works offline"); docs/adr/0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md — door-keeping invariants 1 (wire-silent) and 3 (no first-party connectors); docs/adr/0095-mcp-oauth-tokens-are-persisted-by-apogee.md — front-matter shape (Status/Amends); docs/manual/configuration.md — heading slugs (the `#update-check` anchor exists only once item 4 adds a `### update-check` heading)
**Tests:** none (docs only).
**Acceptance:** `test -f docs/adr/0096-*.md`; `grep -n "update-check" README.md`
**Commit:** `docs(adr): record the boot update check and the package channels`

## 2. `internal/update`: latest-release lookup and version ordering — ✅ DONE (2026-10-10)

NOTES (2026-10-10): `Client` is built with `NewClient(baseURL)` (fields unexported, zero value refused by `Latest`); the package doc lives in latest.go rather than a separate doc.go, keeping to the item's four named files.

NOTES (2026-10-10): `Newer` drops semver build metadata (`+…`) before comparing, so the full `Version()` string orders like `BaseVersion()`; `Latest` itself accepts only an exact `vX.Y.Z` final segment under `/releases/tag/` (pre-release, build-metadata and `/releases` index redirects are errors).

**What:**
**Goal:** package `internal/update` exports `Latest(ctx, Client) (string, error)` and `Newer(current, latest string) bool`. `Latest` returns the `vX.Y.Z` tag of the newest published release. `Newer` orders 0.x semver numerically and treats any version in `[v1.0.0, v1.8.0]` (the retracted series, `go.mod`) or a malformed one as not newer.
**Approach (assumed at the header base):**
- `Client` holds a base URL (default `https://github.com/airiclenz/apogee`) and an `*http.Client` with `CheckRedirect` returning `http.ErrUseLastResponse`, `Proxy: http.ProxyFromEnvironment` and a 5 s timeout.
- `Latest` sends `HEAD <base>/releases/latest`, expects a 3xx, and parses the last path segment of `Location` (`/releases/tag/vX.Y.Z`). Anything else is an error.
- No dependency on `internal/tui` or the root package. It does not go through `security.URLGuard`: that guards model-driven requests, and this one is apogee's own fixed URL.
**Files:** internal/update/latest.go; internal/update/version.go; internal/update/latest_test.go; internal/update/version_test.go
**Read first:** go.mod — retract [v1.0.0, v1.8.0]; version.go — BaseVersion (VERSION reads `v0.24.11`); Makefile — check target's ADR-0010 grep (internal/ must not import the root module)
**Tests:** httptest server: a 302 to `/releases/tag/v0.25.0` gives `v0.25.0`; a 200, a 404, a bad Location and a timeout give errors. Table for `Newer`: `v0.24.11 < v0.25.0`, `v0.9.0 < v0.10.0`, equal → false, `v1.2.0` → false, `dev` → false.
**Acceptance:** `go test -race -count=1 ./internal/update/`
**Commit:** `feat(update): look up the latest published release`

## 3. Install-method detection and the dist stamp — ✅ DONE (2026-10-10)

NOTES (2026-10-10): `DistBuild()` returns a package-level `isDistBuild = distBuild != ""` computed at initialisation rather than reading `distBuild` itself: with no caller of `DistBuild` linked into cmd/apogee yet (item 6 wires it), the linker dead-code-eliminated the unread `-X` variable and `make dist` binaries carried no `release-archive` string (grep count 0); the init-time read keeps the stamp in every binary (count 1 on all four tar.gz binaries, 0 on a local `go build`).

NOTES (2026-10-10): the Makefile gains `DIST_LDFLAGS := $(GO_LDFLAGS) -X $(MODULE).distBuild=release-archive`, used by the `dist` recipe only; `build`/`run`/`install`/`cross` keep `GO_LDFLAGS`. `UpgradeCommand` returns "" for an out-of-range Method; a test reads go.mod's `module` line so the GoInstall command cannot drift from the module path.

NOTES (2026-10-10): consequential edit — version.go: the versionFile doc said a release build injects "one" -ldflags provenance value; it now names both buildCount and distBuild.

**What:**
**Goal:** `update.Detect(Inputs) Method` classifies a binary as Homebrew, Scoop, Winget, GoInstall, Source or Archive. `Method.UpgradeCommand()` returns exactly: `brew upgrade apogee`, `scoop update apogee`, `winget upgrade AiricLenz.Apogee`, `go install github.com/airiclenz/apogee/cmd/apogee@latest` (the module's real cmd path, read from `go.mod`), `git pull && make install`, `apogee update`. Release archives from `make dist` carry a stamp that local builds lack.
**Approach (assumed at the header base):**
- `Inputs{ExePath string; DistBuild bool; HasVCS bool; ScoopRoot string}` keeps `Detect` pure.
- Path checks use a lower-cased, forward-slash form of the symlink-resolved exe path, first match wins:
  1. `/cellar/apogee/` → Homebrew
  2. `<ScoopRoot or ~/scoop>/apps/apogee/` → Scoop
  3. `/microsoft/winget/packages/airiclenz.apogee_` → Winget
  4. otherwise `!HasVCS` (BuildInfo without `vcs.revision`) → GoInstall
  5. otherwise `!DistBuild` → Source
  6. otherwise → Archive
- Root `version.go` gains an unexported `distBuild string`, set by ldflags, with an exported `DistBuild() bool`. The Makefile `dist` recipe appends `-X $(MODULE).distBuild=1` to its own ldflags only; `GO_LDFLAGS` for `build`/`install` stays unchanged.

**Regression guard.** `make dist` builds with `-trimpath` (Makefile:297), which drops `-ldflags` from build info, so `go version -m … | grep distBuild` can never match: stamp a distinctive value (`distBuild=release-archive`) and check it with `grep -ac release-archive` on the host-arch binary, keeping `grep -n distBuild Makefile`. Rule 2 matches the `/scoop/apps/apogee/` segment anywhere in the path (after the `ScoopRoot` prefix check), so `Detect` stays pure with no home lookup and global `C:\ProgramData\scoop` installs match. Rule 3 matches `/winget/packages/airiclenz.apogee_` with no `microsoft/` prefix, so machine-scope installs under `C:\Program Files\WinGet\Packages\` are Winget, never Archive.
**Files:** internal/update/detect.go; internal/update/detect_test.go; version.go; version_internal_test.go; Makefile
**Read first:** version.go — buildCount, BaseVersion, buildMetadata; version_internal_test.go — TestBuildMetadata; Makefile — GO_LDFLAGS, MODULE, dist recipe; scripts/release-smoke.sh — binary_revision, binary_modified; README.md — "From source" line (`go install …@latest`, `@main`, `@<sha>` all work)
**Tests:** a table over Unix and Windows-shaped paths (`C:\Users\x\scoop\apps\apogee\current\apogee.exe` with empty `ScoopRoot`, `C:\ProgramData\scoop\apps\apogee\current\apogee.exe`, `C:\Users\x\AppData\Local\Microsoft\WinGet\Packages\AiricLenz.Apogee_Microsoft.Winget.Source_8wekyb3d8bbwe\apogee.exe`, `C:\Program Files\WinGet\Packages\AiricLenz.Apogee_Microsoft.Winget.Source_8wekyb3d8bbwe\apogee.exe`, `/opt/homebrew/Cellar/apogee/0.24.11/bin/apogee`, `/home/linuxbrew/.linuxbrew/Cellar/apogee/...`), each Method's command string, and `DistBuild()` false when unstamped.
**Acceptance:** `go test -race -count=1 ./internal/update/ .`; `grep -n distBuild Makefile`; `make dist` then `grep -ac release-archive` on the host-arch archive's binary (non-zero count).
**Commit:** `feat(update): detect how apogee was installed`

## 4. Config key `update-check` — ✅ DONE (2026-10-10)

NOTES (2026-10-10): the env override is a named helper, applyNoUpdateCheck, called in ResolveOptions right after applyEnv; its marker is written in overrideSources (so both the unit and ApplyConfig paths record it), sharing one unexported updateCheckPath const. The helper's comment names it the one ADR 0043 exception, and so does registry.go's Key doc.

NOTES (2026-10-10): the row is Editable: false ("not live-editable"), so /settings carries the $EDITOR pointer for it; "update-check" was added to TestSettingsRowsPointReadOnlyKeysAtTheirEditor's hand-written list of externally edited keys, and the settingsrows fixture sets UpdateCheck false so the value row is not the default.

NOTES (2026-10-10): the starter template ships `update-check: true` as an active line, after remember-model, and the registry row sits at that same position. The template and manual prose deliberately leave out `apogee update`, which item 8 has not shipped yet.

NOTES (2026-10-10): consequential edit — docs/manual/configuration.md (opening paragraph): "Every other key is file-only (no flag or env)" now names update-check as the exception, made necessary by the new env override.

**What:**
Recast at the regression check (2026-10-10).
**Goal:** `update-check` (bool, default `true`) is a registry-backed setting, documented under `docs/manual/configuration.md#update-check` and in the starter config. `APOGEE_NO_UPDATE_CHECK` set to any non-empty value forces it off.
**Approach (assumed at the header base):** a `*bool` field with yaml tag `update-check` on `fileConfig`, a `bool` on `config.Options`, and a registry row (`KindBool`, Default `"true"`, not live-editable, since it is read once at boot). The env override goes into `ResolveOptions` beside the existing `APOGEE_*` handling. The doc entry says what is requested (one HEAD to github.com per TUI boot) and how to switch it off.

**Regression guard.** The registry row carries no `EnvVar`/`fromEnv` (applyEnv would parse `APOGEE_NO_UPDATE_CHECK=1` as a bool and turn the check ON, and `TestMultiSourceKeysReadTheRegistry` would fail); the override is hand-coded in `ResolveOptions` after `applyEnv`, and `/settings` must still report it: when APOGEE_NO_UPDATE_CHECK disables the check, /settings shows `update-check` as false with the env override marker (extend settingSource / the settings rows so this hand-coded override is reported as an env source, labelled `APOGEE_NO_UPDATE_CHECK` since the row has no `k.EnvVar`) — owner decision, 2026-10-10. `docs/manual/configuration.md`'s "## Environment overrides" names the variable beside the existing groups and its opener "Eight `APOGEE_*` variables" becomes "Nine" (`TestManualListsEveryEnvironmentOverride`). Fixtures: `owns["update-check"]="UpdateCheck"` (registry_test.go), a settingsrows `want` entry, and config_test.go's `wantDefaults` (`UpdateCheck: true`), `everyKeyFileConfig` (`UpdateCheck: boolptr(false)`) and `TestEveryConfigKeyReachesTheOptions`' want map.
Also (re-check guards, verified): `opts.Overrides` is built only by `overrideSources` (called in `ApplyConfig`, not `ResolveOptions`) and `settingSource` reads only that map plus `k.EnvVar`, so config records `Overrides["update-check"]=SourceEnv` when `getenv(EnvNoUpdateCheck)!=""` (in `overrideSources`, or in `ApplyConfig` right after it), and `settingSource` names `config.EnvNoUpdateCheck` for that row when `k.EnvVar==""`; `settingsRows` stays pure over its `Options`. Update every site that counts the `APOGEE_*` variables or names how they are read (`grep -rniE "eight .?APOGEE|three groups|overrideSources (range|ranges)" cmd internal docs/adr`: e2e_newcomer_test.go's "eight `APOGEE_*` variables" rubric, config.go's Env-const "three groups" doc, registry.go's Key doc). The registry row sits at the template line's first-mention position; the template line ships commented or as `update-check: true`. ADR 0043 (docs/adr/0043-files-split-by-concern-and-config-gets-a-package.md:344-347 — the row is the one table resolution reads) stands: the item yields to it, and the hand-coded override's comment names itself the one exception, citing ADR 0043.
**Files:** internal/config/config.go; internal/config/options.go; internal/config/registry.go; internal/config/defaults/config.yaml; internal/config/config_test.go; internal/config/registry_test.go; cmd/apogee/settingsrows.go; cmd/apogee/settingsrows_test.go; cmd/apogee/e2e_newcomer_test.go; docs/manual/configuration.md
**Read first:** internal/config/config.go — overrideSources, ApplyConfig, ResolveOptions, applyEnv, Env consts; cmd/apogee/settingsrows.go — settingSource, settingsRows; cmd/apogee/settingsrows_test.go — fabricatedSettings, TestSettingsRowsMarkOverriddenKeys, TestSettingsRowsFormatEffectiveValues; internal/config/config_test.go — TestApplyConfigRecordsOverrideSources, resolveSources, wantDefaults, TestMultiSourceKeysReadTheRegistry;
internal/config/defaults_test.go — TestRegistryFollowsTheTemplateOrder, TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride, environmentVariablesRead
**Tests:** default true; file `false` gives false; the env var overrides the file `true`, tested through `ResolveOptions`/`ApplyConfig` (not `resolveSources`, which runs `applyEnv` only); a case in `TestOverrideSourcesNameTheWinningSource`/`TestApplyConfigRecordsOverrideSources`: with `APOGEE_NO_UPDATE_CHECK` set, `Overrides["update-check"]==SourceEnv`; a settingsrows test: a fixture whose `Overrides` carries `"update-check": config.SourceEnv` gives the `update-check` row the env source marker with `SourceName==config.EnvNoUpdateCheck`; the existing gates `TestRegistryIsBijectionWithFileConfig`, `TestRegistryRowInvariants`, `TestManualDocumentsEverySettingsKey`, `TestManualListsEveryEnvironmentOverride`, `TestRegistrySetIsTheInverseOfRead`, `TestSettingsRowsFormatEffectiveValues`, `TestEveryConfigKeyReachesTheOptions`, `TestMultiSourceKeysReadTheRegistry`, `TestRegistryFollowsTheTemplateOrder` and `TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` pass.
**Acceptance:** `go test -race -count=1 ./internal/config/`; `go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestManualListsEveryEnvironmentOverride|SettingsRows' ./cmd/apogee/`
**Commit:** `feat(config): add the update-check setting`

## 5. TUI: show the update notice on the version row — ✅ DONE (2026-10-10)

NOTES (2026-10-10): consequential edit — internal/tui/doc.go: made necessary by the new update_check.go (the package file map TestDocMapNamesEveryFile enforces).

NOTES (2026-10-10): the UpdateHost interface sits in tui.go beside RecallHost; the landed result is an unexported Options.updateNotice field (the plan's "field on m.opts"), composed by startupVersion; a host answer with ok=true but no tag, or an empty BaseVersion, leaves the version value bare.

NOTES (2026-10-10): the CHANGELOG entry describes the surface as shipped once item 6 wires the host; nothing calls Options.Update until then, so item 6 should not add a second entry for the notice itself.

**What:**
**Goal:** when a host on `tui.Options` reports a newer release, the startup box's version value reads `<current> → <latest> · <command>` (U+2192, U+00B7) in both wide and stacked layouts. It survives `/clear`'s re-seed. With no host, or when the host returns nothing, the box is byte-identical to today's.
**Approach (assumed at the header base):**
- `type UpdateHost interface { CheckForUpdate(ctx context.Context) (latest, command string, ok bool) }` and a field `Update UpdateHost` on `tui.Options`.
- `Init`'s `tea.Batch` gains `m.updateCheckCmd()`, which returns nil when the host is nil and otherwise sends an `updateCheckedMsg`.
- The fold stores the result on the model and calls `transcript.refreshStartup(newStartupView(...))`. `newStartupView` (also used by `startNewSession`) composes the version value from `opts.BaseVersion` plus the stored result.
- Narrow terminals rely on the existing stacked truncation; the wide layout falls back to stacked when the longer value no longer fits (existing width rule).

**Regression guard.** `newStartupView(opts Options)` has six callers (newModel, resetSessionView, resumeLoaded, applyRebind, foldServerSwitch, foldServerBind — `grep -n newStartupView internal/tui`), so the fold stores the result where `newStartupView` already reads — a field on `m.opts` — and every caller keeps the notice; a result held only on the model would be wiped by the cold start's first heartbeat (`applyRebind`→`refreshStartup`) or a `/resume`.
**Files:** internal/tui/tui.go; internal/tui/model.go; internal/tui/update_check.go; internal/tui/startupbox_test.go; internal/tui/update_check_test.go
**Read first:** internal/tui/model.go — newStartupView, Model.Init, newModel; internal/tui/transcript.go — refreshStartup, addStartup; internal/tui/startupbox.go — renderStartupBox, renderStartupStacked; internal/tui/heartbeat.go — applyRebind, foldServerSwitch; internal/tui/prebound.go — foldServerBind; internal/tui/sessions.go — resumeLoaded
**Tests:**
- A fake host gives the version value `v0.24.11 → v0.25.0 · scoop update apogee` in a rendered frame.
- A nil host, or `ok=false`, leaves the frame identical to the pinned `TestRenderStartupBox` goldens.
- `/clear` keeps the notice.
- The notice survives an `applyRebind` (heartbeat rebind) and a server switch.
- Stacked width truncates with "…".
**Acceptance:** `go test -race -count=1 -run 'Startup|UpdateCheck' ./internal/tui/`
**Commit:** `feat(tui): name the upgrade command when a newer release exists`

## 6. Wire the update host into the interactive TUI only — ✅ DONE (2026-10-10)

NOTES (2026-10-10): consequential edit — cmd/apogee/doc.go: made necessary by the new wire_update.go (the composition root's file map TestDocMapNamesEveryFile enforces).

NOTES (2026-10-10): the gate is wired as updateHostFor(updateCheck, baseVersion, fullVersion) over the pure updateCheckEnabled; options() passes w.opts.UpdateCheck (APOGEE_NO_UPDATE_CHECK already forced off by config.ApplyConfig) with apogee.BaseVersion()/Version(). The host captures the updateBaseURL/installInputs package seams at construction; tests that swap them are serial and restore via t.Cleanup.

NOTES (2026-10-10): demorig's apogeeEnv now strips APOGEE_NO_UPDATE_CHECK with the other overrides and appends APOGEE_NO_UPDATE_CHECK=1, so a take never asks and never paints the notice; TestApogeeEnvStripsEveryApogeeOverride now expects that trailing entry.

**Depends on items 2, 3, 4, 5.**
**What:**
**Goal:** the root TUI run passes a non-nil `tui.Options.Update` only when `update-check` is true, `APOGEE_NO_UPDATE_CHECK` is unset, and `apogee.BaseVersion()` is a release (not `dev`, and `apogee.Version()` not `.dirty`). Headless, daemon, probe and undo never construct one.
**Approach (assumed at the header base):** a `cmd/apogee/wire_update.go` adapter implements `tui.UpdateHost`. It calls `update.Latest`, then `update.Newer(apogee.BaseVersion(), latest)`, then `update.Detect(...)` with `os.Executable` + `filepath.EvalSymlinks`, `apogee.DistBuild()`, BuildInfo `vcs.revision` presence and `$SCOOP`. Any error gives `ok=false`. `(*rootWiring).options()` sets the field. The base URL is a package var that tests point at an httptest server.

**Regression guard.** No test or demo take ever sends a live request — add APOGEE_NO_UPDATE_CHECK=1 to the ambient-env handling in cmd/apogee/main_test.go (ambientApogeeEnv / driven-test env) and to cmd/demorig/record.go's environment, and add both paths to Files; detection inputs (exe path, DistBuild, HasVCS, ScoopRoot) are read through one injectable package-level seam in cmd/apogee so tests supply a Method without real build stamps.
Also (reviewer guards, verified): `TestAmbientApogeeConfigIsClearedForTheSuite` asserts every `ambientApogeeEnv` name is empty, so TestMain sets the variable apart from that cleared list, and `ptyEnv` (e2e_support_test.go) appends it for PTY launches; the item's enabled-path tests clear it with `t.Setenv(…, "")`. The enable gate is a pure function of `(updateCheck bool, baseVersion, fullVersion string)` that `options()` calls with `apogee.BaseVersion()`/`apogee.Version()` — `versionFile` is unexported, so the `dev`/`.dirty` cases are table-tested on that function.
Also extend the closed env lists the re-check named (headlessRunUnderEnv in cmd/apogee/docs_env_test.go if it enumerates APOGEE_* overrides, and cmd/demorig's override list) with APOGEE_NO_UPDATE_CHECK where they must know every override; item 4's override is read through the injected getenv only, never os.Getenv inside settingsRows.
**Files:** cmd/apogee/wire_update.go; cmd/apogee/wire_options.go; cmd/apogee/wire_update_test.go; cmd/apogee/main_test.go; cmd/apogee/e2e_support_test.go; cmd/apogee/docs_env_test.go; cmd/demorig/record.go; cmd/demorig/record_test.go
**Read first:** cmd/apogee/wire_options.go — rootWiring.options; cmd/apogee/wire.go — runRootWith, rootWiring; cmd/apogee/main_test.go — TestMain, ambientApogeeEnv, buildE2EBinary; cmd/apogee/e2e_support_test.go — ptyEnv, frameRedactions; version.go — BaseVersion, Version, buildMetadata; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride, headlessRunUnderEnv;
cmd/demorig/record.go — ambientApogeeEnv, apogeeEnv; cmd/demorig/record_test.go — TestApogeeEnvStripsEveryApogeeOverride
**Tests:**
- Enabled with a newer stub release (detection supplied through the seam): `CheckForUpdate` returns the latest tag plus the detected command.
- `update-check: false` or the env var: `Options.Update == nil`; the gate function's table gives false for `dev` and a `.dirty` full version.
- The suite environment carries `APOGEE_NO_UPDATE_CHECK=1` (TestMain and `ptyEnv`), so no driven or PTY launch sends a request.
- A headless run against the stub server records zero requests.
**Acceptance:** `go test -race -count=1 -run 'Update' ./cmd/apogee/`
**Commit:** `feat(cli): check for a newer release when the TUI boots`

## 7. `internal/update`: download, verify and stage a release binary — ✅ DONE (2026-10-10)

NOTES (2026-10-10): consequential edit — internal/update/latest.go: made necessary by adding `Stage` (the package doc, which lives in latest.go, described only the lookup; one sentence now names `Stage`).

NOTES (2026-10-10): the archive is downloaded into memory (bounded at 256 MiB; SHA256SUMS at 64 KiB, extracted binary at 512 MiB) and the binary is written to a `.apogee-staging-*` temp file in `dir`, chmod 0755, fsynced and renamed into place, so every failure leaves `dir` empty; `dir` must already exist. `Stage` validates the tag (`vX.Y.Z`) and goos/goarch (`[a-z0-9]+`) before any request, and exports `ErrChecksumMismatch` for item 8.

NOTES (2026-10-10): the entry is matched by exact name and must be a regular file (a symlink entry is refused); the entry name never reaches the file system, so `../apogee` cannot escape `dir`. Lint: pinned golangci-lint over ./internal/update/ reports 0 issues.

**Depends on item 2.**
**What:**
**Goal:** `update.Stage(ctx, Client, tag, goos, goarch, dir string) (path string, err error)` returns the path of an extracted, checksum-verified `apogee`/`apogee.exe` inside `dir`. It fails, leaving no file behind, on a missing asset, a SHA256 mismatch or an archive without the expected entry.
**Approach (assumed at the header base):**
- Asset name `apogee_<bare>_<goos>_<goarch>.{zip|tar.gz}` (zip on windows) with entry `apogee_<bare>_<goos>_<goarch>/apogee[.exe]`, matching the Makefile `dist` layout. Both come from `<base>/releases/download/<tag>/`, together with `SHA256SUMS`.
- The download client follows redirects (assets redirect to a CDN), uses `ProxyFromEnvironment` and a 2 min timeout.
- Extract with `archive/zip` / `archive/tar` + `compress/gzip`, reading only the one expected entry (no path traversal). Files are written `0755` and the staged file is fsynced.
**Files:** internal/update/stage.go; internal/update/stage_test.go
**Read first:** Makefile — dist, CROSS_TARGETS, DIST_VERSION, SHA256; scripts/release-smoke.sh; internal/update/latest.go — Client (item 2, no-redirect client: Stage needs its own redirect-following client)
**Tests:** httptest serves a real tar.gz and zip built in the test plus SHA256SUMS. Happy path for both formats; a tampered archive gives a checksum error and an empty `dir`; a missing asset gives an error; an archive whose entry is named `../apogee` is rejected.
**Acceptance:** `go test -race -count=1 ./internal/update/`
**Commit:** `feat(update): download and verify a release binary`

## 8. `apogee update` subcommand — ✅ DONE (2026-10-10)

NOTES (2026-10-10): consequential edit — cmd/apogee/doc.go: made necessary by the new update.go / update_swap_unix.go / update_swap_windows.go (TestDocMapNamesEveryFile requires every file in the map).

NOTES (2026-10-10): the `<exe>.old` start-up sweep runs only where the swap makes one (Windows, `swapLeavesOldExecutable`); on Unix it is a no-op so it can never delete a user's own `apogee.old` backup — the Unix swap never leaves a `.old`. The deletion itself (removeLeftoverExecutable) is tested on every OS.

NOTES (2026-10-10): updateDeps carries no separate exe-path or out field: the replaced file is the detection seam's `Inputs.ExePath` (one source, so the Method and the swapped file cannot disagree), and output goes to cobra's `cmd.OutOrStdout()`, which tests capture with SetOut.

NOTES (2026-10-10): writer's calls — a declined prompt prints `Update cancelled.` and exits 0; `--check` on a managed install still refuses with exit 1 (the Goal's refusal is unconditional); the stub release binary is a `/bin/sh` script, so the three exec tests skip on Windows (cmd/apogee tests do not run in the Windows CI leg) and run serially to avoid ETXTBSY from parallel forks.

**Depends on items 3, 7.**
**What:**
**Goal:** `apogee update [--yes] [--check]` exists.
- On Homebrew, Scoop, Winget, GoInstall and Source it prints `apogee was installed via <channel> — run: <command>` and exits 1.
- On Archive, with a newer release, it prompts `Update <cur> → <latest>? [y/N]` (skipped by `--yes`; `--check` only reports). It then stages next to the executable, runs `<staged> --version` and requires the output to contain `<latest>`, and swaps.
- When already current it prints `apogee <cur> is up to date` and exits 0.
- A leftover `<exe>.old` is removed best-effort at every process start.
**Approach (assumed at the header base):**
- `cmd/apogee/update.go` builds `newUpdateCommand()` and adds it to `subcommands()`.
- The swap is `os.Rename(staged, exe)` on Unix. On Windows it renames the running exe to `<exe>.old` first, then renames staged to exe, and renames back on failure.
- A non-interactive stdin without `--yes` refuses with exit 1.
- The `.old` sweep runs early in `main()`.
- The manual page `docs/manual/updating.md` is indexed in `docs/manual/README.md`.

**Regression guard.** reuse item 6's detection-input seam (do not re-read build info directly) so tests can force each Method
Also (reviewer guards, verified): build it as `newUpdateCommandWith(updateDeps{…})` — exe path, detection inputs/Method, isTerminal, stdin, out, client — on the `mcpLoginDeps`/`testMCPLoginDeps` pattern, with `newUpdateCommand()` filling production values. The `.old` sweep goes immediately after `maybeDispatchConfinedExec()` in `main()`, never before it (main.go: "no subcommand may be reachable before it").
**Files:** cmd/apogee/update.go; cmd/apogee/update_swap_unix.go; cmd/apogee/update_swap_windows.go; cmd/apogee/subcommands.go; cmd/apogee/main.go; cmd/apogee/update_test.go; docs/manual/updating.md; docs/manual/README.md
**Read first:** cmd/apogee/main.go — main, maybeDispatchConfinedExec; cmd/apogee/subcommands.go — subcommands; cmd/apogee/root.go — newRootCommand (SilenceErrors/SilenceUsage); cmd/apogee/headless.go — exitCodeFor, exitError; cmd/apogee/mcp_cmd_test.go — testMCPLoginDeps, runMCPCommand; docs/manual/README.md — page index table
**Tests:**
- Each managed Method (forced through the seam / `updateDeps`) gives the exact message plus exit 1.
- Archive + `--yes` against the stub server replaces a temp-dir "exe" (a stub release binary built by the test that echoes the version) and leaves no `.old` on Unix.
- `--check` reports without writing.
- Non-tty stdin without `--yes` refuses.
- The `.old` sweep deletes the file.
**Acceptance:** `go test -race -count=1 -run 'Update' ./cmd/apogee/`; `go run ./cmd/apogee update --help`
**Commit:** `feat(cli): add apogee update for archive installs`

## 9. Scoop manifest publisher — ✅ DONE (2026-10-10)

NOTES (2026-10-10): consequential edit — .gitattributes: made necessary by the item's LF guard for the new script; `scripts/*.sh text eol=lf` keeps a core.autocrlf=true (Git Bash) checkout from breaking it (the existing scripts were already LF, so nothing renormalizes).

NOTES (2026-10-10): scripts/release-scoop.sh is staged (`git add`) so the index records mode 100755 and `TestReleaseScoopScriptIsExecutable` passes before the commit; the test skips without git or outside a checkout.

NOTES (2026-10-10): added `BUCKET_URL=` (plain `git clone` of that URL instead of `gh repo clone`) so the publish half can be exercised against a local bare repo; verified by hand that way (commit `apogee 9.8.7` pushed, a re-run reports nothing to publish). docs/manual/building.md is left to item 11, which owns the Releasing section.

**What:**
**Goal:** `make release-scoop` (script `scripts/release-scoop.sh`) writes `bucket/apogee.json` into a clone of `$BUCKET_REPO` (default `airiclenz/scoop-bucket`) for `$VERSION`, then commits `apogee <bare>` and pushes. `DRY_RUN=1` prints the manifest instead.
- The hashes come from the published release's `SHA256SUMS` (`SUMS_FILE=<path>` overrides, for tests).
- The manifest validates as JSON with `version`, `description`, `homepage`, `license`, and `architecture.64bit` / `architecture.arm64` each holding `url`, `hash` and `extract_dir`.
- It also carries `bin: "apogee.exe"`, and `checkver: "github"` plus a matching `autoupdate` block.
**Approach (assumed at the header base):** bash with `set -euo pipefail`, in the style of `scripts/release-smoke.sh` (`step()` banners, `REPO`/`VERSION` env with the same defaults). Clone with `gh repo clone` or `git clone` into a temp dir. A root-package Go test runs the script with `DRY_RUN=1` + `SUMS_FILE` and skips when `bash` is absent.

**Regression guard.** new scripts are committed with the executable bit (`git update-index --chmod=+x scripts/release-scoop.sh`) and LF line endings; the Go test asserts the git index mode is 100755 via `git ls-files -s`
Also (reviewer guards, verified): `DRY_RUN=1` with `SUMS_FILE` branches before any clone and touches no network, `gh`, `git` or `jq` (manifest built with printf/heredoc); the test sets `VERSION` explicitly. `homepage` is pinned to `https://github.com/airiclenz/apogee` (Scoop's `checkver: "github"` requires it); `autoupdate` uses `$version` URLs, `extract_dir: "apogee_$version_windows_<arch>"` and hashes from `$baseurl/SHA256SUMS`.
**Files:** scripts/release-scoop.sh; Makefile; release_scripts_test.go
**Read first:** scripts/release-smoke.sh — step, REPO/VERSION/BARE defaults, TARGETS asset naming; Makefile — release-smoke recipe, dist (archive layout apogee_X_windows_arch/apogee.exe + LICENSE, README); version_test.go — package apogee root tests; .gitattributes — scripts have no eol rule
**Tests:** `TestReleaseScoopDryRun` parses the JSON and checks both arch URLs (`.../releases/download/vX/apogee_X_windows_{amd64,arm64}.zip`) and their hashes against the fixture SUMS, and the `homepage` value. A test asserts `git ls-files -s scripts/release-scoop.sh` reports mode 100755.
**Acceptance:** `go test -race -count=1 -run TestReleaseScoop .`
**Commit:** `feat(release): publish the Scoop manifest`

## 10. winget submission via komac — ✅ DONE (2026-10-10)

NOTES (2026-10-10): the token reaches komac through its environment (`GITHUB_TOKEN="$token" komac ...`, which komac reads natively) instead of `--token`, so no process listing shows it; the dry run therefore prints `GITHUB_TOKEN=<redacted> komac update ...` rather than `--token <redacted>` — the redaction, the sentinel-token assertion and the skipped komac/`gh auth token` lookups are as the item specifies.

NOTES (2026-10-10): `runReleaseScript` now returns stderr too (and `releaseScriptCommand` is factored out of it) so the winget dry-run test can assert the token is absent from both streams; `GITHUB_TOKEN` joins `releaseScriptEnvKeys`. Added `TestReleaseWingetRequiresKomac` (real run, komac-free PATH, non-zero exit + install hint) beyond the two named tests.

NOTES (2026-10-10): scripts/release-winget.sh is staged (`git add`, mode 100755) so `TestReleaseWingetScriptIsExecutable` passes before the commit. docs/manual/building.md is left to item 11, which owns the Releasing section.

**Depends on item 9** (shares `release_scripts_test.go`).
**What:**
**Goal:** `make release-winget` (script `scripts/release-winget.sh`) runs `komac update AiricLenz.Apogee --version <bare> --urls <windows_amd64.zip url> <windows_arm64.zip url> --submit`, using `$GITHUB_TOKEN` (or `gh auth token`). It exits non-zero with an install hint when `komac` is missing. `DRY_RUN=1` prints the exact command (token redacted) and runs nothing.
**Approach (assumed at the header base):** the same bash style and `REPO`/`VERSION` defaults as item 9. The script only submits the PR; merge happens later in `microsoft/winget-pkgs`, and the script says so in its final line.

**Regression guard.** same exec-bit/LF rule and index-mode assertion for scripts/release-winget.sh
Also (reviewer guard, verified): `DRY_RUN=1` skips the komac-presence check and the `gh auth token` lookup (komac and gh are absent on CI and this box), printing `--token <redacted>`; the test sets `GITHUB_TOKEN` to a sentinel and asserts it is absent from the output.
**Files:** scripts/release-winget.sh; Makefile; release_scripts_test.go
**Read first:** scripts/release-smoke.sh — step, skip, REPO/VERSION defaults; Makefile — release-smoke recipe and `## name:` help comments; release_scripts_test.go (from item 9) — TestReleaseScoopDryRun harness; komac README — `komac update <Id> --version --urls ... --submit`, GITHUB_TOKEN env (verified, matches the item)
**Tests:** `TestReleaseWingetDryRun` (with a sentinel `GITHUB_TOKEN`, no komac on PATH) checks the printed command names both URLs, the bare version and `AiricLenz.Apogee`, and contains no token value. A test asserts `git ls-files -s scripts/release-winget.sh` reports mode 100755.
**Acceptance:** `go test -race -count=1 -run TestReleaseWinget .`
**Commit:** `feat(release): submit the winget manifest with komac`

## 11. Install docs, release runbook and smoke check for the new channels — ✅ DONE (2026-10-10)

NOTES (2026-10-10): the building.md one-time setup is a bold-led paragraph plus a numbered list inside `## Releasing`, not a `###` heading — a heading there would have pulled the section's following `make check` / raw-toolchain paragraphs under it.

NOTES (2026-10-10): release-smoke.sh gains `BUCKET_REPO` (same default as release-scoop.sh) and `BUCKET_MANIFEST_URL` (a file:// URL reads a local fixture) env overrides; the bucket step reads the manifest with jq and SKIPs when jq is absent rather than parsing JSON by hand. "Is $VERSION the latest release" is the same HEAD releases/latest redirect lookup the update check uses; when it cannot be read, a version mismatch SKIPs.

NOTES (2026-10-10): the bucket step was exercised offline against fixtures (manifest from release-scoop.sh's own DRY_RUN): match → OK; hash mismatch → FAIL; stale bucket + latest → FAIL; stale bucket + newer latest → SKIP; latest unreadable → SKIP; unreachable manifest → SKIP; no SHA256SUMS → SKIP; invalid JSON → FAIL; no jq → SKIP.

NOTES (2026-10-10): README "Three ways in, and all three land the same thing" now reads "Every way in lands the same thing", with no number to go stale.

**Depends on items 9, 10.**
**What:**
**Goal:**
- README `## Install` gains a `**Scoop — Windows:**` block (`scoop bucket add airiclenz https://github.com/airiclenz/scoop-bucket`, `scoop install apogee`) and a `**winget — Windows:**` block (`winget install AiricLenz.Apogee`), and mentions `apogee update` for archive installs.
- `docs/manual/building.md` `## Releasing` lists `make release-scoop` and `make release-winget` after the tap step, plus a "one-time setup" subsection: create the bucket repo, fork `winget-pkgs`, install komac, the first `komac new AiricLenz.Apogee` submission, and the token scope.
- `scripts/release-smoke.sh` gains a step that fetches the bucket's `bucket/apogee.json` and checks its version and both hashes against `SHA256SUMS`. It skips loudly when the bucket is unreachable.
**Approach (assumed at the header base):** amend in place; keep `TestReadmeArchiveInstallDoesNotPinAVersion` green (no pinned version in any new snippet).

**Regression guard.** also add a docs/manual/updating.md row to README.md's Documentation table (item 11 owns all README edits after item 1).
Also (reviewer guards, verified): the bucket step fails on a version mismatch only when `$VERSION` is the latest release, otherwise SKIPs naming the bucket's version (`make release-smoke VERSION=<older tag>` must stay green), and parses without jq or SKIPs when jq is absent. README's "Three ways in, and all three land the same thing" is reworded to the new channel count (or drops the number). Every prose site enumerating release-smoke's checks — docs/manual/building.md `## Releasing` act 4, the scripts/release-smoke.sh header, the Makefile release-smoke comment — names the bucket step.
**Files:** README.md; docs/manual/building.md; scripts/release-smoke.sh; Makefile
**Read first:** scripts/release-smoke.sh — header Needs list, step/skip/fail, SHA256SUMS download block, Homebrew step; README.md — ## Install (Homebrew block, prebuilt-archive block); cmd/apogee/readme_test.go — TestReadmeArchiveInstallDoesNotPinAVersion; docs/manual/building.md — ## Releasing acts 3 and 4; Makefile — release-smoke comment
**Tests:** existing README tests.
**Acceptance:** `go test -race -count=1 -run Readme ./cmd/apogee/`; `bash -n scripts/release-smoke.sh`
**Commit:** `docs(install): document Scoop, winget and apogee update`
