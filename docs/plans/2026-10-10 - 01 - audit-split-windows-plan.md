# Shipped `/audit` runs on Windows — the split in Go

**Goal:** the shipped `audit` recipe's split runs on a stock Windows box (no `sh`, no coreutils, no
Git Bash) and on POSIX with byte-identical outputs. The split moves from `split.sh` into apogee
itself, called from the same script stage as a hidden `apogee audit-split` subcommand.

**Date:** 2026-10-10
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** f165a528
**Closes:** apogee-audit-split-windows

**Regression check (2026-10-10, f165a528):**
- 1: guard folded (listing seam; git and walk paths tested parallel and hermetic).
- 2: guard folded (two output orders; knobs through an options struct; per-line flags match).
- 3: guard folded (doc.go file-map entry; TestDocMap in Acceptance).
- 4/5: recast (writer decision: `apogee` is a reserved input name; no exec grant) and guards folded (injected executable + TestMain `audit-split` arm; rendered-line execs stay linux-tagged; orphaned helpers removed, receipt-keys contract test kept; missing executable fails with a restart sentence); yields to ADR 0001 (no process globals in the engine).
- 6: recast (writer decision: recipe-layer quoter with per-platform safe sets, cmd /c outer-quote wrap, `%VAR%` a documented non-guarantee); yields to host.go's windowsQuote contract and its two pinned tests.
- 7: guard folded (Makefile vet line, building.md, ci.yml comment).
- 8: guard folded (no doc-drift test exists; checkable Acceptance; workflow-bench-experiment.md in Files; fixture `split.sh` exempt).
- 4/5 (second check): guards folded (projectConfig wiring test for both Drivers; stored pre-upgrade plan fails with a start-/audit-again sentence + CHANGELOG line; unset executable field gets its own embedder sentence); yields to ADR 0031 invariant 4.
- 6 (second check): guard folded (writer decision: the Windows outer-quote wrap is render's last step, one wrap pinned on the shipped audit split line; one-time new folder for pre-upgrade Windows audits accepted).
- writer, after the amender rounds: oversized item 4 split into 4 (engine `{apogee}`) and 5 (audit recipe), later items renumbered, CHANGELOG.md dropped from Files (sidecar); item 6's cmd wrap moved into `internal/platform` so the native test drives it; ADR 0031 invariant 4 confirmed as yields-to (embedders name an apogee binary).

**Sources:**
- `internal/skills/shipped/audit/split.sh` (the behaviour to port — the parity reference)
- `internal/skills/shipped/audit/SKILL.md` (stages `split`, `flags`)
- `docs/adr/0042-external-programs-are-optional-enhancements-never-prerequisites.md` (D2, D3)
- `docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md` (D6)
- `docs/adr/0065-shipped-skills-and-the-load-skill-door.md` (2026-09-27 amendment)
- `docs/manual/workflows.md` (script stages, placeholders)

**Diagnosis (2026-10-10):** on Windows a script stage runs `cmd /c <line>`; `sh` is not on a
default Git-for-Windows PATH (exit 9009, `split.status=blocked`, every later stage skipped). Behind
it: `shellQuote` sends POSIX `'…'` quoting to `cmd`, and `split.sh` writes MSYS `/c/…` paths that
Go reads as relative. `internal/skills/shipped/**` carries no LF pin.

**Ratified design calls** (owner, 2026-10-10):
- **Approach:** port the split to Go as an apogee subcommand; the stage stays a `script` stage (ADR 0087 D6 holds).
- **Placeholder:** `{apogee}` — single braces, like `{workflow_dir}` and `{part_bytes}`.
- **Subcommand:** hidden `apogee audit-split`; not a public contract, absent from help and the manual's command list.

**Standing requirements:**
- skills: coding-standards
- This box (Pi 5): tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./<one pkg>/`; never a package pattern, never two runs at once; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`. Pass this to every sub-agent.
- Bubble Tea v2 names only (`tea.KeyPressMsg`), should any TUI code be touched.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- A Windows-native `sh` discovery (Git Bash lookup) for user recipes.
- Other shipped skills (none has a `run:` stage).
- `VERSION` / release changes.
- Running the whole `/audit` on real Windows — the owner's manual step.

## 1. auditsplit: scope listing and part split — ✅ DONE (2026-10-11)

NOTES (2026-10-11): the git listing runs through internal/gitexec's hardened Host (gitexec.go's own rule: the single place apogee launches git) rather than a raw git-path exec; the listing seam is therefore a gitexec.Host on planRequest (a fake Look forces the walk, fake Spawn/SpawnTo force the git-fails fallback, gitexec.OS() lists a real repo) — one behavioural consequence: a repository whose own config names a program git would execute is refused by gitexec and falls back to the walk.
NOTES (2026-10-11): PART_FILES and GROUP_PARTS are validated as digit-only whole numbers like PART_LINES (split.sh left them unchecked and its `[ -gt ]` misbehaved), and all three knobs are parsed to ints, so a zero-padded knob ("007") would print as 7 where split.sh echoed it raw.
NOTES (2026-10-11): plan.parts come in byte order of part name (split.sh's part_index order); the parts.txt glob order (byte order of `<name>.txt`, `a-b` before `a`) is available as globOrder for item 2. Per-part line count and concurrency flag (CONC_PATTERN) are computed in the plan step, so no file is written yet.
NOTES (2026-10-11): parity was checked against the embedded split.sh with a throwaway differential test (1400 random fixtures: scope, src, tests, every part's files, conc parts — all identical); the test was deleted, not committed. plan.go is ~580 lines, over the ~400-line guideline, because the item's Files list names it as the one implementation file.

**What:**
**Goal:** a new package `internal/auditsplit` lists a workspace scope and splits its source files
into parts exactly as `split.sh` does (`list_scope`, test/source classification, `split_by_top_level_dir`,
`resplit_one_level_deeper`, `chunk_flat_part`, `merge_small_parts`, `resolve_part_lines`), with tests
proving parity on the cases `split.sh` handles.
**Approach (assumed at the header base):** one deep module; an unexported `plan` step that returns
the scope list, src/tests lists and the ordered parts (name → files), no file writes yet. Port the
constants verbatim (`TEST_PATTERN`, `CONC_PATTERN`, `EXCLUDED_DIRS`, `EXCLUDED_FILES`, `MAX_PART_LINES`,
`MIN_PART_LINES`, `BYTES_PER_LINE`, `MIN_PART_FILES`, `PART_FILES`, `GROUP_PARTS`, `WHOLE_PART`) and
the `PART_LINES`/`PART_FILES`/`GROUP_PARTS` env knobs. Listing: `git ls-files --cached --others
--exclude-standard -z` when git is on PATH (NUL-separated, so no quoting), else `filepath.WalkDir`
skipping `.git` — ADR 0042 D2. Scope entries are workspace-relative, `/`-separated, sorted in byte
order; never globbed; names holding a newline skipped. Line counting matches `wc -l` (newline count).
**Regression guard.** The listing takes an unexported seam on the `plan` step (a lister / git-path
option the public entry fills from PATH), so the git listing and the `WalkDir` fallback are each tested
hermetic under `t.Parallel` — no `t.Setenv`, no PATH edit (split.sh's tests forced the fallback per run
with `GIT_DIR`, which a parallel in-process test cannot do).
**Files:** internal/auditsplit/doc.go, internal/auditsplit/plan.go, internal/auditsplit/plan_test.go
**Read first:** internal/skills/shipped/audit/split.sh — list_scope, resolve_part_lines, split_by_top_level_dir, resplit_one_level_deeper, chunk_flat_part, merge_small_parts, part_index;
internal/skills/load_test.go — runAuditSplit
**Tests:** table tests per mechanic (top-level split, one-level-deeper resplit, flat chunking, small-part
merge, whole-scope `all` part, test classification, exclusions, spaces/`*`/`[` in names, no-git walk and
git listing, each through the listing seam, parallel).
**Acceptance:**
- `go build ./internal/auditsplit/ && GOOS=windows go build ./internal/auditsplit/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/auditsplit/`
**Commit:** `feat(auditsplit): list and split an audit scope in Go`

## 2. auditsplit: run folder, receipt and flags — ✅ DONE (2026-10-11)

NOTES (2026-10-11): consequential edit — internal/auditsplit/doc.go: made necessary by the new split.go, which the package doc's "The files, one line each" map now names.
NOTES (2026-10-11): the internal entry is split(ctx, splitRequest, w), splitRequest carrying item 1's planRequest (knobs and the gitexec.Host listing seam) plus run and focus; Split fills it from os.Getenv and gitexec.OS() with context.Background(), and converts its int partBytes to the plan's digit string (zero or negative = unknown budget = 8000 lines).
NOTES (2026-10-11): Split prints only the receipt to w; split.sh's duplicate of the failure on stderr is left to the item-3 subcommand, which gets the same text as the returned error. No scratch folder is created inside the run (split.sh's `.split.$$`).
NOTES (2026-10-11): parity was checked against the embedded split.sh with a throwaway differential test (300 random fixtures over PART_LINES/PART_FILES/GROUP_PARTS, nested test dirs and `x`/`x-y` siblings: receipt and every run-folder file byte-identical after RUN substitution); deleted, not committed. One known divergence by construction: a file name holding a tab, which split.sh's awk field splitting truncated in tests-<part> lists, is kept whole.
NOTES (2026-10-11): split.go is ~550 lines, over the ~400-line guideline, because the item's Files list names it as the one implementation file.

Depends on item 1.

**What:**
**Goal:** `auditsplit.Split(workspace, run, scope, focus string, partBytes int, w io.Writer) error` writes
the same run-folder layout `split.sh` writes (`scope.txt`, `src.txt`, `tests.txt`, `part-<name>/{scope,tests}.txt`,
`group-<name>/{parts,cap}.txt`, `run-dir.txt`, `parts.txt`, `conc-parts.txt`, `groups.txt`, `split.txt`,
`layout.txt`) and prints the same `KEY=value` receipt; `auditsplit.Flags(run, w)` prints
`concurrency=yes|no` from `bundle.md` as `print_flags` does. Absolute paths are native (`filepath.Abs`),
never MSYS-style.
**Approach (assumed at the header base):** port `write_test_lists`, `write_parts`, `write_groups`
(claims cap from `TOTAL_CLAIMS`/`MIN_GROUP_CLAIMS`/`MAX_GROUP_CLAIMS`), `holds_concurrency`,
`print_receipt`, `layout_key` (same `recipe-1` layout key and the unchanged-scope no-op),
`remove_previous_split`, `focus_area`, and `die` (a failure prints `summary=audit-split: <msg>` and
returns an error). Files written LF, one entry a line.
**Regression guard.** Two orders, as split.sh: `parts.txt`/`conc-parts.txt` sort by `name + ".txt"` (the
`scope-*.txt` glob, so `x-y` precedes `x`); `split.txt`, groups and merge neighbours sort by name. `Split`
reads `PART_LINES`/`PART_FILES`/`GROUP_PARTS` into an unexported options struct an internal entry takes,
so tests pass knobs directly and stay parallel. `Flags` matches each line of `bundle.md` separately.
**Files:** internal/auditsplit/split.go, internal/auditsplit/split_test.go
**Read first:** internal/skills/shipped/audit/split.sh — main, write_parts, write_groups, print_receipt, layout_key, print_flags;
internal/skills/load_test.go — runAuditSplitWithEnv, TestAuditSplitFitsPartsUnderTheBudget
**Tests:** port the five `TestAuditSplit*` in `internal/skills/load_test.go` (budget fit, window bound,
spaces/glob chars, never globs the scope, git names unquoted) as `auditsplit` tests with no `sh` skip,
knobs through the options struct (no `t.Setenv`);
add re-run no-op, changed-bound re-split, test-only scope, flags yes/no, a parity test with a part `x`
beside `x-y` (both orders), a flags test with `CONCURRENCY:` and `yes` on separate lines expecting `no`,
and a test that every absolute path written satisfies `filepath.IsAbs`.
**Acceptance:**
- `GOOS=windows go vet ./internal/auditsplit/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/auditsplit/`
**Commit:** `feat(auditsplit): write the audit run folder and receipt`

## 3. Hidden `apogee audit-split` subcommand — ✅ DONE (2026-10-11)

NOTES (2026-10-11): consequential edit — internal/auditsplit/doc.go: made necessary by the new main.go, which the package doc's "The files, one line each" map now names.
NOTES (2026-10-11): the one entry is `auditsplit.Main(args []string, stdout, stderr io.Writer) int` (args = the words after `audit-split`; workspace = os.Getwd()); exit 1 on every failure, a malformed argv included, as split.sh's die; the split form passes PART_BYTES to the internal split as the raw string, so a non-digit value is an unknown budget exactly as in split.sh (the public Split takes an int).
NOTES (2026-10-11): the cobra command sets DisableFlagParsing (so `--flags` and dash-led scope words reach Main; `apogee audit-split --help` is therefore a usage refusal) and captures Main's stderr line into the returned exitError, so main prints the cause once and exits with Main's code.

Depends on item 2.

**What:**
**Goal:** `apogee audit-split <RUN> <SCOPE> [FOCUS] [PART_BYTES]` and `apogee audit-split --flags <RUN>`
run `auditsplit.Split` / `Flags` in the current directory, print the receipt on stdout, exit non-zero on
failure; the command is hidden from `apogee --help`.
**Approach (assumed at the header base):** a `newAuditSplitCommand()` in `cmd/apogee/auditsplit.go`
(cobra, `Hidden: true`), appended to `subcommands()` in `cmd/apogee/subcommands.go`. Workspace is the
cwd, matching the stage's current semantics. No config, no wiring, no network. `internal/auditsplit`
exposes one entry (argv → exit code) that the cobra command calls; item 5's test binary calls it too.
**Regression guard.** `cmd/apogee/doc.go`'s file map gains a one-line entry for `auditsplit.go`
(docmap matches the bare file name), or `TestDocMapNamesEveryFile` goes red.
**Files:** cmd/apogee/auditsplit.go, cmd/apogee/auditsplit_test.go, cmd/apogee/subcommands.go, cmd/apogee/doc.go, internal/auditsplit/main.go
**Read first:** cmd/apogee/subcommands.go — subcommands; cmd/apogee/root.go — newRootCommand, newRootCommandWith;
cmd/apogee/doc.go — file map; internal/docmap — Check; cmd/apogee/daemon_test.go — TestDaemonIsRegisteredOnTheRoot;
cmd/apogee/root_test.go — the hidden-diagnostic-flags --help test; cmd/apogee/probemodel_test.go — t.Chdir pattern (non-parallel)
**Tests:** argv → receipt on a temp workspace; `--flags` form; bad argv exits non-zero with a `summary=`
line; `--help` output does not name `audit-split`.
**Acceptance:**
- `go build ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAuditSplit|TestDocMap' ./cmd/apogee/`
**Commit:** `feat(cli): hidden audit-split subcommand`

## 4. `{apogee}` placeholder in the engine — ✅ DONE (2026-10-11)

NOTES (2026-10-11): cmd/apogee/wire_config_test.go — the two-Driver table of TestEveryDriverCarriesTheProjectedConfig was lifted unchanged into projectedDrivers() so the new TestProjectConfigNamesTheRunningExecutable drives the same session and Firing assemblies; assertCarriesProjection also compares ApogeeExecutable.
NOTES (2026-10-11): the reserved name is exported as workflow.ApogeeInputName and the engine's apogeePlaceholder is spelled from it, so the validation and the placeholder cannot drift; a non-NotExist stat failure (e.g. permission) gets its own "cannot be reached" error rather than the restart sentence; if os.Executable fails the field stays empty and a `{apogee}` stage fails with the unset sentence.
NOTES (2026-10-11): pre-existing, not this item's: inputs named `workflow_dir` or `part_bytes` pass ValidateInputs and would shadow those placeholders the same way `apogee` would have; `{apogee}` is rendered with the POSIX shellQuote until item 6's platform quoting lands.

Depends on item 3.

**What:**
Split from the recast item 4 at write time (2026-10-10): this half is the engine, item 5 the recipe.
**Goal:** a recipe `run:` may write `{apogee}`, rendered to the apogee executable named by a
`domain.Config` field that cmd/apogee's wiring sets to `os.Executable()` for both Drivers; an input
named `apogee` is rejected by validation; a vanished or unset executable fails the stage with a sentence.
**Approach (assumed at the header base):** add `apogeePlaceholder = "{apogee}"` next to
`workflowDirPlaceholder` in `internal/agent/recipe.go`; `recipeScripts.render` replaces it with the quoted
path from the Config field (set in `projectConfig`, `cmd/apogee/wire_config.go`). Reserve `apogee` in the
recipe input validation (`internal/workflow/inputs.go`).
**Regression guard.** The engine reads no `os.Executable()` itself (yields to ADR 0001: no process
globals in the engine). bindPlanInputs runs before render, so the reserved name is what keeps an input
from shadowing `{apogee}`. A path that no longer exists fails the stage with a sentence saying apogee was
upgraded or moved since the session started (restart), never a bare exit 127. An UNSET field fails with
its own sentence naming the Config field an embedder must set to an apogee binary, documented on the
field (`apogee.Config` aliases `domain.Config`); this yields to ADR 0031 invariant 4 (benchable all the
way up) — an embedder reaches `{apogee}` stages only by naming an apogee binary there. No confinement exec
grant is needed (every backend already allows exec outside the box).
**Files:** internal/agent/recipe.go, internal/agent/recipe_test.go, internal/domain/config.go, cmd/apogee/wire_config.go, cmd/apogee/wire_config_test.go, internal/workflow/inputs.go, internal/workflow/inputs_test.go
**Read first:** internal/agent/recipe.go — recipeScripts.render, workflowDirPlaceholder, inputReplacers, bindPlanInputs;
cmd/apogee/wire_config.go — projectConfig; internal/agent/launch.go — launchRunner, newLaunchRunner; internal/workflow/inputs.go
**Tests:** `render` expands `{apogee}` to the quoted executable; a missing executable fails with the restart
sentence; an unset field fails with the Config-field sentence (a separate case); `projectConfig` sets the
field to `os.Executable()` for the session (wire_boot.go) and the Firing Driver (wire_firing.go); an input
named `apogee` is rejected.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Recipe|Render' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Input' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestProjectConfig ./cmd/apogee/`
**Commit:** `feat(workflow): {apogee} placeholder names the apogee executable`

## 5. The audit recipe runs `{apogee} audit-split`

Depends on item 4.

**What:**
Split from the recast item 4 at write time (2026-10-10). Fixes the Windows failure of `apogee-audit-split-windows`.
**Goal:** the shipped audit recipe's `split` and `flags` stages run
`{apogee} audit-split {workflow_dir} {scope} {focus} {part_bytes}` and `{apogee} audit-split --flags {workflow_dir}`;
`internal/skills/shipped/audit/split.sh` no longer exists; the split stage succeeds in Plan mode under
confinement; a stored pre-upgrade plan still naming `{{SKILL_DIR}}/split.sh` fails with a sentence.
**Approach (assumed at the header base):** edit the two stage lines and the SKILL.md step-1 prose that names
`split.sh`; delete `split.sh`; remove `runAuditSplit`, `runAuditSplitWithEnv`, the `TestAuditSplit*` tests and
every helper only they use (`auditFixture`, `writeSplitFixture`, `hostCanName`, `readLines`) from
`internal/skills/load_test.go` (item 2 owns those tests now).
**Regression guard.** `confine_linux_test.go`'s `TestMain` gains an `audit-split` arm calling item 3's
`auditsplit` argv entry, so the Plan-mode test execs the test binary as `{apogee}` and never re-runs the
agent suite; every exec of a rendered `{apogee}` line stays in linux-tagged tests. Keep one contract test
in `load_test.go` running `auditsplit.Split`/`Flags` and comparing receipt keys to the stages' `returns:`.
A stored plan (RerunFailed/resume read it back) whose staged `{{SKILL_DIR}}/<file>` is no longer in the
skill fails render with a sentence telling the user to start /audit again (the workflow came from an
older apogee); the item's CHANGELOG sidecar notes it.
**Files:** internal/agent/recipe.go, internal/agent/recipe_test.go, internal/agent/confine_linux_test.go, internal/skills/shipped/audit/SKILL.md, internal/skills/shipped/audit/split.sh, internal/skills/load_test.go
**Read first:** internal/agent/recipe.go — skillDir, stageSkillFile; internal/agent/confine_linux_test.go — TestMain, livePlanScripts, TestRecipe_TheShippedAuditSplitRunsInPlan;
internal/agent/background.go — RerunFailed, folderLaunch; internal/skills/load_test.go — TestShippedAuditRecipeLoads, auditFixture, readLines
**Tests:** `TestRecipe_TheShippedAuditSplitRunsInPlan` renders the embedded SKILL.md's exact `split` run line,
executes it through the terminal path in Plan mode and reads `status=ok`; a stored legacy plan fails with
the start-/audit-again sentence; the `load_test.go` contract test matches receipt keys to `returns:`.
**Acceptance:**
- `test ! -e internal/skills/shipped/audit/split.sh`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Recipe|Script' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Audit|Shipped' ./internal/skills/`
**Commit:** `fix(audit): run the split as apogee audit-split, not sh`

## 6. Platform quoting for recipe values

Depends on item 5.

**What:**
Recast at the regression check (2026-10-10).
**Goal:** every value a recipe substitutes into a `run:` line (`{scope}`, `{focus}` and other inputs,
`{{SKILL_DIR}}`, `{workflow_dir}`, `{apogee}`) is quoted for the shell the terminal actually runs — POSIX
`sh -c` unchanged, Windows `cmd /c` with its own quoting — so a Windows path with spaces, `&` or `^` is
one argument and never splits the command.
**Approach (assumed at the header base):** route `shellQuote` call sites in `bindPlanInputs` and
`recipeScripts.render` through a recipe-layer quoter (see the Regression guard). POSIX output stays
byte-identical, so existing tests pass unchanged. `%VAR%` inside a Windows value is not neutralised — a
documented non-guarantee, as `windowsQuote`'s own contract states.
**Regression guard.** Leave platform.Host.Quote, windowsQuote and its pinned tests (host_test.go "windows env var is not neutralised", platform_windows_test.go TestWindowsQuoteDoesNotNeutraliseEnvironmentExpansion) and the host.go comment untouched. Quote in a recipe-layer quoter in internal/agent: keep the bare-word shortcut ahead of quoting so POSIX output stays byte-identical, with a per-platform safe set (the Windows set excludes `%` and `\`; update the shellSafeValue comment); non-safe values are quoted with the platform shell's own Quote. On Windows, a rendered run line that starts with `"` is wrapped in one extra outer quote pair (the cmd /c outer-quote rule, the echoThroughCmd idiom). `%VAR%` inside a Windows value is NOT neutralised — drop that promise from the Goal and state it as a documented non-guarantee matching host.go's contract (item 7 documents it in docs/manual/workflows.md). Seam: a quote/shell field on recipeScripts and a parameter on bindPlanInputs, nil → platform.Current(); agent tests use an exported Windows-rules constructor in internal/platform or a fake. The whole-rendered-line round trip through real cmd is a native test in internal/platform/platform_windows_test.go (runs on test-windows CI). Add internal/agent/launch.go, internal/agent/confine_linux_test.go and internal/platform/platform_windows_test.go to Files; add "Depends on item 4".
The item yields to host.go's windowsQuote doc ("no in-line escape" for `%VAR%`) and the two tests that pin it.
Also binding (second check): The Windows outer-quote wrap is the last step of render, applied after every placeholder ({apogee}, {{SKILL_DIR}}, {workflow_dir}, {part_bytes}) has landed; a test pins that the shipped audit split line (which opens with the quoted {apogee} on Windows) gets exactly one wrap. The one-time new workflow folder for pre-upgrade Windows audits (bindPlanInputs output is hashed) is accepted — those runs never worked on Windows.
Also binding (writer, after the second check): the cmd outer-quote wrap is an exported `internal/platform` function
(Windows rules: wrap a line starting with `"`; POSIX: identity) that render calls as its last step, so the native
`platform_windows_test.go` test drives the same code over a whole line built from `Host.Quote` values.
**Files:** internal/agent/recipe.go, internal/agent/recipe_test.go, internal/agent/launch.go, internal/agent/confine_linux_test.go, internal/platform/platform_windows_test.go, internal/platform/host.go (the exported wrap, and an exported Windows-rules constructor should that seam be chosen; windowsQuote and its comment untouched), internal/platform/host_test.go
**Read first:** internal/agent/recipe.go — shellQuote, shellSafeValue, inputReplacers, bindPlanInputs, recipeScripts.render; internal/platform/host.go — hostRules.Quote, windowsQuote, windowsRules, hostRules.CommandLine;
internal/platform/platform_windows_test.go — echoThroughCmd, TestWindowsQuoteRoundTripsThroughCmd, TestWindowsQuoteDoesNotNeutraliseEnvironmentExpansion; internal/agent/recipe_test.go — TestShellQuote, TestBindPlanInputs;
internal/agent/launch.go — launchRunner (bindPlanInputs call), newLaunchRunner; internal/tools/terminal.go — Terminal.Execute, preflightCommandLine; .github/workflows/ci.yml — test-windows job
**Tests:** POSIX cases of `TestShellQuote` and `TestBindPlanInputs` unchanged; Windows-rules cases (spaces,
`&`, `^`, `'`, a value holding `%` or `\` quoted rather than left bare, a rendered line starting with `"`
wrapped once more, the shipped audit split line rendered under Windows rules wrapped exactly once, after every
placeholder has landed) through the Windows-rules constructor or a fake; a native test in
`platform_windows_test.go` running a whole rendered line through real `cmd /c` and asserting the argv
round-trips.
**Acceptance:**
- `GOOS=windows go vet ./internal/agent/ ./internal/platform/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Quote|Render|Bind' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run Quote ./internal/platform/`
**Commit:** `fix(agent): quote recipe values for the host's shell`

## 7. LF pin and Windows CI coverage

**What:**
**Goal:** every file under `internal/skills/shipped/` is LF in any checkout, and the `test-windows` CI
job runs `./internal/auditsplit/...`.
**Approach (assumed at the header base):** `.gitattributes` gains `internal/skills/shipped/** text eol=lf`
with a comment in the file's style (go:embed bakes the bytes); `.github/workflows/ci.yml` job
`test-windows` adds the package to its `go test` line.
**Regression guard.** In the same commit the `check` target's `GOOS=windows go vet` line in `Makefile`
gains `./internal/auditsplit/...`, `docs/manual/building.md`'s Windows-tagged-tests paragraph names the
fourth tree, and the ci.yml step comment ("These three trees rather than `./...`") is updated — as the
2026-10-04 console plan did for `./internal/console`.
**Files:** .gitattributes, .github/workflows/ci.yml, Makefile, docs/manual/building.md
**Read first:** .github/workflows/ci.yml — test-windows job "go test (race)" step; .gitattributes — go:embed LF block (cmd/apogee/defaults/**, internal/config/defaults/**);
Makefile — check target GOOS=windows go vet; docs/manual/building.md — Windows-tagged tests paragraph
**Tests:** none (config).
**Acceptance:**
- `git check-attr eol internal/skills/shipped/audit/SKILL.md | grep -q 'eol: lf'`
- `grep -q 'internal/auditsplit' .github/workflows/ci.yml`
- `grep -q 'internal/auditsplit' Makefile && grep -q 'internal/auditsplit' docs/manual/building.md`
**Commit:** `build: pin shipped skills to LF and test auditsplit on Windows`

## 8. Docs and ADR amendments

Depends on items 5 and 6.

**What:**
**Goal:** `docs/manual/workflows.md` lists `{apogee}` among the script-stage placeholders and says a
script stage runs through `cmd /c` on Windows (portable recipes call `{apogee}` or a native program);
ADR 0087 (D6) and ADR 0065 carry a dated 2026-10-10 amendment recording that audit's split is apogee's
own hidden subcommand, per ADR 0042; no doc, comment or test outside archived/historical records still
names `split.sh` as live.
**Approach (assumed at the header base):** edit the placeholder paragraph in the script-stage section;
append amendments in each ADR's existing amendment style. Rule for the sweep: every live mention of
`split.sh` — find with `git grep -n 'split\.sh' -- ':!docs/plans/archived' ':!docs/reviews' ':!CHANGELOG.md'`.
**Regression guard.** No `*_test.go` reads `workflows.md` or these ADRs, so no test line runs. A `split.sh`
used as an arbitrary fixture command in `internal/workflow` tests is not a reference to the shipped script
and stays. `docs/design/workflow-bench-experiment.md`'s mention is swept like any other. `workflows.md`
also states that `%VAR%` inside a Windows value is not neutralised (item 6's documented non-guarantee).
**Files:** docs/manual/workflows.md, docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md, docs/adr/0065-shipped-skills-and-the-load-skill-door.md, docs/design/workflow-bench-experiment.md
**Read first:** docs/manual/workflows.md — script-stage paragraph (placeholders), Inputs paragraph ("shell-quoted"); docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md — D6, "> **Amended …**" blockquotes;
docs/adr/0065-shipped-skills-and-the-load-skill-door.md — decision 1 "Amended 2026-09-27" blockquote; docs/design/workflow-bench-experiment.md — split.sh mention; internal/workflow/validate_test.go — split.sh fixture Run strings
**Tests:** none (no test reads these files).
**Acceptance:**
- `grep -q '{apogee}' docs/manual/workflows.md && grep -q 'cmd /c' docs/manual/workflows.md`
- `grep -q 'Amended 2026-10-10' docs/adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md && grep -q 'Amended 2026-10-10' docs/adr/0065-shipped-skills-and-the-load-skill-door.md`
- `git grep -n 'split\.sh' -- ':!docs/plans' ':!docs/reviews' ':!docs/adr' ':!CHANGELOG.md' ':!.beads' ':!internal/workflow/*_test.go'` prints nothing (ADR bodies and dated amendments are historical; the ADR amendments are checked above)
**Commit:** `docs: the audit split runs in apogee; {apogee} placeholder`
