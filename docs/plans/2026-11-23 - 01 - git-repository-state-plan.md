# Repository state is one fact from gitexec — plan

**Goal:** gitexec answers "what is at this root" as one value — work tree with its top level, no repository, refused, incomplete, git unavailable — and a refusal on its data path is a typed error; the status host and the tree snapshot read that value in place of string predicates and their own probes. The git config-key catalogue lives in one leaf package that `security` and `gitexec` both import.
**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1f9f5369
**Closes:** git-refusal-as-text
**Regression check (2026-11-23, 1f9f5369):** 1: guard folded (a `Resolve` failure is never a `RefusedError`; one-error prose rule) · 2: recast (six kinds with `Unknown`; `Err` and `TopLevel` specified) · 3: guard folded (returns `state.Err`; the fake answers in `spawn`; one Acceptance run) · 4: guard folded (calls `t.host` directly; `RunGitQuery` retired) · 5: guard folded (pattern grep in Acceptance; prose rule; supersedes `adr-0049-amend-1114`'s placement) · 6: guard folded (external test package; grep in Acceptance; open Files). Re-check: 2: guard folded (the resolving entry point is named `StateAt`) · 3: guard folded (the cancelled-context test drives a context-aware launcher) · 4: guard folded (`Unavailable` latches `treeProbeNone`; calls `StateAt`) · 6: guard folded (names `State`/`StateAt`).

**Sources:**
- `.capcom/issues/git-refusal-as-text.md`; `docs/reviews/architecture-review-2026-11-21.html` (#3)
- ADR 0010 and `internal/gitexec/doc.go` (gitexec imports no tui), ADR 0012 amendments 0906 and 1118(c), ADR 0108 amendment 1118, ADR 0049, ADR 0056 amendment 1103 (a refusal latches the tree floor off): `capcom know show "ADR NNNN" --with-children`
- `TestGitStatusImportsNoGitRunner` (`internal/tui/gitstatus_test.go`): tui production files import neither `gitexec` nor `subprocess`

**Ratified design calls** (owner, 2026-11-23):
- **TUI seam:** `tui.GitRefusedError` stays the TUI's type; `cmd/apogee` maps gitexec's state to it with no string predicate.
- **Key catalogue:** a new leaf `internal/gitconfigkeys`, imported by `security` and `gitexec` (gitexec cannot own it: `security` would import `gitexec`, a cycle).

**Writer's calls** (2026-11-23):
- **State shape:** struct `gitexec.RepoState{Kind, TopLevel, Refusal, Err}` with a six-value kind enum, zero value first — `Unknown`, `WorkTree`, `NoRepository`, `Refused`, `Incomplete`, `Unavailable`; `gitexec.RefusedError{Sentence}` for the data path. Git absent or fenced is its own kind, `Unavailable`; a probe that produced no answer is `Unknown`.
- **Capture path:** unchanged — a refusal stays a failed result, so model-facing git tools need no new branch.
- **Rebase:** `gitactions_host.go` keeps `gitexec.IsCommandConfigRefusal`; a merge-driver refusal is per argv, not a repository state.
- **Tree snapshot on refused:** same behaviour (floor latched off), recorded as a distinct probe outcome.

**Standing requirements:**
- skills: coding-standards
- Machine rule (Raspberry Pi 5, 8 GB): never `go test -race`/`-cover` over a whole heavy package or a pattern; one package + one `-run` with `GOMEMLIMIT=2GiB`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`; never two test runs at once. Forward to every sub-agent.
- Byte-identical: `CommandConfigRefusal`, `MergeDriverRefusal`, `CommandConfigIncompleteRefusal`, `UnavailableMessage`, the data-path texts `git %s: exit %d: %s` and `timed out after`, `gitexec.CommandConfigName.String()`, the TUI's `gitRefusedLabel`, `gitRefusedAdvice`, "no git here".
- What the model receives from a git tool on a refusal does not change.
- An authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `internal/tools/git.go`'s failed-result wrap of an absent or fenced git (`gitRead`/`gitWrite`): not a config refusal.
- The `GitStatusHost` interface and `tui.GitRefusedError` themselves; `gitOutcomeError`'s flattening of other actions.
- Changing which keys are refused, neutralised or benign.

## 1. A data-path refusal is a typed error — ✅ DONE (2026-11-23)

**What:**
**Goal:** every command-config refusal (a `CommandConfigRefusal` or `MergeDriverRefusal` sentence) built in `queryDiagnosed` — so returned by gitexec's data path (`Run`, `RunTo`, `RunDiagnosed`, `Query`) — is a `gitexec.RefusedError` whose `Error()` is the refusal sentence; an incomplete check still matches `gitexec.ErrIncomplete` and never matches `RefusedError`.
**Approach (assumed at the header base):**
- New exported `RefusedError struct{ Sentence string }` in `internal/gitexec/gitexec.go`; `queryDiagnosed` returns it where it builds `errors.New(refusal)`.
- `refuseCapture` and the three sentence builders are untouched.
- `internal/gitexec/doc.go` states the outcomes of each path.
**Regression guard.** A `Resolve` failure stays as it is and never matches `RefusedError`: an absent git is still `UnavailableMessage`, a fenced git still satisfies `errors.Is(err, security.ErrExecFromWritablePath)`. Prose rule: every hit of `grep -rn -i 'ONE error\|flattened to one error\|not distinguished' --include='*.go' internal cmd` that counts a refused repository among failures no caller can tell apart is corrected (at the base: `Run`'s doc in `gitexec.go`, `doc.go`, `RunGitQuery`'s doc in `internal/tools/git.go`).
**Files:** internal/gitexec/gitexec.go, internal/gitexec/gitexec_test.go, internal/gitexec/doc.go, internal/tools/git.go
**Read first:** internal/gitexec/gitexec.go — queryDiagnosed, admitCommandConfig, incompleteError, ErrIncomplete, Resolve; internal/gitexec/gitexec_test.go — TestMergeDriver_RefusesOnlyTheMergeCapableArgv, fakeHost; internal/tools/git.go — RunGitQuery
**Tests:** new `TestDataPathRefusalIsTyped` — a repo-local command-config key and a merge driver each give `errors.As(err, &RefusedError{})` with `Sentence` equal to `CommandConfigRefusal(...)` / `MergeDriverRefusal(...)` and `err.Error()` equal to the sentence, and a planted (fenced) git and an absent git each give an error that is not a `RefusedError` (the fenced one still matching `security.ErrExecFromWritablePath`); `TestIncompleteIsNotARefusal` — an incomplete probe matches `ErrIncomplete` and not `RefusedError`; existing refusal tests (the `err.Error() == MergeDriverRefusal` assertion included) pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Refus|Incomplete|CommandConfig|MergeDriver' ./internal/gitexec/`
**Commit:** `refactor(gitexec): a data-path refusal is a typed error`

## 2. gitexec reports the repository state at a root — ✅ DONE (2026-11-23)

NOTES (2026-11-23): doc.go's "Seven entry points, one funnel" now reads "The entry points, one funnel" (no number to go stale) and gains one sentence naming State and StateAt.
NOTES (2026-11-23): the non-zero exit is the unexported typed `exitError` (text byte-identical `git %s: exit %d: %s`) — the second of the two forms the approach allows.
NOTES (2026-11-23): tests beyond the listed table — StateAt also covers a fenced git (ErrExecFromWritablePath → Unavailable) and a resolved git; the kind String() names are pinned; gitexec_test.go imports internal/realpath to spell the temp root as git does.

Depends on item 1.

**What:** Recast at the regression check (2026-11-23).
**Goal:** `(gitexec.Host).State(ctx, gitPath, root string, env []string, timeout time.Duration) RepoState` (a value receiver, like every Host method) returns exactly one of six kinds — `Unknown` (the zero value: no answer), `WorkTree` (with `TopLevel`), `NoRepository`, `Refused` (with the standing sentence in `Refusal`), `Incomplete`, `Unavailable` — with `Err` the cause for every kind but `WorkTree` and `NoRepository`; a second entry point, `(gitexec.Host).StateAt(ctx, root string, env []string, timeout time.Duration) RepoState`, resolves git itself and returns `Unavailable` when it cannot.
**Approach (assumed at the header base):**
- `State` runs one `rev-parse --is-inside-work-tree --show-toplevel` on the data path with the caller's env (the config-check memo key includes env, so the caller's later runs are judged by the same check) and classifies: `RefusedError` → `Refused`; `ErrIncomplete` → `Incomplete`; a non-zero git exit → `NoRepository`; exit 0 with exactly two lines, `true` + a non-empty path → `WorkTree`; exit 0 with anything else (`false` + a top level under a foreign `core.worktree`, empty output) → `NoRepository`; a spawn, admit, confinement or context error → `Unknown`.
- `State` reads a non-zero exit from the result, never from error text: a shared unexported helper under `queryDiagnosed`, or an unexported typed exit error whose `Error()` is byte-identical `git %s: exit %d: %s`.
- `StateAt` uses `Resolve`; a `Resolve` failure (absent, or `security.ErrExecFromWritablePath`) → `Unavailable` with `Err` set.
- `RepoStateKind` has a `String()` for test output only; no sentence is derived from a kind.
**Regression guard.** `RepoState` is fully specified so items 3, 4 and 6 need no guess — the kind enum has SIX values, zero value first: `Unknown` (the probe produced no answer: context ended, confinement or spawn failure; `Err` holds the cause), `WorkTree` (`TopLevel` set, `Err` nil), `NoRepository` (`Err` nil), `Refused` (`Refusal` = the standing sentence, `Err` = the `RefusedError`), `Incomplete` (`Err` = the data-path error that matches `ErrIncomplete`, whose text is the incomplete sentence it carries today, so a caller can return `state.Err` unchanged), `Unavailable` (`Err` = the `Resolve` failure). `Err` is non-nil for exactly Unknown, Refused, Incomplete and Unavailable. `TopLevel` is the spelling `treeSnapshotter.runProbe` latches at the header base (same trimming and slash conversion — read it there and reproduce it). `State` launches git the way the data path does at the base, so a caller's scripted fake of the two-flag `rev-parse` argv keeps working. Item 2 also corrects `internal/gitexec/doc.go`'s entry-point count sentence by the rule "no doc line states a stale number of entry points" (`grep -n -i 'entry points' internal/gitexec/doc.go`). The resolving entry point is named — `(gitexec.Host).StateAt(ctx, root string, env []string, timeout time.Duration) RepoState` — beside `State` (which takes the pre-resolved git path); items 3, 4 and 6 use these two names.
**Files:** internal/gitexec/gitexec.go, internal/gitexec/gitexec_test.go, internal/gitexec/doc.go
**Read first:** internal/gitexec/gitexec.go — queryDiagnosed, admitCommandConfig, probeCommandConfig, Resolve, incompleteError, ErrIncomplete, runSpec; internal/agent/treesnapshot.go — treeSnapshotter.runProbe, treeSnapshotter.active; cmd/apogee/gitstatus_host.go — insideWorkTree, gitOutcomeError;
     internal/gitexec/gitexec_test.go — fakeHost, cleanRepository, outsideGit, realGit, runRealGit, appendToConfig, TestHost_RefusesAnIncompleteCommandConfigProbe, TestHost_RunReportsAnAbsentGitWithoutSpawning; internal/subprocess/subprocess.go — run (a done ctx is a Go error, never a result);
     internal/agent/treesnapshot_test.go — scriptedGit, probeScriptedGit, treeProbeCommand; internal/gitexec/doc.go — "Seven entry points" paragraph
**Tests:** `TestStateKinds` table over real temp dirs: a repo root and a subdirectory (`WorkTree`, `TopLevel` the root's real path, `Err` nil), a plain dir (`NoRepository`, `Err` nil), a bare repo (`NoRepository`), a scripted exit 0 printing `false` + a top level (`fakeHost`; `NoRepository`), a repo with a refused command-config key (`Refused`, `Refusal == CommandConfigRefusal(...)`, `Err` a `RefusedError`), a scripted incomplete probe (`Incomplete`, `errors.Is(state.Err, ErrIncomplete)` and its text the incomplete sentence), a cancelled context (`Kind == Unknown`, `Err` set); `StateAt` with git unresolvable gives `Unavailable` with `Err` set; the zero `RepoState` is `Unknown`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'State|Refus|Incomplete' ./internal/gitexec/`
**Commit:** `feat(gitexec): report the repository state at a root as one value`

## 3. The status host reads the repository state — ✅ DONE (2026-11-23)

NOTES (2026-11-23): the `insideWorkTree` helper keeps its name and signature; its body is the one `State` call and the kind mapping, so `Status` itself is unchanged apart from its doc sentence ("Inside" is decided by the RepoState kind).
NOTES (2026-11-23): `TestGitStatusHostCancelledProbeIsAnError` wraps the fake's `Spawn` with a context check on the host under test rather than adding a context-aware mode to `gitStatusFake`.

Depends on item 2.

**What:**
**Goal:** `cmd/apogee/gitstatus_host.go` contains no call to `gitexec.IsCommandConfigRefusal` or `gitexec.IsIncompleteRefusal`; `gitStatusHost.Status` decides "inside a work tree", "refused" and "ask again" from `RepoState.Kind`; what the TUI shows for each case is unchanged.
**Approach (assumed at the header base):**
- `insideWorkTree` is replaced by one `State` call with the host's pre-resolved git path, `baseEnv()` and `gitStatusBackstop`. Mapping: `WorkTree` → inside; `NoRepository`, `Unavailable` → `(false, nil)`; `Refused` → `tui.GitRefusedError{Sentence: state.Refusal}`; `Incomplete` → `state.Err` (it matches `gitexec.ErrIncomplete` and carries the text returned today, so the existing `IsIncompleteRefusal(err.Error())` test assertion still holds); `Unknown` → `state.Err`.
- Consumer of the changed probe: `gitStatusFake` (`cmd/apogee/gitstatus_host_test.go`) — its `spawn` answers a non-`--git-path` `rev-parse` with `Stdout: "true\n"+spec.Dir+"\n"` (the `--git-path` case stays first); the `spawnTo` `rev-parse` case goes.
- `internal/tui/gitstatus_test.go`: `testRefusal` is built with `gitexec.CommandConfigRefusal(...)` (test files may import gitexec), not typed by hand.
- `gitactions_host.go` is untouched (see Writer's calls).
**Regression guard.** Per item 2's `RepoState`: `Incomplete` → return `state.Err`, `Unknown` → return `state.Err` — a cancelled or confinement-failed poll stays an error as it is today, never `(false, nil)`, which would clear the footer's snapshot. `State` runs on the data path, which launches through the fake's `spawn` and reads `res.Stdout`; left as it is the fake answers exit 128 there and every fake-driven test reads `NoRepository`. The host calls `State` (pre-resolved git path), never `StateAt`.
**Files:** cmd/apogee/gitstatus_host.go, cmd/apogee/gitstatus_host_test.go, internal/tui/gitstatus_test.go
**Read first:** cmd/apogee/gitstatus_host.go — gitStatusHost.Status, insideWorkTree, gitOutcomeError; cmd/apogee/gitstatus_host_test.go — gitStatusFake.spawn, gitStatusFake.spawnTo, TestGitStatusHostIncompleteProbeIsAnError; internal/agent/treesnapshot_test.go — probeScriptedGit; internal/gitexec/gitexec.go — queryDiagnosed; cmd/apogee/e2e_gitstatus_test.go — TestE2EGitActionsRefusedRepository
**Tests:** existing status-host tests for refused (asserting `tui.GitRefusedError` and its sentence), incomplete and no-repository pass with only the fake changed; new `TestGitStatusHostCancelledProbeIsAnError` — a cancelled context gives an error, not `inside=false, nil`; it drives a context-aware launcher, following `probeScriptedGit` in `internal/agent/treesnapshot_test.go`, because `gitStatusFake.spawn` ignores its context; `TestE2EGitActionsRefusedRepository` (`e2e_gitstatus_test.go`) passes unchanged (the exact refusal sentence and "git refused" reach the frame); new `TestGitStatusHostUsesNoRefusalPredicate` greps the host file for the two predicate names; `TestGitStatusImportsNoGitRunner` and `TestGitStatusRefusalNotesOncePerSentence` pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'GitStatus|E2EGitActionsRefusedRepository' ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatus' ./internal/tui/`
**Commit:** `refactor(gitstatus): the status host reads gitexec's repository state`

## 4. The tree snapshot reads the repository state

Depends on item 2.

**What:**
**Goal:** `treeSnapshotter` (`internal/agent/treesnapshot.go`) learns work tree, top level, no repository, refused and incomplete from `gitexec`'s `RepoState`; a refusal is recorded as its own probe outcome, distinguishable in a test from "no repository", and still latches the floor off; an incomplete check is still retried.
**Approach (assumed at the header base):**
- `runProbe` calls `t.host.StateAt` directly (the snapshotter already holds the `gitexec.Host`, and `statusListing` already uses `t.host.RunTo`) in place of `tools.RunGitQuery` + `errors.Is(err, gitexec.ErrIncomplete)`.
- New `treeProbeRefused` beside `treeProbeNone`; every reader of `t.probe` treats it as `treeProbeNone` does (grep `treeProbeNone` and cover each site). `Incomplete`, and `Unknown` with the run context ended, stay transient; `Unknown` otherwise is treated as a non-incomplete probe error is at the base.
- `treeProbeTimeout` and the nil env are unchanged.
**Regression guard.** `runProbe` is `tools.RunGitQuery`'s only production caller: once it calls `t.host` directly, `RunGitQuery` and its three `TestRunGitQuery_*` tests are deleted, and every hit of `grep -rn 'RunGitQuery' --include='*.go' internal` is corrected — no comment keeps naming it as the floor's entry (at the base: `internal/tools/git.go`, `git_test.go`, `doc.go`, `readonly_subprocess.go`, `internal/agent/treesnapshot.go`). `Unavailable` latches `treeProbeNone`, exactly as a `Resolve` failure does at the header base (`TestTreeSnapshot_PlantedGitTurnsTheFloorOff` passes unchanged and is named in Tests).
**Files:** internal/agent/treesnapshot.go, internal/agent/treesnapshot_test.go, internal/tools/git.go, internal/tools/git_test.go, internal/tools/doc.go, internal/tools/readonly_subprocess.go
**Read first:** internal/agent/treesnapshot.go — treeSnapshotter.runProbe, treeSnapshotter.active, treeProbeState, treeProbeTransientLimit; internal/agent/treesnapshot_test.go — scriptedGit, TestTreeSnapshot_CancelledProbeIsRetried, TestTreeSnapshot_PlantedGitTurnsTheFloorOff; internal/tools/git.go — RunGitQuery
**Tests:** `TestTreeSnapshot_CommandConfigRefusalLatchesOff` additionally asserts the probe outcome is `treeProbeRefused`; new `TestTreeSnapshot_NoRepositoryIsNotRefused`; `TestTreeSnapshot_PlantedGitTurnsTheFloorOff` passes unchanged (`Unavailable` latches `treeProbeNone`); `TestTreeSnapshot_IncompleteConfigCheckIsRetried` passes with only its scripted argv/result adjusted; `treeProbeCommand` follows the argv.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestTreeSnapshot' ./internal/agent/ && go vet ./internal/tools/ && ! grep -rn 'RunGitQuery' --include='*.go' internal`
**Commit:** `refactor(agent): the tree snapshot reads gitexec's repository state`

## 5. The git config-key catalogue is one leaf package

Depends on item 2 (same files in gitexec).

**What:**
**Goal:** `internal/gitconfigkeys` (standard library only) is the single definition of the program-naming key source, the write-view suffix, the merge-driver key pattern and the benign values; `security` and `gitexec` import it and neither restates a pattern; every refusal, neutralisation and shell-write verdict is unchanged.
**Approach (assumed at the header base):**
- Move from `internal/security/shellwrites.go`: `GitCommandConfigNameSource`, the suffix composing `GitCommandConfigName`, `gitConfigValueIsBenign` and `gitBooleans`. Move from `internal/gitexec/gitexec.go`: the `merge\..*\.driver` pattern behind `mergeDriverName`.
- `security.GitCommandConfigName` and `gitexec.CommandConfigName` stay exported, compiled from the leaf; `gitexec.CommandConfigName.String()` is byte-identical (tests hand it to git). `security.GitCommandConfigNameSource` is removed; its callers name the leaf.
- `neutralisableMergeDriver` stays in gitexec (which keys can be neutralised is the runner's rule), reading the leaf's pattern.
- ADR 0049 gains a dated amendment child Note (`adr-0049-amend-MMDD`, the day the item runs), named on the Status line and in the reading-order list; `internal/gitexec/doc.go` and `internal/security/doc.go` name the leaf.
**Regression guard.** The amendment supersedes `adr-0049-amend-1114`'s placement of the probe source in `security` and says so. Prose rule: every hit of `grep -rn 'GitCommandConfigNameSource\|lives there\|lives here because' --include='*.go' internal/gitexec internal/security internal/tools` names `internal/gitconfigkeys` after the item (at the base: `CommandConfigName`'s doc in `gitexec.go`, the source's doc in `shellwrites.go`, `internal/gitexec/doc.go`). "Neither restates a pattern" is checked by the Acceptance grep, which finds lines in both files at the base.
**Files:** internal/gitconfigkeys/doc.go, internal/gitconfigkeys/keys.go, internal/gitconfigkeys/keys_test.go, internal/security/shellwrites.go, internal/security/doc.go, internal/gitexec/gitexec.go, internal/gitexec/doc.go, .capcom/knowledge/decisions/adr-0049.md, .capcom/knowledge/decisions/adr-0049/adr-0049-amend-MMDD.md
**Read first:** internal/security/shellwrites.go — GitCommandConfigNameSource, GitCommandConfigName, gitConfigValueIsBenign, gitBooleans; internal/gitexec/gitexec.go — CommandConfigName, mergeDriverName, neutralisableMergeDriver; .capcom/knowledge/decisions/adr-0049.md — Status line, reading-order list
**Tests:** `keys_test.go`: `TestSourceIsUnchanged` pins the source string and the write-view suffix against hand-spelled literals copied from the header base; the merge-driver pattern matches `merge.x.driver` and not `merge.x.name`; benign-value table; the leaf imports nothing internal. Existing `internal/security` shell-write git tests, `internal/gitexec` refusal and neutralisation tests, and `internal/tools` `TestGit_RefusesRepoLocalCommandConfig`, `TestGit_RefusesRepoLocalFilterDriver`, `TestGitCommit_RefusesRepoSuppliedFilterDriver` pass unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/gitconfigkeys/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Git' ./internal/security/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'CommandConfig|MergeDriver|Refus|Neutralis' ./internal/gitexec/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGit_Refuses|TestGitCommit_Refuses' ./internal/tools/ && ! grep -n 'core\\\.hookspath\|merge\\\.\.\*\\\.driver' internal/gitexec/gitexec.go internal/security/shellwrites.go && capcom check`
**Commit:** `refactor(gitconfigkeys): one catalogue of program-naming git config keys`

## 6. Retire the predicates nothing reads

Depends on items 3, 4 and 5.

**What:**
**Goal:** `gitexec` exports a text predicate over refusal sentences only where a production caller uses it: `IsIncompleteRefusal` is unexported or deleted — no file outside `internal/gitexec` names it; `IsCommandConfigRefusal` stays, with its doc naming `Rebase` as its one caller and `RepoState` (from `State` or `StateAt`) as the way to ask about a root.
**Approach (assumed at the header base):**
- A test that used the removed export asserts through `errors.Is(err, gitexec.ErrIncomplete)` or `RepoState.Kind`; `cmd/apogee/gitstatus_host_test.go`'s `IsIncompleteRefusal(err.Error())` assertion becomes `errors.Is(err, gitexec.ErrIncomplete)` (item 3 returns `state.Err`, which matches it).
- Prose rule: every comment or doc line that says a caller tells a refusal from other failures "by its text" or "by prefix" is corrected — `grep -rn -i 'by its text\|by prefix\|IsCommandConfigRefusal\|IsIncompleteRefusal' --include='*.go' internal cmd`.
**Regression guard.** `internal/gitexec/gitexec_test.go` is `package gitexec_test`, so it cannot call an unexported predicate: on unexport, `TestIsIncompleteRefusal_TellsAnUnfinishedProbeFromARefusedRepository` moves to a `package gitexec` file (`internal/gitexec/refusal_internal_test.go`); on delete it is deleted. Either way the incomplete-versus-refused distinction stays pinned by item 1's `TestIncompleteIsNotARefusal`.
**Files:** internal/gitexec/gitexec.go, internal/gitexec/gitexec_test.go, internal/gitexec/refusal_internal_test.go (on unexport), internal/gitexec/doc.go, cmd/apogee/gitstatus_host_test.go, and every file the prose grep hits (at the base, outside gitexec: cmd/apogee/gitstatus_host.go, cmd/apogee/gitactions_host.go)
**Read first:** internal/gitexec/gitexec.go — IsIncompleteRefusal, IsCommandConfigRefusal, incompleteRefusalLead, queryDiagnosed; internal/gitexec/gitexec_test.go — TestIsIncompleteRefusal_TellsAnUnfinishedProbeFromARefusedRepository, isIncompleteRefusal (test helper, same name); cmd/apogee/gitstatus_host_test.go — TestGitStatusHostIncompleteProbeIsAnError; cmd/apogee/gitactions_host.go — Rebase
**Tests:** `IsCommandConfigRefusal`'s table stays in `gitexec_test.go`; an unexported incomplete predicate keeps its table in the internal test file; all item 1–5 acceptance tests still pass.
**Acceptance:** `go build ./... && go vet ./internal/gitexec/ ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Refus|Incomplete|State' ./internal/gitexec/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'GitStatusHost|GitActions' ./cmd/apogee/ && ! grep -rn 'IsIncompleteRefusal' --include='*.go' cmd internal | grep -v '^internal/gitexec/'`
**Commit:** `refactor(gitexec): retire the refusal predicates nothing reads`
