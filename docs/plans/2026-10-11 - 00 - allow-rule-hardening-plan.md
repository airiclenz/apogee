# Allow-rule hardening — env vectors, CDPATH and interpreter suggestions

**Goal:** a terminal line answered by an allow rule runs without the inherited environment vectors the pure matcher cannot see (CDPATH, BASH_ENV, ENV, exported bash functions), a scoped PATH never degrades to a current-directory search, and the approval pane's rule suggestion never offers a bare interpreter name that would allow any script.

**Date:** 2026-10-11
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** ac1c8f0c
**Closes:** apogee-allow-rule-env-vectors

**Sources:**
- `docs/adr/0096-project-config-grants-are-live-only-by-adoption.md` (§2 matcher, §3 where a rule answers)
- `docs/plans/archived/2026-10-10 - 00 - project-config-and-allow-rules-plan.md` (items 9, 11)
- `CONTEXT.md` — **Allow rule**, **Project config**, **Adoption**
- `docs/manual/configuration.md` — "Commands that run without asking — `allow:`"
- beads `apogee-allow-cd-cdpath`, `apogee-allow-rule-env-vectors`, `apogee-allow-suggest-interpreters`

**Ratified design calls** (owner, 2026-10-11):
- **Env vectors:** scrubbed for rule-answered terminal calls only — the engine marks the call on its context, `Terminal.Execute` drops `CDPATH`, `BASH_ENV`, `ENV` and every `BASH_FUNC_*` for that call; a human-approved line keeps the operator's full environment.
- **Interpreters:** the suggestion lookup strips a trailing version (`python3.12`, `ruby3.2`, `lua5.4`) and the set gains `dash ksh mksh yash fish csh tcsh nodejs php lua luajit tclsh Rscript pwsh awk gawk mawk nawk`.

**Standing requirements:**
- skills: coding-standards
- This box is a Raspberry Pi: never run `go test -race`/`-cover` over a whole package or pattern; narrow to one test (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/<pkg>/`); never two test runs at once.

**Out of scope:**
- Windows `cmd` lines (a rule never matches on a non-POSIX shell).
- Env of `python_exec`, `console_open`, hooks and `run_tests` (never rule-answered).
- Bare-leader suggestions for non-interpreter commands (`rm`, `sed`).

**Regression check (2026-10-11, ac1c8f0c):**
- 1: guard folded (§11 carrier row; forced-fallback test case).
- 2: guard folded (assert on the captured spec; journey built on one workspace root).
- 3: recast — widened to `hostRules.ScopeEnv` (writer's decision).
- 4: guard folded (doc grep scoped to the real sites; doc.go and configuration.md dropped from Files).
- 3 (re-check): recast — POSIX fallback `PATH=/usr/bin:/bin`, Windows omits PATH; supersedes the first round's omit (writer's decision).
- 3 (re-check, follow-up): guard folded — fallback fires only for a non-empty root with a PATH entry present; its entries pass the in-workspace filter (PATH omitted if none survive); scoping doc comments updated (writer's decision).

## 1. Mark a rule-answered call on its context — ✅ DONE (2026-10-11)

NOTES (2026-10-11): executeGate installs the marker after the deny branch (only an allowed call runs), not before approve; a confineOnAllow gate is always forced, so a rule never answers it and the marker cannot reach executeConfineFallback — stated in the inline comment.
NOTES (2026-10-11): the §11 row names `tools.Terminal.Execute` as reader, per the plan; that read lands with item 2, so until then the marker has no production reader.
NOTES (2026-10-11): approve's doc comment gains "and mark the call's context (domain.WithRuleAnswered)" — a one-clause doc touch in a listed file, describing the new use of its returned rules.

**What:**
**Goal:** a tool executed because an allow rule answered its gate can tell so from its `context.Context` (`domain.RuleAnswered(ctx) == true`); a call approved by a human, a session allow, or the forced confinement fallback reads `false`.
**Approach (assumed at the header base):** add `domain.WithRuleAnswered(ctx) context.Context` and `domain.RuleAnswered(ctx) bool` beside the existing permit helpers (`WithSubprocessPermit`, `WithWriteEscapePermit`) in `internal/domain/confinement.go`, with an unexported key type. In `internal/agent/dispatch.go`, `executeGate` wraps the ctx with `WithRuleAnswered` when the `approve` result carries covering rules (the branch that sets `AuditAllowedByRule`), before it calls `executeConfine` / `executeRun`. `executeConfineFallback` never sets it.
**Regression guard.** The new key is a per-call ctx carrier: give it a row in `docs/design/confinement-execution-contract.md` §11 ("A new carrier gets a row here, next to its With… pair") — installer `executeGate` (reached from `runCall` and `recipe.go` `runScriptCall`), reader `Terminal.Execute`, lifetime one tool execution. The Goal's forced-fallback clause is pinned by a test case: an Auto Confine whose tool returns `domain.ErrConfinementUnavailable`, demote approved by the human, recorded ctx unmarked (precedent: `TestDisposition_RuntimeConfineUnavailable_DemotesToApproval`).
**Files:** internal/domain/confinement.go, internal/domain/confinement_test.go, internal/agent/dispatch.go, internal/agent/dispatch_test.go, docs/design/confinement-execution-contract.md
**Read first:** internal/agent/dispatch.go — executeGate, approve, executeConfineFallback, executeRun, executeToolDemotable; internal/agent/recipe.go — runScriptCall; internal/domain/confinement.go — WithWriteEscapePermit, WriteEscapePermitFrom;
internal/agent/dispatch_test.go — ruleTerminal, allowRuleConfig, newRuleAgent, TestAllowRuleAnswersAnOrdinaryGate; internal/agent/gate_test.go — prepareAndRun; docs/design/confinement-execution-contract.md — §11 Per-call context carriers
**Tests:** `TestRuleAnsweredRoundTrips` (domain); in `dispatch_test.go` beside `TestAllowRuleAnswersAnOrdinaryGate`, `TestAllowRuleMarksTheCallContext` — a recording fake tool sees the marker on a rule answer and not on a human approval, a session allow, or a human-approved forced confinement fallback (an Auto Confine whose tool returns `domain.ErrConfinementUnavailable`).
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRuleAnswered' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAllowRule' ./internal/agent/`
**Commit:** `feat(agent): mark a rule-answered call on its context`

## 2. Scrub env vectors from rule-answered terminal lines — ✅ DONE (2026-10-11)

**What:** fixes `apogee-allow-cd-cdpath` and the CDPATH/BASH_FUNC half of `apogee-allow-rule-env-vectors`: a rule-covered `cd sub` follows an inherited CDPATH out of Root, and exported bash functions or BASH_ENV/ENV run code the matcher never saw. Depends on item 1.
**Goal:** when `domain.RuleAnswered(ctx)` holds, the terminal child's environment carries no `CDPATH`, `BASH_ENV`, `ENV` or `BASH_FUNC_*` entry; without the marker the child environment is unchanged.
**Approach (assumed at the header base):** one unexported helper `scrubRuleAnsweredEnv(env []string) []string` in `internal/tools/exec_common.go` (one list of names plus the `BASH_FUNC_` prefix, matched on the name before `=`); `Terminal.Execute` applies it to the result of `subprocessEnvScopedPath` when the marker is set. Docs: one sentence in the `allow:` section of `docs/manual/configuration.md` and one clause in `CONTEXT.md`'s **Allow rule** paragraph stating the scrub.
**Regression guard.** The terminal test asserts on the captured spec, never on a shell's `env` output (dash drops `BASH_FUNC_*` from its child env): `capturedRunHost` (`exec_host_test.go`) + `envValue` (`python_exec_test.go`), as `TestTerminal_DropsTheConfiguredSecretNamesFromTheChildEnvironment` does; not parallel (`t.Setenv`). The journey builds `ws` first, `tools.NewTerminal(ws, nil)`, sets `cfg.WorkspaceDir = ws` after `allowRuleConfig`; strips the cwd line (`domain.StripCwdLine`), asserts no line names the outside dir and the `pwd` line (EvalSymlinks'd) is `ws/sub`; non-parallel, skipped on Windows (precedent: `confine_linux_test.go` `livePlanScripts`).
**Files:** internal/tools/exec_common.go, internal/tools/exec_common_test.go, internal/tools/terminal.go, internal/tools/terminal_test.go, internal/agent/dispatch_test.go, docs/manual/configuration.md, CONTEXT.md
**Read first:** internal/tools/terminal.go — Terminal.Execute; internal/tools/exec_host.go — subprocessEnvScopedPath; internal/tools/exec_common.go — subprocessEnv, isSecretEnv; internal/tools/terminal_test.go — TestTerminal_DropsTheConfiguredSecretNamesFromTheChildEnvironment;
internal/tools/exec_host_test.go — capturedRunHost; internal/agent/confine_linux_test.go — livePlanScripts; internal/security/allowmatch.go — allowedCdDestination; internal/agent/dispatch_test.go — allowRuleConfig
**Tests:**
- `TestScrubRuleAnsweredEnv` (table: each name, a `BASH_FUNC_x%%` entry, a lookalike `CDPATHX` kept).
- `TestTerminal_RuleAnsweredLineDropsEnvVectors` — `t.Setenv` all four, execute with the marker through `capturedRunHost`, assert each absent from the captured spec's `Env` (`envValue`); without the marker, present. Not parallel.
- Journey: `TestAllowRuleCdIgnoresCDPATH` in `internal/agent` — CDPATH points at an outside dir holding `sub`, the workspace `ws` holds `sub`, a rule covers `pwd`; built on `tools.NewTerminal(ws, nil)` with `cfg.WorkspaceDir = ws`, the engine rule-answers the exact line `cd sub && pwd` through the real `tools.Terminal`; after `domain.StripCwdLine`, no line names the outside dir and the printed dir (EvalSymlinks'd) is `ws/sub`. Not parallel; skipped on Windows.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestScrubRuleAnsweredEnv|TestTerminal_RuleAnswered' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAllowRuleCdIgnoresCDPATH' ./internal/agent/`
**Closes:** apogee-allow-cd-cdpath
**Commit:** `fix(tools): scrub CDPATH, BASH_ENV, ENV and exported functions from rule-answered lines`

## 3. A fully scoped PATH never searches the current directory

**What:** Recast at the regression check (2026-10-11). fixes the PATH half of `apogee-allow-rule-env-vectors`. `hostRules.ScopeInheritedEnv` already drops empty, relative (`.`) and in-workspace PATH entries via `scopePathValue`, but when every entry is dropped it emits `PATH=`, which bash treats as the current directory.
**Goal:** the env returned by `ScopeInheritedEnv` and `ScopeEnv` with a non-empty workspace root never holds an empty `PATH=` entry nor a PATH with an empty or relative element; when nothing survives scoping, the POSIX rules emit the fixed fallback `PATH=/usr/bin:/bin` and the Windows rules omit the `PATH` entry.
**Approach (assumed at the header base):** in `internal/platform/host.go`, `ScopeInheritedEnv` and `ScopeEnv` emit `PATH=/usr/bin:/bin` on the POSIX hostRules, and drop the `PATH` entry on the Windows hostRules, when `scopePathValue` leaves nothing.
**Regression guard.** widen the item to cover hostRules.ScopeEnv as well — it shares scopePathValue and emits `PATH=` when every entry scopes away (callers: gitexec hooks, internal/mcp/transport.go, diagnostics.go, toolchain_roots.go); with a test for each; Files and Acceptance follow. This supersedes the first round's "omit the PATH entry" decision (bash's compiled-in default PATH ends in `.`, so omission reintroduces a current-directory search): when scopePathValue leaves nothing, the POSIX hostRules' ScopeInheritedEnv and ScopeEnv emit the fixed fallback `PATH=/usr/bin:/bin`; Windows hostRules omit the PATH entry. The three remaining regression-3 G: lines are compatible with the /usr/bin:/bin fallback and are now folded, not overridden — (a) the fallback fires only for a non-empty workspace root with a PATH entry present: an absent PATH stays absent and an empty root leaves PATH untouched; (b) the fallback's entries pass the same in-workspace filter as every other entry (a root of `/`, `/usr` or `/bin` contains them), and if none survive the PATH entry is omitted; (c) update every doc comment describing the scoping as purely subtractive — rule: any comment on ScopeInheritedEnv, ScopeEnv or scopePathValue stating the output shape, find with `grep -rn 'ScopeInheritedEnv\|ScopeEnv\|scopePathValue' --include=*.go internal/platform/`.
**Files:** internal/platform/host.go, internal/platform/host_test.go, internal/platform/platform.go
**Read first:** internal/platform/host.go — hostRules.ScopeInheritedEnv, hostRules.ScopeEnv, scopePathValue, posixRules, windowsRules; internal/platform/platform.go — Shell.ScopeEnv;
internal/platform/host_test.go — TestScopeInheritedEnvScopesOnlyPATH, TestScopeEnv… "PATH entries inside the workspace are dropped"
**Tests:** extend `TestScopeInheritedEnvScopesOnlyPATH` (or a sibling) with `PATH=.`, `PATH=:`, `PATH=<workspace>/bin` → `PATH=/usr/bin:/bin` on posixRules, no PATH entry on windowsRules; `PATH=.:/usr/bin` → `PATH=/usr/bin`. A `TestScopeEnv…` sibling: the same cases through `ScopeEnv` with a non-empty workspace root — `PATH=.`, `PATH=:`, `PATH=<workspace>/bin` → `PATH=/usr/bin:/bin` on posixRules, no PATH entry on windowsRules; `PATH=.:/usr/bin` → `PATH=/usr/bin`. Guard (a), both methods: an env / keys lookup without PATH → no PATH entry; an empty workspace root keeps `PATH=` and `PATH=.` byte for byte. Guard (b), both methods on posixRules: a workspace root of `/` (and `/usr`) with `PATH=.` → no PATH entry.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestScopeInheritedEnv|TestScopeEnv' ./internal/platform/`
**Commit:** `fix(platform): fall back to /usr/bin:/bin when scoping leaves no PATH entry`

## 4. Interpreter suggestions cover versioned and unlisted interpreters

**What:** fixes `apogee-allow-suggest-interpreters`: `SuggestAllowRules` offers the bare name for `dash -c`, `ksh -c`, `python3.12 -c`, `nodejs -e`, `php -r`, `awk '…'`, a rule that would allow any script. Matching (`MatchAllowRules`) stays literal.
**Goal:** for any leader whose basename, exactly or with a trailing `[0-9.]+` version stripped, names an interpreter in the ratified set, `SuggestAllowRules` returns either `<leader> <literal script operand>` or nil — never the bare leader; a non-interpreter whose name ends in digits (`base64`) is suggested as before.
**Approach (assumed at the header base):** in `internal/security/allowmatch.go`, add the ratified names to `allowInterpreters` and route the lookup in `suggestRule` through one helper `isAllowInterpreter(leader string) bool` (exact basename, then version-stripped basename). Update every description of the interpreter set — rule: any doc or comment naming that set; find with `grep -rn 'allowInterpreters\|interpreter' internal/security/doc.go docs/manual/configuration.md`.
**Regression guard.** The prescribed grep finds no site; scope it to `grep -rn -i 'interpreter' internal/security/*.go docs/manual/ CONTEXT.md` — the real sites are the `SuggestAllowRules` and `allowInterpreters` doc comments in `allowmatch.go`. `internal/security/doc.go` and `docs/manual/configuration.md` leave **Files** unless a sentence is added on purpose.
**Files:** internal/security/allowmatch.go, internal/security/allowmatch_test.go
**Read first:** internal/security/allowmatch.go — suggestRule, allowInterpreters, SuggestAllowRules, allowSuggestionOperand, isLiteralRuleWord, allowCandidates; internal/security/allowmatch_test.go — TestSuggestAllowRules, allowTestWorkspace
**Tests:** rows in `TestSuggestAllowRules` (which also checks each suggestion covers its own line): `dash -c 'x'`, `ksh -c 'x'`, `python3.12 -c 'print(1)'`, `nodejs -e 'x'`, `php -r 'x'`, `awk '{print}' f` → nil; `python3.12 tool.py` → `python3.12 tool.py`; `/usr/bin/lua5.4 x.lua` → `/usr/bin/lua5.4 x.lua`; `base64 f` → `base64 f`.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSuggestAllowRules|TestMatchAllowRules|TestDocMapNamesEveryFile' ./internal/security/`
**Commit:** `fix(security): never suggest a bare versioned or unlisted interpreter as an allow rule`
