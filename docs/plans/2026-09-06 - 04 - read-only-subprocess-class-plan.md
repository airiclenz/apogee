# Plan: read-only-subprocess class — the hardened git read trio runs in every agent mode

**Goal:** `git_status`, `git_log` and `git_diff_range` are refused in Plan and gated in Ask-Before / Allow-Edits because their `Subprocess()` marker outranks their `ReadOnly()` declaration (contract §4, amended 2026-07-26/2026-08-02). Every hardening that closed "a subprocess is unbounded" for these three landed after that rule (hooks/fsmonitor off, repo-local program-key refusal, `--no-textconv --no-ext-diff`, ref guards, argv[0] fence, scrubbed child PATH), and the engine already runs the same hardened read-side git unattended in every mode for its tree-snapshot floor (`tools.RunGitQuery`). Give the trio an unexported read-only-subprocess marker and a class that takes the RO row everywhere.
**Date:** 2026-09-06
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** f5315fc2

**Sources:**
- `docs/design/confinement-execution-contract.md` §4 (class definitions, ladder table, amendments 2026-07-26 and 2026-08-02)
- `docs/adr/0012-*.md` (core invariant; amendment 2026-07-25 = the marker precedent)
- `internal/agent/resolution.go` (`classifyTool`, `planAdmits`, `resolveLadder`, `resolveLadderAuto`)
- `internal/tools/git.go` (hardening: `gitHardeningOptions`, `gitHardeningEnv`, `gitDiffHardeningArgs`, `repoLocalCommandConfig`, `runGit`)
- `internal/tools/workspace_scoped.go`, `internal/tools/network.go` (the unexported-marker template)
- `internal/agent/treesnapshot.go` (the engine's own unattended read-side git)

**Ratified design calls (owner, 2026-09-06 — do not re-ask):**
- **Scope:** exactly `git_status`, `git_log`, `git_diff_range`. `diagnostics` stays `classSubprocess`; network, MCP and every other tool untouched.
- **Ladder cell:** the RO row in every mode, Auto included — run unconfined like `read_file`; no Confine in Auto, no new Confine-with-Run fallback.
- **Submodules:** `git_status` passes `--ignore-submodules=dirty` (no child git spawned; a moved submodule commit still reports, work-tree dirt inside a submodule does not). No submodule-config scan.
- **Marker (orchestrator, conventional):** unexported `readOnlySubprocess` in `internal/tools` mirroring `workspaceScopedWriter`, exported `tools.IsReadOnlySubprocess`; `classReadOnlySubprocess` consulted after WS-write and external-effect, before the bare Subprocess marker; docs vehicle = dated amendments on contract §4 and ADR 0012.

**Regression check (2026-09-06, f5315fc2):**
- 1: guard folded — the real-repo test gains a dirt-only submodule case (the moved-commit case alone renders identically at BASE); the argv test uses the `TestGitCommit_PassesNoGpgSign` fake-git-through-`Execute` shape, not `TestRunGit_AppliesHardeningToEveryInvocation`'s direct `runGit` call.
- 2: guard folded — argv pins per tool (`git_log` ends with `--`; `git_diff_range` puts `--` only before passed paths); `internal/tools/registry_test.go` joins `**Files:**`; `diagnostics_test.go` stays untouched like `diagnostics.go`.
- 4: guard folded — the manual-drift test is `TestManualListsEveryKnownToolName`; CONTEXT.md sites named (Agent-mode Plan bullet, Confinement's by-construction clause); the manual has no per-mode rows, so (d) names a site or drops the addition; the contract amendment follows the 2026-08-30 block.
- 3: SAFE.

**Standing requirements:**
- `skills: coding-standards`
- Commit per item to `main`, no attribution trailers; `make check` once at closeout.
- Any authorized deviation lands as a dated NOTES line under its item.

**Out of scope:** `diagnostics` in Plan (any half); MCP `readOnlyHint`; network tools in Plan; a `terminal` command allowlist; a submodule-config scan; `run_tests`; version bumps.

## 1. `git_status` stops spawning a child git in submodules — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the `description` was read and left untouched — it promises no submodule work-tree dirt, as the item anticipated.
NOTES (2026-09-07): both new tests were confirmed to bite by reverting the flag against them — the dirt-only case reports `Unstaged (1):` / `M  sub` at BASE and the argv test sees `status --porcelain=v2 --branch -z`.

**What:** In `internal/tools/git.go`, `GitStatus.Execute` adds `--ignore-submodules=dirty` to its argv (after `--branch`, before `-z`). Extend the `git_status` doc block (the `git_status (2026-08-10)` paragraph in `internal/tools/doc.go` and the tool's own comment in `git.go`) with one sentence: with the default `none`, git runs `git status --porcelain=2` inside every submodule, whose own config (`.git/modules/<name>/config`) `repoLocalCommandConfig` never scans; `dirty` drops that child spawn while still reporting a submodule whose recorded commit moved. Update the tool's `description` only if it currently promises submodule work-tree dirt (read it; it does not appear to).

**Regression guard.** A submodule that is both dirty and commit-moved renders `M  sub` once at BASE and after the item alike (`addChange`, `git.go:1014-1029`, keeps only XY; git never lists a submodule's inner files), so that case cannot bite: the real-repo test MUST carry a dirt-only case (edit inside the submodule, no commit move) — at BASE the report shows `Unstaged (1):` / `M  sub` (porcelain `1 .M S.M.`), after the item the tree reports clean — and keeps the moved-commit case as the positive assertion. There is no named argv-recording harness: `TestRunGit_AppliesHardeningToEveryInvocation` calls `runGit` directly with an inline script and cannot observe `GitStatus.Execute`'s argv — use the `TestGitCommit_PassesNoGpgSign` shape (`git_test.go:1631-1663`: fake script in its own `t.TempDir()`, `withFakeGit(t, true, fakeGit)`, `Execute`, read the record); the probe's `config … --get-regexp` invocations are recorded first, so append (`>>`) and match the `status` line.

**Files:** `internal/tools/git.go`, `internal/tools/doc.go`, `internal/tools/git_test.go`

**Tests:** a fake-git test in the `TestGitCommit_PassesNoGpgSign` shape (script in its own `t.TempDir()`, `withFakeGit(t, true, fakeGit)`, `GitStatus.Execute`, record appended with `>>`) asserting the record's `status` line is exactly `status --porcelain=v2 --branch --ignore-submodules=dirty -z` after the hardening options. Add a real-repo test (the existing `TestGitStatus_ReportsStagedUnstagedAndUntracked` pattern: `t.TempDir()`, real git, skip when absent) with a nested repo added as a submodule (`git -c protocol.file.allow=always submodule add`) and two cases: dirt-only (a file edited inside the submodule, no commit move) — the report is clean, where at BASE it showed `Unstaged (1):` / `M  sub`; and a moved submodule commit — the report lists the submodule path once as a modified entry.

**Acceptance:**
```
go build ./... && go test ./internal/tools/ -run 'TestGitStatus|TestRunGit' -count=1
```

**Commit:** `fix(tools): git_status ignores submodule work-tree dirt so no child git runs under a config the program-key scan never sees`

## 2. The `readOnlySubprocess` marker, carried by the git read trio

**What:** New file `internal/tools/readonly_subprocess.go` holding an unexported interface `readOnlySubprocess` (embeds `domain.Tool`, one unexported method `readOnlySubprocess()` returning nothing) and the exported helper `IsReadOnlySubprocess(t domain.Tool) bool`, documented on the `workspace_scoped.go` model: the marker can only be minted inside `internal/tools`, and is minted ONLY for a tool whose every git invocation goes through `runGit` (so `gitHardeningOptions`, `gitHardeningEnv`, the `repoLocalCommandConfig` refusal and the argv[0] fence apply), passes `gitDiffHardeningArgs` on every diff-producing path, validates its refs with `validRef` + `looksLikeOption`, and writes nothing to the tree. `GitStatus`, `GitLog` and `GitDiffRange` gain the method plus compile-time assertions `var _ readOnlySubprocess = (*GitStatus)(nil)` (all three). They KEEP `ReadOnly()` and `Subprocess()` unchanged — the subprocess marker still drives the execution mechanics (env scoping, fence, teardown); only classification changes, in item 3. Rewrite the prose that states the old rule: the `git.go` package header lines saying the subprocess marker outranks the declaration and "none of them is offered nor run in Plan", the three per-tool `ReadOnly`/`Subprocess` method comments, `doc.go`'s `git_status`/`git_log` paragraphs, and `registry.go`'s `DefaultTools` comment — the rule finding them is "every comment in `internal/tools` stating that the trio's subprocess marker outranks its read-only declaration or that Plan does not offer them" (`grep -n 'outrank\|nor run in Plan\|offered nor' internal/tools/*.go`). Say instead: the marker classifies them RO-subproc; the ladder treats that as RO in every mode (item 3). Do not touch `diagnostics.go`'s wording — it stays true.

**Regression guard.** Test (c) as first written cannot pass: `git_diff_range` appends `--` only when paths are given and the resolved paths FOLLOW it (`git.go:845-846`); with no paths its argv ends in `base...head` (or `--stat`/`--name-only`). Pin instead: `git_log`'s argv ends with `--` (`git.go:1211-1212`); `git_diff_range`'s contains `--no-textconv --no-ext-diff` (`gitDiffHardeningArgs`, `git.go:820`) with `--` immediately before the paths when paths are passed. The prose-rewrite grep also hits `internal/tools/registry_test.go:241-243` (the trio's "subprocess marker outranks it … Plan does not offer it" comment) — that file joins `**Files:**` — and `diagnostics_test.go:80`, which stays untouched like `diagnostics.go`.

**Files:** `internal/tools/readonly_subprocess.go`, `internal/tools/git.go`, `internal/tools/doc.go`, `internal/tools/registry.go`, `internal/tools/registry_test.go`, `internal/tools/git_test.go`, `internal/tools/roster_test.go`

**Tests:** (a) `TestGit_Markers` extended: each of the trio satisfies `IsReadOnlySubprocess`, `domain.IsReadOnly`, `domain.IsSubprocessTool`, and not `IsWorkspaceScopedWriter`; `git_branch`, `git_commit` and `NewDiagnostics` do NOT satisfy `IsReadOnlySubprocess`. (b) New roster-walk test in `roster_test.go`: over `DefaultToolsWithHost(root, HostTools{...})` with every host hook set, `IsReadOnlySubprocess(t)` is true exactly for the names `git_status`, `git_log`, `git_diff_range`. (c) Argv guard: using item 1's fake-git-through-`Execute` shape, `git_log`'s argv contains `--no-textconv --no-ext-diff` and ends with `--`; `git_diff_range`'s contains `--no-textconv --no-ext-diff`, ends in `base...head` with no paths, and has `--` immediately before the resolved paths when paths are passed; `git_status`'s argv equals item 1's exact list.

**Acceptance:**
```
go build ./... && go vet ./internal/tools/ && go test ./internal/tools/ -run 'TestGit_Markers|TestDefaultTools|TestReadOnlySubprocess|TestRunGit' -count=1
```

**Commit:** `feat(tools): readOnlySubprocess marker for the hardened git read trio`

## 3. `classReadOnlySubprocess` takes the RO row in every mode

**What:** Depends on item 2. In `internal/agent/resolution.go`: add `classReadOnlySubprocess` to `toolClass` (after `classReadOnly`); `classifyTool` checks `tools.IsReadOnlySubprocess(tool)` AFTER the `IsWorkspaceScopedWriter` and `ExternalEffectTool` checks and BEFORE `IsSubprocessTool` (a tool carrying both the read-only-subprocess and a network/WS-write marker still takes the outranking class — check order stays the invariant); `planAdmits` returns true for `classReadOnly` or `classReadOnlySubprocess`; every row of `resolveLadder` and `resolveLadderAuto` that returns `resolveRun` for `classReadOnly` does the same for `classReadOnlySubprocess` — Plan, Ask-Before (default arm), Allow-Edits, Auto with confine (no `resolveConfine`, no box). Rewrite the `classifyTool`/`planAdmits` doc comments and `loop.go`'s `toolMenu` comment: the class list gains "RO-subproc = read-only by construction, subprocess by mechanism (git read trio)"; the sentences naming `git_diff_range` as the Plan-dropped example now name `diagnostics` alone (`grep -n 'git_diff_range' internal/agent/*.go`). `dispatch.go` needs no change unless the executor switches on the class — read `internal/agent/dispatch.go` for `classSubprocess`/`resolveConfine` uses and confirm a `resolveRun` verdict for the trio runs the tool's `Execute` with no box (the argv[0] fence then uses the workspace root, `security.ResolveProgram` with a nil box). The gate `reason` and `cacheKey` paths are untouched: the trio never reaches them.

**Files:** `internal/agent/resolution.go`, `internal/agent/loop.go`, `internal/agent/resolution_test.go`, `internal/agent/dispatch_test.go`, `internal/agent/planmenu_test.go`

**Tests:** `TestResolve_LadderTable` gains rows for the REAL `tools.NewGitStatus(ws)`, `NewGitLog(ws)`, `NewGitDiffRange(ws)` in all four modes × `confineToWorkspace` true/false × `fsConfineAvailable` true/false: always `resolveRun`, never Gate/Confine/Refuse; the existing `roSub` fake (`subprocTool{readOnly: true}`, no marker) keeps its refuse/gate/confine rows unchanged. `TestClassifyTool` in `dispatch_test.go`: the three real tools → `classReadOnlySubprocess`; `diagnostics (real)` and the `ro-subproc` fake stay `classSubprocess`. `planmenu_test.go`: `TestPlanToolMenuDropsTheDriftedPair` becomes "drops diagnostics and the unmarked fakes" (remove `git_diff_range` from its list) and a new test asserts the Plan menu built by `toolMenu()` over the default registry contains `git_status`, `git_log`, `git_diff_range` by exact name and still omits `diagnostics`, `git_branch`, `git_commit`, `terminal`. `TestPlanToolMenuAgreesWithTheLadder` must still pass unchanged (it walks the whole registry).

**Regression guard.** Ask-Before/Allow-Edits still gate and Plan still refuses `git_branch`, `git_commit`, `terminal`, `python_exec`, `run_tests`, `diagnostics`, `console_open`, `console_send`; the Auto `classSubprocess` Confine path and its caps-insufficient Gate fallback are byte-for-byte unchanged — the ladder-table test's existing subprocess rows are the pin.

**Acceptance:**
```
go build ./... && go vet ./internal/agent/ && go test ./internal/agent/ -run 'TestResolve|TestClassifyTool|TestPlanToolMenu|TestPlanAdmits|TestToolMenu' -count=1
```

**Commit:** `feat(agent): the git read trio classifies RO-subproc and runs in every mode`

## 4. Contract, ADR 0012, CONTEXT.md and manual record the new class

**What:** Depends on item 3. (a) `docs/design/confinement-execution-contract.md` §4: the tool-class definitions gain **RO-subproc** (= `readOnlySubprocess` marker: read-only by construction, subprocess by mechanism; obtainable only inside `internal/tools`, minted only for the git read trio); the ladder table gains a row `**RO-subproc** (git read trio) | run | run | run | run | run`; a dated `> **Amended 2026-09-06 …**` block after the 2026-08-13 one states that the 2026-07-26 "marker outranks declaration" rule stands for every other marker carrier and is superseded for this class only, lists the hardening timeline (2026-08-12 argv fence, 2026-08-13 PATH scrub, 2026-08-14/26 program-key refusal, `gitDiffHardeningArgs`) and the `RunGitQuery` tree-snapshot precedent, and notes footnote ² now applies to `diagnostics` alone. (b) `docs/adr/0012-*.md`: new `## Amendment (2026-09-06) — a subprocess that is read-only by construction takes the read-only row` after the 2026-07-26 one, same structure as the 2026-07-25 amendment (why now / what changes / tighten-only? — no: this is a LOOSEN, say so, and state why the core invariant still holds: bounded by construction, so "unsupervised" is no longer paired with "unbounded"). (c) `CONTEXT.md` **Agent mode** entry: Plan reads "read-only — including the hardened git read tools (`git_status`, `git_log`, `git_diff_range`); no writes, no other command execution"; the **Resolution** / **Confinement** entries gain the class name where they list classes (`grep -n 'RO-subproc\|3p-net\|WS-write' CONTEXT.md` finds the class lists). (d) `docs/manual/`: every sentence stating the git read tools prompt, gate, or are unavailable in Plan is corrected — the rule is "any manual sentence naming `git_status`, `git_log`, `git_diff_range` or 'the git tools' together with Plan, Ask-Before, a prompt, or confinement" (`grep -rn 'git_status\|git_log\|git_diff_range\|git tools' docs/manual/`); the per-mode description in `docs/manual/configuration.md` gains one sentence per mode row that lists what runs free. The CHANGELOG entry travels in the item's sidecar.

**Regression guard.** The manual-drift test is `TestManualListsEveryKnownToolName` (`internal/tools/manual_drift_test.go:18`) — `-run TestManualDrift` matches nothing and passes vacuously; Tests and Acceptance use the real name. (c) has no class-listing entries to extend (`grep -n 'RO-subproc\|3p-net\|WS-write' CONTEXT.md` returns nothing): add the class to the **Agent mode** Plan bullet (`CONTEXT.md:682`) and to Confinement's "bounded … by Apogee's own path-safety … and url-safety" clause (`CONTEXT.md:783-785`) as the third by-construction bound; the entries-that-list-classes wording is dropped. (d) has no per-mode rows to extend (none exist in `docs/manual/`; modes appear only in passing at `configuration.md:1575-1576`, `1641`, `commands.md:80`; `README.md:179` "read-only Plan" stays true): put the sentence under `## Auto mode's blast radius` (`configuration.md:1493`) or beside the mode-cycle sentence at `commands.md:80` — or drop (d)'s per-mode addition and keep only the correction rule. (a)'s block goes after the **2026-08-30** block (`confinement-execution-contract.md:509`, the last dated block before the table), not after the 2026-08-13 one, so §4's blocks keep date order.

**Files:** `docs/design/confinement-execution-contract.md`, `docs/adr/0012-confinement-policy.md` (resolve the exact filename with `ls docs/adr/0012*`), `CONTEXT.md`, `docs/manual/configuration.md`, `docs/manual/tools.md` (if it exists and matches the grep)

**Tests:** docs-only; `go test ./internal/tools/ -run TestManualListsEveryKnownToolName -count=1` (the manual-drift test in `internal/tools/manual_drift_test.go` pins tool names quoted in the manual) and `grep -rn 'outrank' docs/design/confinement-execution-contract.md CONTEXT.md` shows the surviving sentences scoped to "every other marker carrier".

**Acceptance:**
```
go test ./internal/tools/ -run TestManualListsEveryKnownToolName -count=1 && grep -c 'RO-subproc' docs/design/confinement-execution-contract.md CONTEXT.md
```

**Commit:** `docs(confinement): RO-subproc class — contract §4 row, ADR 0012 amendment, CONTEXT and manual`
