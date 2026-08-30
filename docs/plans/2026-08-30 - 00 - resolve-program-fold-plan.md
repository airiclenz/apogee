# Resolve-program fold — the seven hand-rolled LookPath+fence pairs onto `security.ResolveProgram`

**Goal:** make `security.ResolveProgram` classify a real `exec.ErrDot` correctly, then fold every
remaining hand-rolled `exec.LookPath` + `RefuseExecFromWritablePath` pair onto it, so the resolver
is the only exec entry rather than the newest of eight. Closes `ISSUES.md`'s "Improvements / Ideas"
entry naming the five tool/present sites.

**Date:** 2026-08-30 · **Status:** unexecuted · **sized for:** ~200k-context host

**Authoritative sources:**
- `internal/security/execsafety.go:89-125` — `ResolveProgram`'s contract and its three-outcome doc
- `docs/plans/archived/2026-08-26 - 01 - safety-floors-subprocess-funnel-plan.md` — call 7 (the five
  sites deliberately out of scope); `ISSUES.md:28` — the entry this plan closes
- Base commit for all line references: `ef814369` (`main`, 2026-08-30)

**Ratified design calls:**
- **ErrDot is RELATIVE (user, 2026-08-30):** `ResolveProgram` checks `exec.ErrDot` before the absent
  wrap, so a `.`-on-PATH answer takes the fence refusal its doc already promises. Item 1 lands this;
  every later item depends on it.
- **Resolved path on refusal (user, 2026-08-30):** `ResolveProgram` ALSO returns the resolved path
  alongside the error on both refusal branches (RELATIVE and REFUSED: `return resolved, …`); the
  empty first return stays only for a genuine lookup failure, and `ResolveProgram`'s doc contract
  states that split. The four existing callers ignore the first return on error, so no other site
  changes.
- **Opener failure shape (user, 2026-08-30):** LOUD refusal — only a genuine not-found degrades to
  `ErrNoOpener`; both relative shapes (ErrDot and an error-free relative answer) wrap
  `security.ErrExecFromWritablePath`.
- **Scope (user, 2026-08-30):** all SEVEN pairs — the five named sites plus
  `internal/keystore/keystore.go` and `internal/mechanisms/autofix.go`.
- **Scope — the api-key command (user, 2026-08-30):** `internal/config/keyresolve.go`'s bare exec
  joins the plan as its own item.
- **api-key-cmd relative argv[0] (user, 2026-08-30):** before resolving, an `argv[0]` containing a
  path separator (the rule `exec.Command` itself uses) is made absolute with `filepath.Abs` against
  apogee's working directory — which is what `exec.Command` does with it today — and THAT absolute
  path is passed to `security.ResolveProgram`; a bare name with no separator goes through the PATH
  lookup unchanged. This keeps the wrapper script the manual tells operators to write
  (`api-key-cmd: bin/getkey.sh`, `docs/manual/configuration.md:629-630`) working unless it actually
  resolves inside the workspace, which is the case worth refusing.
- **api-key-cmd on the rootless commands (user, 2026-08-30):** `probe model` and `daemon` construct
  the resolver with an EMPTY root (`cmd/apogee/probemodel.go:105`, `cmd/apogee/daemonfire.go:107`), so
  `ResolveProgram`'s documented empty-fence rule (`internal/security/execsafety.go:38-40`) applies and
  their api-key command resolves exactly as it does today. The cost stated plainly: on `probe model`
  and `daemon` the api-key program is NOT fenced, because neither command has a workspace to measure it
  against. This yields to the documented decision at `cmd/apogee/probemodel.go:88-91` ("a workspace
  never changed this report"), which stays as written along with its pinning test
  `TestProbeModelRejectsTheWorkspaceFlag` (`cmd/apogee/probemodel_test.go:398`). The four ROOTED
  producers — `probe.go:89`, `wire_boot.go:64`, `keymigrate.go` (via `keyMigrator`),
  `wire_firing.go:111` — pass the `roots.workspace` they hold and fence normally. REPLACES the earlier
  CWD-default posture wording, whose warrant (`keystore.Probe` and the opener) was wrong: both of those
  run only under the rooted TUI wiring.
- **Injection seam (write-time):** each site keeps its dependency-injected look function, passed INTO
  `ResolveProgram` (the `settingsedit.go:394` precedent); the `(path, bool)` resolver vars die. A test
  fake fakes the LOOK, never the fence.
- **Python candidates (write-time):** the ordered `python3`→`python` probe stays; each candidate
  resolves through `ResolveProgram` in order; a fence refusal on a FOUND candidate is terminal — never
  a fall-through (same outcome as today's post-loop fence).
- **ErrDot wrap shape (write-time):** the relative refusal wraps `ErrExecFromWritablePath` only —
  `errors.Is(err, exec.ErrDot)` is NOT preserved, mirroring the absent branch's inverse contract.
- **Env untouched (write-time):** `safeGitEnv`, `subprocessEnvScopedPath`, `subprocessEnv`, `goVetEnv`
  and the opener's inherited env stay exactly as they are — the fold replaces the lookup+fence pair only.
- **Announced wordings preserved (write-time):** `git not available: no git executable found on PATH`,
  `python not available: no Python interpreter found on PATH (looked for python3, python)`,
  `…not available: the project's markers select %s, but no %q executable was found on PATH`,
  `go vet skipped: no 'go' toolchain found on PATH.`, `go vet skipped: <refusal>` (non-error result)
  and keystore's `ErrNoStore`-wrapped `%w: %s is not on this machine's PATH` survive byte-identical,
  selected by `errors.Is(err, security.ErrExecFromWritablePath)`.
- **Autofix silent degrade (write-time):** a refusal still means "absent, rung skipped" — no result
  surface exists to speak on.

**Regression check (2026-08-30, ef814369):**
- 1: recast — `ResolveProgram` also hands back the resolved path on both refusal branches
  (guard folded); re-check: guard extended — the ErrDot editor assertion is a NEW
  `TestExternalEditSpec…` case, not an existing one to re-assert, since no ErrDot look fake exists in
  `cmd/apogee/settingsedit_test.go` today and neither test file imports `os/exec`.
- 2: guard folded — the look-var test sweep is a rule with two greps, not the named subset.
- 3: guard folded — four inline look sites take the new signature, and every
  `refuseExecFromWritablePath` comment is re-pointed.
- 4: guard folded — the refusal test is table-ified with a per-row look; both tests already drive
  `Open`; re-check: unchanged — the reviewer's NOTES line about `ResolveProgram`'s RELATIVE branch
  does not reach this item: the opener's argv[0] is apogee's own fixed per-GOOS opener name, never an
  operator-written path.
- 6: guard folded — the refusal keeps naming the RESOLVED path; the item yields to
  `internal/keystore/keystore.go:133-137`, whose wording stays byte-identical.
- 7: NEW item, added at the re-check (user, 2026-08-30) — `internal/config/keyresolve.go:336`, the
  api-key command's bare exec, was the site the plan never named; it resolves through the resolver,
  and the item amends the opposite decision recorded at `keyresolve.go:307-318` (ADR 0047's no-shell
  rejection is untouched — `ResolveProgram` is a lookup, not a shell); re-check: recast — a relative
  argv[0] is made absolute before resolving (the manual's wrapper script survives), the empty-root
  warrant is deleted (`resolveRoots` substitutes `os.Getwd()`, so the fence rides `roots.workspace`
  including its CWD default), the producers become the `NewKeyResolver(` grep and take in the 18
  `cmd/apogee` test sites, `migrateKey` receives the root as a new parameter from `keyMigrator`, the
  `"failed"`-pinning `wantErr` row is re-pinned, and `docs/manual/configuration.md:629-630` gains the
  refusal clause; re-check (final): recast — the two ROOTLESS producers pass `""` and stay unfenced
  under `ResolveProgram`'s empty-fence rule, which REPLACES the CWD-default posture warrant just above
  (`probemodel.go:105` and `daemonfire.go:107` build the resolver with no root, so the item yields to
  `probemodel.go:88-91` and its pinning test); the producer grep gains `KeyResolver{` (the
  `&KeyResolver{…}` literal at `keyresolve_test.go:263` that the `NewKeyResolver(` form misses) and the
  `cmd/apogee` test sites are 17, not 18; the acceptance pattern becomes
  `MigrateKey|KeyMigrat|Probe|Boot|Firing`, because `KeyMigrate` matched no test in the package.
- 8 (was 7, renumbered): recast — call-form grep with one expected line, `doc.go:115-118`, and the
  contract gains a NEW dated amendment (guard folded); re-check: the header's "Out of scope" premise
  was wrong both ways and both entries are dropped (`internal/agent/treesnapshot.go` runs git through
  `tools.RunGitQuery`; `host.go:522` is the constant `failFastPreamble`, so no pipefail probe
  exists), and the `doc.go` enumeration gains `internal/config`'s api-key command.

**Standing requirements:**
- skills: coding-standards
- After item 6, no production code outside `internal/security/execsafety.go` may CALL `exec.LookPath`
  — the call form `exec\.LookPath\(` — except the one declared allowlist site item 8 names,
  `internal/platform/confinetest/lines_other.go:57`. A plain value assignment handing `exec.LookPath`
  to `ResolveProgram` as an injected default is the seam this plan preserves, not a violation.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Env/PATH redesign, the L5 accepted cost (in-repo venv refusals STAY), version bump (closing note only)

## 1. `security.ResolveProgram` refuses a real `exec.ErrDot` instead of calling it absent — ✅ DONE (2026-08-30)

**What:** Recast at the regression check (2026-08-30).
Regression from the resolver's own contract: `ResolveProgram` (`execsafety.go:109-125`)
returns on `err != nil` before the `!filepath.IsAbs` check, but Go's `exec.LookPath` answers a
relative PATH entry with a non-empty relative path AND `exec.ErrDot` — so the RELATIVE outcome its
doc promises (`:97-102`, which names `exec.ErrDot` explicitly) is unreachable from the production
default. Add, before the absent wrap: when `errors.Is(err, exec.ErrDot)`, take the relative branch
using the path `look` returned — `fmt.Errorf("%w: %s resolves to a relative program path",
ErrExecFromWritablePath, resolved)`. Any other lookup error stays ABSENT, `%w`-wrapped, and the
ErrDot refusal does NOT satisfy `errors.Is(err, exec.ErrNotFound)` (ratified wrap shape). Fix the
stale comment in `execsafety_test.go:138-139` that calls a nil-error relative answer "what
exec.LookPath returns for a relative PATH entry (exec.ErrDot)".
Classification flips for the four existing callers, each of which already renders the resolver's
error unchanged: `internal/tools/exec_common.go:478` (terminal/console argv[0]), `:595`
(`resolveShell`), `internal/mcp/transport.go:150` (stdio server command), `cmd/apogee/settingsedit.go:394`
(`resolveEditor` — its `errors.Is(…ErrExecFromWritablePath)` branch at `:395` now fires, so a
`.`-on-PATH editor announces `refusing to run editor %q` instead of the `cannot run editor %q…`
install hint). That flip is the point: a `.` PATH entry resolves against a working directory that is
the workspace.

**Files:** internal/security/execsafety.go, internal/security/execsafety_test.go, cmd/apogee/settingsedit_test.go

**Tests:** `TestResolveProgram` (`execsafety_test.go:111`) gains a subtest driving the REAL shape —
a look returning `("node_modules/.bin/tool", exec.ErrDot)` — asserting `errors.Is(err,
ErrExecFromWritablePath)`, the `resolves to a relative program path` sentence, and
`!errors.Is(err, exec.ErrNotFound)`. Both refusal subtests now capture the FIRST return and assert it
is the resolved path: the new ErrDot subtest and the in-root subtest (`:128-136`, "a program inside
the fence is refused by name"); the absent subtest keeps its empty first return. The existing
nil-error relative subtest (`:138-147`) and the absent subtest (`:149-159`) otherwise pass
unchanged. `cmd/apogee/settingsedit_test.go` gains a NEW `TestExternalEditSpec…` case (its name must contain
`Editor` or `Settings` so `-run 'Editor|Settings'` selects it) whose `e.look` returns
`("bin/vim", exec.ErrDot)` and which asserts the `refusing to run editor` wording — not the install
hint — plus `errors.Is(err, security.ErrExecFromWritablePath)`; the file gains the `os/exec` import,
as does `internal/security/execsafety_test.go`.

**Acceptance:** `go build ./... && go test ./internal/security/ ./internal/tools/ ./internal/mcp/ && go test ./cmd/apogee/ -run 'Editor|Settings'`

**Commit:** `fix(security): a relative PATH entry is refused by the resolver, not reported absent`

**Regression guard.** This is a deliberate user-visible message change on four live surfaces — the
announced-surface rule applies: the settingsedit test drives `resolveEditor` with the exact ErrDot
shape and asserts the emitted sentence. The ABSENT contract must not widen: a plain
`exec.ErrNotFound` still wraps to `%s not available: %w` and must never satisfy
`errors.Is(err, ErrExecFromWritablePath)` — the existing subtest pins it.
`ResolveProgram` ALSO returns the resolved path alongside the error on both refusal branches
(RELATIVE and REFUSED: `return resolved, …`); the empty first return stays only for a genuine lookup
failure, and `ResolveProgram`'s doc contract states that split (ratified by the user, 2026-08-30;
recorded in the header as **Resolved path on refusal**). The four existing callers ignore the first
return on error, so no other site changes; this item's Tests gain an assertion that the ErrDot and
in-root subtests hand back the resolved path.
The settingsedit assertion is an ADDED case, not a re-assertion: `cmd/apogee/settingsedit_test.go`
has no ErrDot look fake today (`editorAlwaysFound` at `:136`/`:182`/`:553`, a bare not-found error at
`:614`, two nil-error absolute paths at `:638`/`:663`), and neither it nor
`internal/security/execsafety_test.go` imports `os/exec` — both imports land with the case.

## 2. `internal/tools`: git and python_exec resolve through the resolver — ✅ DONE (2026-08-30)

NOTES (2026-08-30): consequential edit — internal/tools/run_tests.go: made necessary by lookGit/lookInterpreter changing shape; `lookTestProgram`'s doc claimed "the shape lookGit and lookInterpreter already use", which item 2 makes false. Only that clause was dropped — the var itself and its `ok=false` contract are item 3's.
NOTES (2026-08-30): `lookGit` and `lookInterpreter` are written as the plain value assignment `var x = exec.LookPath` (the injected-default seam the plan's standing requirement preserves) rather than a wrapper literal; the inferred type is the required `func(string) (string, error)`.
NOTES (2026-08-30): the new terminal-refusal case is a standalone test in `exec_fence_test.go` (`TestPythonExecRefusalIsTerminalAcrossTheCandidates`) rather than a row of a table — `TestPythonExecRefusesAnInRepoVirtualenvByName` beside it is a single-case function, and this case needs a per-candidate look fake no shared helper offers.

**Depends on item 1.**

**What:** `lookGit` (`git.go:143-146`) becomes a `func(string) (string, error)` var defaulting to
`exec.LookPath`; `resolveGit` (`git.go:166-175`) calls `security.ResolveProgram(lookGit, "git", root,
confinementBox(ctx))` and maps the outcome INSIDE `resolveGit`, not in `gitProgram`: sentinel
(`errors.Is(err, security.ErrExecFromWritablePath)`) → the error verbatim; any other error →
`errors.New(gitUnavailableMessage)` (`git.go:1232`). Mapping inside `resolveGit` is required because
`RunGitQuery` (`git.go:254`) consumes it directly, not through `gitProgram`'s render.
`lookInterpreter` (`python_exec.go:41-47`) becomes a `func(string) (string, error)` var defaulting to
`exec.LookPath` — its `[]string` parameter goes away; `Execute` (`python_exec.go:228-241`) loops
`pythonCandidates` calling `ResolveProgram(lookInterpreter, cand, t.root, confinementBox(ctx))` per
candidate: sentinel → terminal error result (no fall-through); other error → next candidate;
exhausted → the existing `python not available: …` wording. The in-function fence (`:239-241`) is
deleted — the resolver owns it.

**Files:** internal/tools/git.go, internal/tools/python_exec.go, internal/tools/git_test.go, internal/tools/python_exec_test.go, internal/tools/exec_fence_test.go

**Tests:** rework `withFakeGit` (`git_test.go:21-26`) and `withFakeInterpreter`
(`python_exec_test.go:25-30`) to fake the look func (planted absolute path with a nil error; `found`
false → `("", exec.ErrNotFound)`). `TestGit_GracefulWhenAbsent` (`git_test.go:172`),
`TestPythonExec_GracefulWhenAbsent` (`python_exec_test.go:104`) and
`TestRunGitQuery_RefusesAPlantedGit` (`git_test.go:1786`) pass unchanged. The `git` and `python_exec`
rows of the fence table (`exec_fence_test.go:62-86`) and
`TestPythonExecRefusesAnInRepoVirtualenvByName` (`:171-189`, including the not-"no Python interpreter
found" assertion) pass unchanged — all three go through the two helpers.
`TestPythonExec_WorkspaceDoesNotShadowTheStdlib`'s host probe (`python_exec_test.go:420`) calls
`lookInterpreter(pythonCandidates)` in today's `([]string) (string, bool)` form, so it reworks to a
loop over `pythonCandidates` calling `lookInterpreter(name)` and skipping while `err != nil`. New: a
`python3` planted inside the root with a clean system `python` still refuses (terminal, no
fall-through).

**Acceptance:** `go build ./... && go test ./internal/tools/`

**Commit:** `refactor(tools): git and python_exec resolve through security.ResolveProgram`

**Regression guard.** The resolver's PATH is apogee's inherited PATH at resolve time — the same PATH
the old `exec.LookPath` probed — so graceful degradation must not regress for hosts whose git or
python sit outside the workspace. Both refusals must keep naming the resolved path
(`security.EvalRealPath`); the fence rows pin it. `resolveGit` is the mapping site, not `gitProgram`:
a fold that maps in the renderer leaves `RunGitQuery` emitting the raw resolver error.
The look-var test sweep is a RULE, not the named list: a look-var signature change reaches EVERY
inline `look* = func(` assignment AND every direct `look*(` call in `internal/tools/*_test.go` — run
`grep -rn 'look\(Git\|Interpreter\|TestProgram\|Go\) *= *func(' internal/tools/*_test.go` and
`grep -rn 'lookGit(\|lookInterpreter(\|lookTestProgram(\|lookGo(' internal/tools/*_test.go`, each
re-run until only the new signature remains. This item's own sweep includes
`internal/tools/python_exec_test.go:420` (`interp, found := lookInterpreter(pythonCandidates)` — the
`[]string` parameter disappears), which the write-time list omitted.

## 3. `internal/tools`: run_tests and diagnostics resolve through the resolver — ✅ DONE (2026-08-30)

NOTES (2026-08-30): `lookTestProgram` and `lookGo` are written as the plain value assignment `var x = exec.LookPath` (the injected-default seam the plan's standing requirement preserves) rather than a wrapper literal; the inferred type is the required `func(string) (string, error)`, and `lookGo` gains the name parameter it lacked.
NOTES (2026-08-30): the prose re-point is a rule, not the enumerated list — beyond `internal/tools/exec_common.go:124`, `path_safety.go`'s `confinementBox` doc said the box is passed "to the fence above", which named the deleted helper; it now names `security.ResolveProgram` and carries over the deleted paragraph's point (every PATH-reaching tool — git, python_exec, run_tests, diagnostics — goes through it, so bytes the model may write never become argv[0]). `grep -rn 'refuseExecFromWritablePath' internal/tools/` ends empty, comments included.
NOTES (2026-08-30): no import became unused by the deletion — `path_safety.go` still needs `domain` (confinementBox's return) and `security` (ResolveInRoot, ErrPathEscape, the write primitives); `internal/tools` production code now has zero `exec.LookPath(` CALL sites, only the four look-var value assignments.

**Depends on item 2** (shares `exec_fence_test.go`).

**What:** `lookTestProgram` (`run_tests.go:386-389`) and `lookGo` (`diagnostics.go:406-409`) become
`func(string) (string, error)` vars defaulting to `exec.LookPath` (`lookGo` gains the name parameter
it lacks today). `RunTests.Execute` (`run_tests.go:239-251`):
`security.ResolveProgram(lookTestProgram, runner.program, t.root, confinementBox(ctx))` — sentinel →
refusal verbatim; otherwise → the existing
`"%s not available: the project's markers select %s, but no %q executable was found on PATH"`.
`Diagnostics.diagnoseGo` (`diagnostics.go:169`; its lookup+fence pair is `:193-205`): same resolution
for `"go"` — sentinel → `go vet skipped: <refusal>` on the NON-error result; otherwise →
`go vet skipped: no 'go' toolchain found on PATH.`. Both in-function fence calls (`run_tests.go:249`,
`diagnostics.go:200`) are deleted. With the last two callers gone, delete
`refuseExecFromWritablePath` from `internal/tools/path_safety.go:40-42` and any import it alone
needed; `grep -n 'refuseExecFromWritablePath' internal/tools/*.go` must end empty.

**Files:** internal/tools/run_tests.go, internal/tools/diagnostics.go, internal/tools/path_safety.go, internal/tools/exec_common.go, internal/tools/run_tests_test.go, internal/tools/diagnostics_test.go, internal/tools/exec_fence_test.go

**Tests:** `TestRunTestsMissingRunnerProgramDegradesGracefully` (`run_tests_test.go:333`) and
`TestDiagnostics_CleanGoFileWithVetSkipNote` (`diagnostics_test.go:208`) keep every assertion and
wording they have — but the first sets its look var INLINE (`:335`), so its fake takes the new
signature. `withFakeGo` (`diagnostics_test.go:28-33`) reworks to the look-func fake, so the
diagnostics fence row (`exec_fence_test.go:103-118`, `wantError: false`) passes unchanged; `requireGo`
(`run_tests_test.go:537-541`) reworks from `if _, ok := lookTestProgram("go"); !ok` to the error form.
FOUR inline sites assign a look var and ALL take the new `func(string) (string, error)` signature in
this item: the run_tests fence row (`exec_fence_test.go:95-97`, `planted, nil`, keeping its planted
path and assertions), `run_tests_test.go:335` (`"", exec.ErrNotFound`), `:363` and `:401`
(`program, nil`).

**Acceptance:** `go build ./... && go test ./internal/tools/`

**Commit:** `refactor(tools): run_tests and diagnostics resolve through security.ResolveProgram`

**Regression guard.** diagnostics' vet-skip note rides a SUCCESS result — a fold that turns the
refusal into an error result breaks the clean-syntax verdict contract; the `wantError: false` fence
row pins it. run_tests' wording embeds `runner.name` twice and the quoted `runner.program` — assert
all three survive verbatim. The `path_safety.go` deletion belongs to THIS item: after item 2 the
helper still has these two callers, so an earlier delete does not compile.
Same look-var sweep rule and the same two greps as item 2. This item's own sweep includes
`internal/tools/run_tests_test.go:335`, `:363` and `:401` — three further inline
`lookTestProgram = func(string) (string, bool)` assignments beyond the fence row at
`exec_fence_test.go:96` and `requireGo` at `:537`. Separately, the prose guard is a rule not a list:
every comment naming `refuseExecFromWritablePath` is re-pointed to `security.ResolveProgram`,
`internal/tools/exec_common.go:124` being the prose site the write-time list missed, so
`grep -rn 'refuseExecFromWritablePath' internal/tools/` must end empty with comments included.

## 4. `internal/present`: the opener resolves through the resolver — loud refusal — ✅ DONE (2026-08-30)

NOTES (2026-08-30): the file's `exec` and `filepath` imports STAY — the item's "if nothing else uses them" condition is not met: `exec.Command` at `opener.go:434` (the launch itself) and `filepath.Ext` at `opener.go:346` (OpenerRenderable) are unrelated users. `grep -n 'exec\.LookPath' internal/present/*.go` now matches only comment text, no call form.
NOTES (2026-08-30): `TestOpenerDegradesWhenTheProgramDoesNotResolve` keeps its table with the single surviving "nothing on PATH" row rather than being flattened to a straight-line test — the item says the subtest is what survives, and the row's name is what states which outcome still degrades.

**Depends on item 1** (the ErrDot classification is what makes this item's ratified call reachable).

**What:** `Opener.resolveProgram` (`opener.go:228-241`) collapses to
`security.ResolveProgram(o.LookPath, program, o.WorkspaceRoot, nil)` — `o.LookPath` already has the
resolver's `func(string) (string, error)` shape and its nil default moves into `ResolveProgram`; the
package-local `refuseExecFromWritablePath` (`:243-252`) and the file's `exec`/`filepath` imports go if
nothing else uses them. Outcome map: `errors.Is(err, security.ErrExecFromWritablePath)` → wrap as
`present: refusing to launch %s: %w` (`:238` wording kept); any other error → `ErrNoOpener`. Rewrite
the doc comment's two-directions paragraph (`opener.go:210-221`): NOT FOUND → `ErrNoOpener` (normal
degradation, ADR 0019 §4); REFUSED (in-root, ErrDot, or an error-free relative answer) → loud refusal
naming the resolved path.

**Files:** internal/present/opener.go, internal/present/opener_test.go

**Tests:** `TestOpenerRefusesAProgramInsideTheWorkspace` (`opener_test.go:628`) is a single-case
function today, not a table, and its body asserts `strings.Contains(err.Error(),
security.EvalRealPath(planted))` (`:659`) — an assertion no relative row can satisfy: table-ify it
with a per-row `look` and gate the planted-path assertion on the in-workspace row, whose own
assertions are otherwise unchanged.
`TestOpenerDegradesWhenTheProgramDoesNotResolve` (`opener_test.go:672`) keeps only its "nothing on
PATH" subtest (`exec.ErrNotFound` → `ErrNoOpener`, runner not invoked); its two relative subtests —
"found only through a relative PATH entry (ErrDot)" and "a relative answer with no error at all" —
move to the refusal test as cases asserting `errors.Is(err, security.ErrExecFromWritablePath)`,
`!errors.Is(err, ErrNoOpener)`, a message naming the program, and `len(runner.calls) == 0`. The
paragraph above `:672` claiming the relative rows take "the machine's answer" is rewritten with them.

**Acceptance:** `go build ./... && go test ./internal/present/`

**Commit:** `refactor(present): the opener resolves through security.ResolveProgram and refuses a relative lookup loudly`

**Regression guard.** Deliberate user-visible change (silent → loud) — the announced-surface rule
applies: both relative cases drive `Opener.Open` and assert the emitted sentence plus that nothing
was launched. The not-found → `ErrNoOpener` ladder (transcript rung still presents the document) must
not regress. Both existing tests ALREADY drive `Open` (`opener_test.go:650`, `:705`), so neither is
rewritten onto `resolveProgram` — the refusal test's only change is the table-ification the rows need
and the gating of its planted-path assertion (`:659`).

## 5. `internal/mechanisms`: autofix resolves formatters through the resolver — ✅ DONE (2026-08-30)

NOTES (2026-08-30): `TestAutofixRefusesAFormatterInsideTheWritableBox` is a set of arms sharing a `ladder` helper, not a table, so the item's "new cases on the existing table" landed as new arms; `ladder` was generalised from a planted `path` to a per-arm `look func(string) (string, error)` (the seam the ErrDot arm needs) with a `build` helper beneath it, and all three existing arms keep their assertions and messages byte-identical.
NOTES (2026-08-30): the construction comment keeps the ratified rationale verbatim (a permit may authorise an unfenced spawn) and gains the relative-answer outcome, since every resolver error — absent, refused, relative — now collapses to the same `""`.

**Depends on item 1.**

**What:** the construction probe (`autofix.go:116-142`) replaces `look(command)` +
`refuseExecFromWritablePath(p, …)` with one
`security.ResolveProgram(deps.LookPath, command, workspaceRoot, &deps.WritableBox)` call —
`deps.LookPath` (`catalogue.go:35-39`) already has the resolver's shape and stays nil-able, so the
local `look` default at `:118-121` and the file's `exec` import go away. Any error ⇒ `""` (absent,
rung skipped): the silent degrade is ratified and pinned by the comment at `:129-137`; the fence stays
at the single construction-time resolution. Delete the package-local `refuseExecFromWritablePath`
(`:104-109`) once `grep -n 'refuseExecFromWritablePath' internal/mechanisms/*.go` is empty. Behaviour
is identical to today on every input: absent on not-found, absent on refusal, absent on relative.

**Files:** internal/mechanisms/autofix.go, internal/mechanisms/autofix_test.go

**Tests:** `TestAutofixMissingExternalFormatterDegrades` (`autofix_test.go:282`),
`TestAutofixProbesFormattersAtConstructionOnly` (`:205`) and `TestAutofixConfinesTheFormatterBeforeRunning`
(`:384`) pass unchanged. New cases on the existing table: a formatter planted inside the writable box
→ its rung skipped, no error surfaced, Go's in-process gofmt tail still appended; a look answering
`("bin/fmt", exec.ErrDot)` → same silent skip.

**Acceptance:** `go build ./... && go test ./internal/mechanisms/`

**Commit:** `refactor(mechanisms): autofix resolves formatters through security.ResolveProgram`

**Regression guard.** The refusal must stay SILENT here (no result surface) — do not import item 4's
loud call. The spawn-door re-judgment at fire time and the `WritableBox` passing are untouched; the
construction comment's rationale (a permit may authorise an unfenced spawn) stays verbatim. No file is
added, renamed or deleted in `internal/mechanisms`, so `doc.go` and its `docmap_test` are untouched.

## 6. `internal/keystore`: the store probe resolves through the resolver

**Depends on item 1.**

**What:** `probe` (`keystore.go:103`) already takes `lookPath func(string) (string, error)` — the
resolver's exact shape. Both platform arms — darwin/`keychainProgram` (`:105-112`) and
linux/`secretServiceProgram` (`:114-128`) — replace their `lookPath(program)` + `fenceProgram(program,
workspaceRoot)` pair with one `security.ResolveProgram(lookPath, program, workspaceRoot, nil)` call.
Both boundary wordings are re-created from the returned error and neither may lose its sentinel:
`errors.Is(err, security.ErrExecFromWritablePath)` → `fenceProgram`'s wrap
`keystore: refusing to run the secret store tool %s: %w` (`:133-143`, kept, now taking the resolver's
error rather than calling `security.RefuseExecFromWritablePath` itself); any other error →
`fmt.Errorf("%w: %s is not on this machine's PATH", ErrNoStore, program)`, byte-identical to today —
callers select on `errors.Is(err, ErrNoStore)` (`cmd/apogee/keymigrate.go:48`). `Probe` (`:95-97`)
passes `nil` instead of `exec.LookPath`, so the package's non-test `exec` import goes away; `probe`'s
signature and its doc are unchanged, and the linux arm's `store.answers()` check keeps its position
after the resolution.

**Files:** internal/keystore/keystore.go, internal/keystore/keystore_test.go

**Tests:** `TestProbeRefusesAStoreProgramInsideTheWorkspace` (`keystore_test.go:540`) passes unchanged
— both arms, sentinel present, `ErrNoStore` absent, message naming `security.EvalRealPath(planted)`.
The absent-store test keeps its `ErrNoStore` assertion and wording.
The ErrDot case cannot ride that table as it stands — its rows are `{name, goos, program}`
(`:541-548`), the body plants a real tool
unconditionally (`:553`), calls `probe(tc.goos, exec.LookPath, …)` (`:555`) and asserts the planted
path (`:563`): give the table a `look func(string) (string, error)` row field (nil ⇒ plant +
`exec.LookPath`) and gate the plant and the planted-path assertion on it, or give the ErrDot case its
own test. Either way a look answering `("bin/secret-tool", exec.ErrDot)` per arm is refused with the
sentinel, NOT `ErrNoStore`.

**Acceptance:** `go build ./... && go test ./internal/keystore/ && go test ./cmd/apogee/ -run 'KeyMigrate|Keystore'`

**Commit:** `refactor(keystore): the secret-store probe resolves through security.ResolveProgram`

**Regression guard.** `ErrNoStore` is the load-bearing sentinel, not the wording: `keymigrate.go:48`
branches on it, so an absent store that arrives as the resolver's bare `%s not available: %w` would
turn "this machine has no store" into a hard migration error. Both arms re-wrap; the test asserts
`errors.Is(err, ErrNoStore)` on the absent path and its absence on the refused path.
The keystore refusal keeps naming the RESOLVED path in its own `%s` slot, taken from the path item 1
now returns alongside the refusal error; `fenceProgram`'s doc paragraph
(`internal/keystore/keystore.go:133-137`, "it names the resolved path … rather than the name that was
looked up") stays exactly as written and the message stays byte-identical — this item yields to that
documented decision rather than reversing it.

## 7. `internal/config`: the api-key command resolves through the resolver

**Depends on item 1.**

**What:** Recast at the regression check (2026-08-30).
`runKeyCommand` (`internal/config/keyresolve.go:320`) hands `argv[0]` straight to
`exec.CommandContext` (`:336`) with no lookup and no fence — the last unfenced exec in production
code. Thread the workspace root to it: `NewKeyResolver(workspaceRoot string)` (`:153`) stores it on
`KeyResolver`, `Resolve` (`:173`) passes it through `resolveKeySource` (`:238`) to
`runKeyCommand(entry, command, workspaceRoot, timeout)`. Before resolving, an `argv[0]` containing a
path separator (the rule `exec.Command` itself uses) is made absolute with `filepath.Abs` against
apogee's working directory — which is what `exec.Command` does with it today — and THAT absolute path
is what `security.ResolveProgram(nil, argv0, workspaceRoot, nil)` gets; a bare name with no separator
goes through the PATH lookup unchanged. The returned absolute path becomes argv[0]. nil box: the
command runs on apogee's own behalf before any confinement box exists (the keystore and opener
precedent). The fence is the root each producer holds. The four ROOTED producers — `probe.go:89`,
`wire_boot.go:64`, `keymigrate.go` (via `keyMigrator`), `wire_firing.go:111` — hand over the
`roots.workspace` (`wire.go:333`) they already hold and fence normally. The two ROOTLESS commands pass
`""`: `cmd/apogee/probemodel.go:105` and `cmd/apogee/daemonfire.go:107` construct the resolver with an
EMPTY root, so `ResolveProgram`'s documented empty-fence rule (`internal/security/execsafety.go:38-40`)
applies and their api-key command resolves exactly as it does today. The cost stated plainly: on
`probe model` and `daemon` the api-key program is not fenced, because neither command has a workspace
to measure it against — the item yields there to the documented decision at
`cmd/apogee/probemodel.go:88-91` ("a workspace never changed this report"), which stays as written
along with its pinning test `TestProbeModelRejectsTheWorkspaceFlag`
(`cmd/apogee/probemodel_test.go:398`). Producers are a rule plus a grep, not a closed list: every
construction site takes the new signature —
`grep -rnE 'NewKeyResolver\(|KeyResolver\{' --include=*.go .`, the form that also catches the direct
struct literals `NewKeyResolver(` misses — which is the six in `cmd/apogee` production code
(`probe.go:89`, `wire_boot.go:64`, `keymigrate.go:214`, `probemodel.go:105`, `daemonfire.go:107`,
`wire_firing.go:111`) AND the 17 in its test files
(`keysource_test.go:82,119,159,197,247,273`, `wire_server_test.go:608,1457,1544,1701,1773,1833`,
`delegation_test.go:70,285,383,590`, `upstream_test.go:903`), without which the `cmd/apogee` test
package does not compile and this item's own acceptance cannot run.
`internal/config/keyresolve_test.go:263` builds the resolver as `&KeyResolver{commandTimeout:
tt.timeout}`, so every row of `TestKeyResolverCommandSource` carries an empty root and a refusal row
added there could not bite: the refusal and outside-the-workspace tests are built with
`NewKeyResolver(root)` instead. `migrateKey` (`cmd/apogee/keymigrate.go:208`) is a package-level func
with no roots in scope, so it takes the workspace root as a NEW parameter threaded from `keyMigrator`
(`keymigrate.go:159-172`, which holds `w.roots.workspace` — the `prepareKeyMigration` precedent at
`:71-80`), and its three callers at `keymigrate_test.go:144`, `:197` and `:220` change with it.
Failure shapes: `errors.Is(err, security.ErrExecFromWritablePath)` →
`fmt.Errorf("apogee: server %q: api-key-cmd: refusing to run %q: %w", entry, argv[0], err)` (the
keystore wrap's shape); any other resolver error →
`fmt.Errorf("apogee: server %q: api-key-cmd: %q is not on this machine's PATH", entry, argv[0])`,
beside the existing "names no program" family. `internal/config` gains the `internal/security`
import — the sibling edge `internal/tools`, `internal/keystore`, `internal/mechanisms`,
`internal/present` and `internal/mcp` already carry; ADR 0010's flow toward `internal/domain` is
unaffected. Amend the doc paragraph at `:307-318`, which records the opposite decision ("argv[0]
resolved the way the user's own shell would resolve it, their config and their PATH"): it still
resolves on the user's PATH, but a program resolving INSIDE the workspace is refused before it runs —
the config file is the operator's, the workspace is the model's, and an `api-key-cmd` landing in the
latter hands the model the credential this key source exists to protect. The manual's `api-key-cmd`
paragraph (`docs/manual/configuration.md:629-630`) gains the matching clause: the command's program is
resolved before it runs and refused if it lands inside the workspace, so a wrapper script belongs
outside the workspace or is named by an absolute path.

**Files:** internal/config/keyresolve.go, internal/config/keyresolve_test.go, cmd/apogee/probe.go,
cmd/apogee/wire_boot.go, cmd/apogee/probemodel.go, cmd/apogee/daemonfire.go,
cmd/apogee/wire_firing.go, cmd/apogee/keymigrate.go, cmd/apogee/keymigrate_test.go,
cmd/apogee/keysource_test.go, cmd/apogee/wire_server_test.go, cmd/apogee/delegation_test.go,
cmd/apogee/upstream_test.go, docs/manual/configuration.md

**Tests:** `internal/config/keyresolve_test.go`: an `api-key-cmd` whose argv[0] is planted inside the
workspace root is refused with the sentinel and a message naming the resolved path, and NEVER runs
(the planted script writes a marker file the test asserts absent); an absent argv[0] gives the
not-on-PATH wording; an argv[0] outside the root resolves and its stdout is still the key (existing
happy-path tests pass unchanged); a relative argv[0] carrying a separator (`bin/getkey.sh`, the
manual's wrapper-script shape) is made absolute against the working directory and still runs when it
lands outside the workspace. Those refusal and outside-the-workspace rows are built with
`NewKeyResolver(root)`, never the `&KeyResolver{commandTimeout: …}` literal at
`internal/config/keyresolve_test.go:263` whose empty root refuses nothing; one row keeps that
empty-root shape on purpose — a resolver built with `""` (the `probe model` / `daemon` shape) still
runs a command resolving inside the workspace, pinning the empty-fence rule those two commands
inherit. The existing row "a program that is not on PATH refuses"
(`internal/config/keyresolve_test.go:255-257`) pins `wantErr:
[]string{"openrouter", "failed"}` and the new wording carries no "failed": its `wantErr` becomes
`{"openrouter", "is not on this machine's PATH"}`. `cmd/apogee/keymigrate_test.go`'s three
`migrateKey` callers (`:144`, `:197`, `:220`) take the new workspace-root parameter.

**Acceptance:** `go build ./... && go test ./internal/config/ -run 'Key|Resolve' && go test ./cmd/apogee/ -run 'MigrateKey|KeyMigrat|Probe|Boot|Firing'`

**Commit:** `fix(config): the api-key command resolves through the exec fence instead of running bare`

**Regression guard.** A new refusal on an operator-configured command — the announced-surface rule
applies: the refusal test asserts the exact emitted sentence and that nothing ran. A relative argv[0]
is made absolute against apogee's working directory before resolving, so the wrapper script the manual
tells operators to write keeps working unless it actually resolves inside the workspace. The two
ROOTLESS producers (`probemodel.go:105`, `daemonfire.go:107`) pass `""` and stay unfenced under
`ResolveProgram`'s empty-fence rule; the item yields there to `cmd/apogee/probemodel.go:88-91` and its
pinning test `TestProbeModelRejectsTheWorkspaceFlag`. Producers are the
`grep -rnE 'NewKeyResolver\(|KeyResolver\{'` rule: the 17 `cmd/apogee` test sites must take the new
signature or this item's own acceptance cannot compile, and the refusal tests are built with
`NewKeyResolver(root)` because the `&KeyResolver{…}` literal at `keyresolve_test.go:263` carries an
empty root that refuses nothing. The acceptance pattern is `MigrateKey|KeyMigrat|Probe|Boot|Firing` —
the old `KeyMigrate` matched no test, so the three `migrateKey` callers never ran under it.

## 8. Docs and the ISSUES close — the resolver is the only exec entry

**Depends on item 7.**

**What:** Recast at the regression check (2026-08-30).
Remove the `ISSUES.md` "Improvements / Ideas" entry that names the five LookPath+fence pairs
(currently `ISSUES.md:28`; locate it by the text `fold the five hand-rolled`, since earlier items and
other runs shift the line) — executed work leaves the file, `CHANGELOG.md` gets the record at
closeout. Extend the `ResolveProgram` paragraph at `internal/security/doc.go:115-118` (NOT the
`RefuseExecFromWritablePath` paragraph at `:110-112`): `ResolveProgram` is the exec fence's complete
form AND the only exec entry — every site (the shells, the hook door, MCP stdio, the settings editor,
git, python_exec, run_tests, diagnostics, the opener, autofix, the keystore probe, `internal/config`'s
api-key command) resolves through it, and the two declared exceptions are `internal/platform/confinetest` (a test-support package) and
the injected defaults callers hand to `ResolveProgram` itself.
Do NOT rewrite the dated `> **Amended 2026-08-12**` record in
`docs/design/confinement-execution-contract.md` (`:480` ff.) — the file's convention is to APPEND:
add a new `> **Amended 2026-08-30 (the resolver is the only exec entry).**` block after the 2026-08-13
one (`:498-507`), stating that the package-local wrappers the 2026-08-12 record names in
`internal/tools`, `internal/mechanisms` and `internal/present` are gone and every site now calls
`security.ResolveProgram`.

**Files:** ISSUES.md, internal/security/doc.go, docs/design/confinement-execution-contract.md

**Tests:** none — docs-only; the compile in Acceptance covers `doc.go`.

**Acceptance:** `go build ./...`, `grep -c 'fold the five hand-rolled' ISSUES.md` → `0`,
`grep -rn 'refuseExecFromWritablePath' --include='*.go' internal/ cmd/` → empty (items 3, 4 and 5
delete the three package-local wrappers), and the standing-requirement grep in its CALL form, whose
ONLY expected line is `internal/platform/confinetest/lines_other.go:57` — any other line is a
finding:
`grep -rn "exec\.LookPath(" --include="*.go" internal/ cmd/ | grep -v _test.go | grep -v internal/security/execsafety.go`

**Regression guard.** Three corrections plus a scope fix (user, 2026-08-30). (a) The acceptance grep
uses the CALL form `exec\.LookPath\(`: after items 2-6 every surviving `exec.LookPath` reference is
either a plain value assignment (the four `internal/tools` look vars, `present.Opener`'s field
default, `cmd/apogee/settingsedit.go:150`) or a doc comment, so the ONLY expected line is
`internal/platform/confinetest/lines_other.go:57` and any other line is a finding; the header's
standing requirement is reworded to that same call-form rule and its bare-name `exec.Command` half is
DROPPED — `internal/agent/treesnapshot.go` is the only such site and it is not a bare exec at all:
it runs git through `tools.RunGitQuery` (`:116`), covered transitively by item 2 (see (e)). (b) The sentence to extend is `internal/security/doc.go:115-118` (the `ResolveProgram`
paragraph), NOT `:110-112` (the `RefuseExecFromWritablePath` paragraph). (c) Do NOT rewrite
`docs/design/confinement-execution-contract.md:480-486` — that is a dated `> **Amended 2026-08-12**`
record and the file's convention (`:103`, `:116`, `:143`, `:437`, `:447`, `:454`, `:469`) is to
APPEND; add a new `> **Amended 2026-08-30 (the resolver is the only exec entry).**` block stating that
the package-local wrappers named in the 2026-08-12 record are gone and every site now calls
`security.ResolveProgram`. (d) Acceptance also runs
`grep -rn 'refuseExecFromWritablePath' --include='*.go' internal/ cmd/` → empty, since items 3, 4 and
5 delete the three package-local wrappers. (e) Re-check corrections (user, 2026-08-30): the header's
"Out of scope" list was wrong on both entries and both are REMOVED — `internal/agent/treesnapshot.go`
runs git through `tools.RunGitQuery` (`:116`, "not a bare exec"), so item 2 already covers it
transitively and it has no ISSUES entry, and `internal/platform/host.go:522` is the constant
`failFastPreamble`, whose comment says the line self-detects pipefail "instead of asking a
subprocess", so no probe exists. The goal sentence and this item's `doc.go` sentence may now claim
the resolver is the only exec entry WITHOUT qualification, because new item 7 closes
`internal/config/keyresolve.go:336`; the doc.go enumeration gains `internal/config`'s api-key
command. The acceptance greps already sweep `internal/` and `cmd/` whole, so they cover
`internal/config/keyresolve.go` without a scope change.

---

**Suggested version bump:** minor (0.x) — items 1 and 4 change announced wordings on live surfaces
(the editor refusal, the opener's relative-path refusal); the rest is internal. Owner decides; not
performed by this plan.
