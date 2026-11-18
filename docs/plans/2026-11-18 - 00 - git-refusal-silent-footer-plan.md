# Repo-local merge drivers neutralised; refused repositories surfaced in the footer plan

**Goal:** A repository whose own config names a merge driver keeps every git tool, the footer's git segment and its actions: apogee overrides each repo-local `merge.<x>.driver` on every git call so no driver can run, and refuses only the argv whose job is a content merge. A repository refused for any other command-valued key shows a `git refused` marker in the footer and posts the refusal sentence once, instead of silently dropping the segment.
**Date:** 2026-11-18
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7c8f5aa1
**Closes:** git-refusal-silent-footer

**Regression check (2026-11-18, 7c8f5aa1):**
- 1: guard folded (unwind flags exempt, per the writer's decision; `=`-subsection driver names; remerge-diff fixture; widened prose-rule grep)
- 3: recast (a refused rebase runs no conflict listing and no `rebase --abort`; `cmd/apogee/gitactions_host.go` joins Files)
- 4: guard folded (comment rule; only a not-inside or good landing clears the refusal)
- 5: guard folded (e2e refused-repository test, layout.md rule, `footerToneError`); supersedes ADR 0108 Decision 3 via item 6's `adr-0108-amend-1118`
- 6: recast (amend ADR 0012, ADR 0108, ADR 0056 Decision 4 and adr-0049-amend-1103 instead of a new ADR 0049 amendment; prose-rule grep widened to `docs/design` and `layout.md`)
- 1 (re-check): Goal states `IsCommandConfigRefusal` is true for both refusal sentences, per the writer's decision
- 3 (re-check): guard folded (the seam: `Rebase` judges `res.CombinedOutput` before `gitOutcomeError` flattens it; `gitactions_host.go` abort-comment rule; `gitstatus_host.go` comment clause dropped)
- 6 (re-check): guard folded (`adr-0056.md`/`adr-0049.md` Status lines join Files; `merge.<x>.driver` stays in the probe source, stated against amend-1114's split rule; `internal/tools` and `internal/config` manual-drift Acceptance)

**Sources:**
- `.capcom/issues/git-refusal-silent-footer.md`
- ADR 0049 and its amendments (`capcom know show "ADR 0049" --with-children`); ADR 0108 (footer git actions)
- The refusal posture as recorded: ADR 0012 `adr-0012-amend-0906` (`capcom know show "ADR 0012" --with-children`), ADR 0056 Decision 4, ADR 0108 Decision 3
- `layout.md` "The footer's upstream slot" → "The git state."; `docs/manual/building.md` §Setting up a clone (the capcom-log driver)
- capcom repo `docs/site/concepts/branches-and-merging.md`: capcom tells users to register its driver with a plain (repo-local) `git config`

**Verified at write time (git 2.47.3, planted driver writing a marker):** `status`, `log -p`, `show`, `diff`, `blame`, `grep`, `cat-file`, `rev-parse` never run a merge driver; `log`/`show --remerge-diff`, `--diff-merges=remerge`, `-m` under `log.diffMerges=remerge`, and `merge-tree` DO. With `-c merge.<x>.driver=` (empty value) none runs anything: git prints `cannot run :` and read verbs still exit 0. Of the argv apogee runs, only the footer action's `rebase @{u}` can invoke a merge driver (`pull` is `merge --ff-only`; `checkout` has no `-m`; the model's git tools run no merge verb).

**Ratified design calls:**
- **Posture:** neutralise every repo-local `merge.<x>.driver` with `-c merge.<x>.driver=` on every gitexec call; refuse merge-capable argv with a sentence naming the key; every other command-valued key keeps the whole-repository refusal (owner, 2026-11-18, option (b) hybrid).
- **Marker:** the footer git segment reads `git refused` in the footer's error tone; the first read that sees a refusal (or a changed one) posts one routine note with the full sentence; `/git` answers with that sentence instead of "no git here" (owner, 2026-11-18).
- **Security rests on neutralisation, not on the verb list:** the merge-capable argv list exists only so a merge fails with a clear sentence instead of git's `cannot run`; a verb missing from it costs a confusing error, never an execution (writer, 2026-11-18).
- **Unwind flags:** `--abort`/`--quit` are never merge-capable (writer, 2026-11-18, regression check).

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi 5: never `go test -race`/`-cover` over a whole package or pattern; one package, narrowed `-run`: `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; never two test runs at once.
- No AI attribution in commits.

**Out of scope:**
- capcom's own setup text (prints a repo-local `git config`): separate repo.
- Neutralising other key classes (filters, `credential.helper`, pagers, `mergetool.<x>.cmd`, `difftool.<x>.cmd`).
- treesnapshot latching off silently in a repo refused for a non-merge-driver key.
- VERSION or release changes.

## 1. gitexec: neutralise repo-local merge drivers, refuse only merge-capable argv — ✅ DONE (2026-11-18)

NOTES (2026-11-18): `internal/tools/git_test.go`'s `TestGit_RefusesRepoLocalCommandConfig/merge.foo.driver` row now fails by design (git_status succeeds in a merge-driver repo); item 2 owns removing that row, and `internal/tools/readonly_subprocess.go`'s "outright refusal" comment is left for item 2, which lists that file.
NOTES (2026-11-18): the prose-rule grep still hits `CommandConfigRefusal`'s own sentence ("refused for every git tool"), kept verbatim because the Goal pins that sentence unchanged; `subcommand` now delegates to a new `subcommandAt` so the merge-capable judgment reads the argv after the verb.
NOTES (2026-11-18): the `=`-subsection driver takes the refusing route (`CommandConfigRefusal`), not the `GIT_CONFIG_COUNT` route; an empty-subsection `merge..driver` is neutralised (git 2.47.3 accepts `-c merge..driver=`).

**What:** fix: a repo-local `merge.<x>.driver` refuses every git call, though only a content merge can run it (issue `git-refusal-silent-footer`, part 2).
**Regression guard.** `--abort` and `--quit` on rebase, cherry-pick, revert, am and merge are NOT merge-capable (they unwind and merge nothing); add test rows proving `rebase --abort` is not refused in a merge-driver repo. A driver whose subsection holds `=` (`merge.a=b.driver`) cannot be neutralised by `-c` (git splits at the first `=`; checked on git 2.47.3: key `merge.a`, driver live): keep such a name in the refusing class (`CommandConfigRefusal`), or neutralise every name through `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n`, which does stop it; add an `=` test row. The remerge-diff fixture commits `.gitattributes` `<f> merge=Capcom-Log`, both branches edit the same line of `<f>`, and the merge commit is made before the driver is planted (or the marker removed after setup), so a missing `-c` pair fails the test.
**Goal:** in a repository whose own config carries `merge.<x>.driver` and no other command-valued key, every gitexec call runs with `-c merge.<x>.driver=` for each such key (subsection spelled exactly as git's listing spells it) and is not refused, except a merge-capable argv, which is refused with `MergeDriverRefusal`. A repository carrying any other command-valued key is refused exactly as before, sentence unchanged. `IsCommandConfigRefusal(text)` reports true for both a `CommandConfigRefusal` sentence and a `MergeDriverRefusal` sentence, and false for an incomplete refusal and for git's own errors.
**Approach (assumed at the header base):** `probeCommandConfig` returns the probe's names instead of a rendered string; `refuseCapture` and `queryDiagnosed` take `args` and split the names: a name matching `merge\..*\.driver` is neutralised, any other name refuses as today. The neutralising `-c` pairs go into `runSpec` after `hardeningOptions`. Merge-capable argv, judged on `subcommand(args)`: `rebase`, `cherry-pick`, `revert`, `am`, `merge-tree`; `merge` and `pull` unless argv carries `--ff-only`; `stash` when its next non-option word is `apply`, `pop` or `branch`; `checkout`/`switch` with `-m`/`--merge`; `apply` with `-3`/`--3way`; never an argv carrying `--abort` or `--quit`. `MergeDriverRefusal(verb, names)` renders `git refused: <verb> would run the merge driver this repository's own config names (<keys>). Repo-local merge drivers are neutralised on every other git call; a merge driver in the operator's global git config still runs.` It shares `maxNamedCommandKeys` and the `git refused:` prefix, and is not an incomplete refusal. The `security.GitCommandConfigNameSource` string is unchanged; the shell write view still judges writing the key. Prose guard: every comment stating that any command-valued key refuses every git call (`rg -n -i "no git call at all|outright refusal|every repo-local key|costs a repository its git tools|every git tool|REFUSES the call" internal cmd`).
**Files:** internal/gitexec/gitexec.go, internal/gitexec/doc.go, internal/gitexec/gitexec_test.go, internal/agent/treesnapshot.go, internal/tools/git.go, internal/security/shellwrites.go
**Read first:** internal/gitexec/gitexec.go — probeCommandConfig, refuseCapture, queryDiagnosed, runSpec, subcommand, CommandConfigRefusal; internal/gitexec/gitexec_test.go — TestHost_CaptureRefusesARepositoryWhoseConfigNamesAProgram; internal/security/shellwrites.go — GitCommandConfigNameSource

**Tests:**
- Real git, planted driver script writing a marker file, mixed-case subsection (`[merge "Capcom-Log"]`): `status`, `log -p`, `show --remerge-diff <merge>`, `log --diff-merges=remerge -p` succeed and the marker never appears; the recorded argv carries `-c merge.Capcom-Log.driver=`. The merge commit comes from the guard's fixture (committed `.gitattributes`, conflicting sides, merged before the driver is planted).
- `rebase`, `merge` (no `--ff-only`), `merge-tree`, `stash pop`, `-c x=y rebase` return `MergeDriverRefusal` naming the key; `merge --ff-only` is not refused. Marker absent throughout.
- `rebase --abort` (and `--quit`) in a merge-driver repo is not refused.
- A driver named `merge.a=b.driver`: refused with `CommandConfigRefusal`, or (env route) neutralised so the marker never appears.
- Driver plus a `filter.<x>.clean`: refused with `CommandConfigRefusal` naming both keys (as today).
- `IsCommandConfigRefusal` is true for both refusal sentences, false for an incomplete refusal and for git's own errors.

**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestCapture|TestHost_|TestCommandConfig|TestMergeDriver|TestIsCommandConfigRefusal' ./internal/gitexec/`

**Commit:** `fix(gitexec): neutralise repo-local merge drivers instead of refusing the repository`

## 2. Model git tools work in a merge-driver repository — ✅ DONE (2026-11-18)

NOTES (2026-11-18): the new test arms its fixture with a committed `.gitattributes` (`* merge=Capcom-Log`), a mixed-case `merge.Capcom-Log.driver` writing a marker, and a second commit so `git_show HEAD~1` and `git_diff_range HEAD~1..HEAD` have a range; each tool runs in its own fresh repository.

**What:** test-and-comment item for item 1's effect on `internal/tools`: a repo-local merge driver no longer refuses `git_status`, `git_log`, `git_show`, `git_diff_range`, `git_commit`, `git_branch`.
**Goal:** `internal/tools` tests prove each git tool succeeds in a repo with a planted, marker-writing `merge.<x>.driver` and the marker never appears; the `merge.foo.driver` row leaves `TestGit_RefusesRepoLocalCommandConfig`'s refused table and every other row still refuses.
**Approach (assumed at the header base):** move the `merge.foo.driver` row out of `TestGit_RefusesRepoLocalCommandConfig` into a new `TestGit_RepoLocalMergeDriverIsNeutralised` beside `TestGit_RepoLocalFsmonitorTrueIsNotRefused`. Update comments in `internal/tools/doc.go` and `internal/tools/readonly_subprocess.go` under item 1's prose rule.
**Depends on:** item 1.
**Files:** internal/tools/git_test.go, internal/tools/doc.go, internal/tools/readonly_subprocess.go
**Read first:** internal/tools/git_test.go — TestGit_RefusesRepoLocalCommandConfig, TestGit_RepoLocalFsmonitorTrueIsNotRefused, TestGit_RepoLocalFsmonitorHookNeverRuns, writeMarkerScript, requireNotRan, gitRepo; internal/tools/readonly_subprocess.go — package comment; internal/tools/doc.go — package comment

**Tests:** as Goal.

**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGit_RefusesRepoLocal|TestGit_RepoLocal' ./internal/tools/`

**Commit:** `test(tools): git tools keep working beside a repo-local merge driver`

## 3. Footer hosts in a merge-driver repository

**What:** Recast at the regression check (2026-11-18). test item for item 1's effect on `cmd/apogee`: the footer status read and fetch, pull, push and switch work in a merge-driver repo; the rebase action is refused with `MergeDriverRefusal`.
**Regression guard.** the footer rebase action returns gitexec's MergeDriverRefusal unchanged and runs no conflict listing (`diff --diff-filter=U`) and no `rebase --abort` after a refused rebase; add cmd/apogee/gitactions_host.go to item 3's Files if the host needs a change to achieve that, and the test asserts neither follow-up verb ran. (It does: `gitActionsHost.Rebase` calls `gitActionRun.abortRebase` on every failed `rebase`, which runs `conflictedPaths` and `rebase --abort`.) The seam: `Rebase` judges the rebase call's own `res.CombinedOutput` before `gitOutcomeError` flattens it to `git rebase: exit 1: <sentence>` (`act` returns the result, or a typed refusal error), using item 1's `IsCommandConfigRefusal`, which is true for `MergeDriverRefusal`. Every comment in `gitactions_host.go` saying a failed rebase is aborted names the refused-rebase exception (`rg -n -i abort cmd/apogee/gitactions_host.go`; today `Rebase`'s doc comment and `abortRebase`'s).
**Goal:** `cmd/apogee` tests prove, against real git with a marker-writing repo-local `merge.<x>.driver`: `gitStatusHost.Status` reports inside with counts; `gitActionsHost` pull (fast-forward) and switch succeed; rebase returns an error carrying `MergeDriverRefusal` naming the key and runs no `rebase` subprocess, no `diff --diff-filter=U` and no `rebase --abort`; the marker never appears.
**Approach (assumed at the header base):** extend `gitstatus_host_test.go` and `gitactions_host_test.go` with fixtures modelled on `TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch` and `TestGitActionsHostRebaseRefusesAnSshSigningKeyCommand`, using `recordingStatusHost` to assert which verbs ran.
**Depends on:** item 1.
**Files:** cmd/apogee/gitstatus_host_test.go, cmd/apogee/gitactions_host_test.go, cmd/apogee/gitactions_host.go
**Read first:** cmd/apogee/gitactions_host.go — gitActionsHost.Rebase, gitActionRun.act, gitActionRun.abortRebase, gitActionRun.conflictedPaths; cmd/apogee/gitstatus_host.go — gitOutcomeError;
  internal/gitexec/gitexec.go — refuseCapture, IsIncompleteRefusal; cmd/apogee/gitstatus_host_test.go — recordingStatusHost.ran, TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch;
  cmd/apogee/gitactions_host_test.go — TestGitActionsHostRebaseRefusesAnSshSigningKeyCommand, divergeFromUpstream

**Tests:** as Goal.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatusHost|TestGitActionsHost' ./cmd/apogee/`

**Commit:** `test(git): footer status and actions beside a repo-local merge driver`

## 4. GitStatusHost reports a refused repository distinctly

**What:** fix: `gitStatusHost.Status` reads a refused repository as "not a work tree", so the TUI cannot tell a refusal from no repository (issue `git-refusal-silent-footer`, part 1).
**Regression guard.** Comment rule, not a closed list: every comment calling a refused repository "not inside" or saying every failed read keeps the snapshot is corrected (`rg -n 'refus|failed read keeps' cmd/apogee/gitstatus_host.go internal/tui/gitstatus.go internal/tui/doc.go`; today `insideWorkTree`, `foldGitStatus`, doc.go:658). Only a not-inside or good landing clears the recorded refusal; an errored landing (an incomplete-refusal probe timing out on a slow box) keeps it, as it keeps the snapshot.
**Goal:** `internal/tui` defines `GitRefusedError{Sentence string}`; `GitStatusHost.Status` returns `inside=false` and a `GitRefusedError` carrying gitexec's refusal sentence when a refusal stopped the read; a non-repository still returns `inside=false, nil`. `foldGitStatus` clears the snapshot of the read's root, records the sentence in that root's slot (`gitState.refusal`, `watchState.gitRefusal`) and backs off by `gitStatusBackoff`; a not-inside or good landing clears the recorded sentence. Other errors keep the last snapshot and the recorded sentence.
**Approach (assumed at the header base):** `insideWorkTree` checks `gitexec.IsCommandConfigRefusal(res.CombinedOutput)` beside its `IsIncompleteRefusal` check. Plain string fields only (ADR 0011). Update the doc comments under the guard's comment rule, and `TestGitStatusHostRefusedRepositoryRunsNoStatusOrFetch` to expect the error.
**Depends on:** item 1.
**Files:** cmd/apogee/gitstatus_host.go, cmd/apogee/gitstatus_host_test.go, internal/tui/gitstatus.go, internal/tui/watch.go, internal/tui/gitstatus_test.go, internal/tui/doc.go
**Read first:** cmd/apogee/gitstatus_host.go — gitStatusHost.Status, insideWorkTree; internal/tui/gitstatus.go — GitStatusHost, foldGitStatus, keepGitSnapshot, gitState; internal/tui/watch.go — watchState.gitSnap/hasGitSnap; internal/tui/gitstatus_test.go — fakeGitHost

**Tests:**
- Host: a repo with a planted `core.sshCommand` returns `GitRefusedError` whose `Sentence` equals `gitexec.CommandConfigRefusal([]string{"core.sshcommand"})`; a non-repository returns nil error.
- TUI (`fakeGitHost`): a refused landing clears `hasSnap` and records the sentence; an errored landing keeps it; a later good landing clears it; a watched-root refusal lands in the watch slot only.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatusHost' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatus' ./internal/tui/`

**Commit:** `fix(tui): tell a refused repository from no repository in the git status seam`

## 5. Footer `git refused` marker, one-time note, `/git` answer

**What:** fix: a refused repository leaves the footer with no git segment and no explanation (issue `git-refusal-silent-footer`, part 1).
**Regression guard.** `TestE2EGitActionsRefusedRepository` (cmd/apogee/e2e_gitstatus_test.go) waits for `git refused` on the footer workspace row, then expects `gitexec.CommandConfigRefusal([]string{"core.sshcommand"})` from `/git` instead of `gitRefusedNote`; keep its Δ-absence watch and update the file header comment. Layout rule: every layout.md line saying a refused repository has no git segment or that `/git` notes `no git here` there (`rg -n 'refused repository|no git here|names a command to run' layout.md`, today "The git state." and "And the git state is a control too"). The marker paints through a new `footerToneError` (footerfit.go) whose `footerToneStyle` case (theme.go) returns `footerText.Foreground(errorFg)`; mode_test.go's tone loop extends to it. Supersedes ADR 0108 Decision 3 ("A refused repository gets no action, as it gets no git segment"): the segment is recorded by item 6's `adr-0108-amend-1118`; the repository still gets no git action.
**Goal:** while the shown root's slot holds a refusal, the footer's git segment renders `git refused` in the footer's error tone, same priority (`footerSegGit`). For the host workspace, the first landing that records a refusal sentence different from the last noted one posts one `noteRoutine` note: the sentence, then ` Move the key to your global git config if you trust it.`; repeat landings of the same sentence post nothing. `/git` and the footer click answer with that sentence instead of `noGitActionsNote`. `layout.md` "The git state." states the marker and the note.
**Approach (assumed at the header base):** extend `Model.footerGitSegment` / `gitFooterSegment` (footerfit.go, model.go), painted like `offlineLabel` (`footerText.Foreground(errorFg)`); the last-noted sentence is a plain string on `gitState`; `gitActionsRefusal` (gitactions.go) checks the refusal before `hasSnap`.
**Depends on:** item 4.
**Files:** internal/tui/gitstatus.go, internal/tui/footerfit.go, internal/tui/model.go, internal/tui/gitactions.go, internal/tui/gitstatus_test.go, layout.md, internal/tui/theme.go, internal/tui/mode_test.go, cmd/apogee/e2e_gitstatus_test.go
**Read first:** internal/tui/model.go — footerGitSegment, footerGit, noteRoutine; internal/tui/footerfit.go — gitFooterSegment, footerTone; internal/tui/gitactions.go — gitActionsRefusal; internal/tui/theme.go — footerToneStyle; cmd/apogee/e2e_gitstatus_test.go — TestE2EGitActionsRefusedRepository

**Tests:** driven through `fakeGitHost` returning `GitRefusedError{Sentence: gitexec.CommandConfigRefusal(...)}` (the exact emitted sentence): the rendered footer contains `git refused`; the transcript or session log holds the note once after two refused landings; a different sentence posts a second note; `/git` answers with the sentence; a non-repository still renders no segment. The tone-opener test covers `footerToneError` as distinct from the footer's own text; the e2e refused-repository test follows the guard.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitStatus|TestFooterGit|TestGitActions|TestFooterStatesTheGitSegment|TestFooterTones' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestE2EGitActionsRefused ./cmd/apogee/`

**Commit:** `fix(tui): show a refused repository in the footer git segment`

## 6. Record the posture: ADR 0012 and ADR 0108 amendments, manual, glossary

**What:** Recast at the regression check (2026-11-18). docs for items 1–5.
**Regression guard.** the refusal posture is recorded in ADR 0012 amend-0906, ADR 0056 Decision 4 and ADR 0108 Decision 3, not in ADR 0049. Replace item 6's ADR 0049 amendment with: a new Note `adr-0012-amend-1118` (merge drivers neutralised per call, merge-capable argv refused, unwind flags exempt, other keys still refuse the repository) named on ADR 0012's Status line and reading order; a new Note `adr-0108-amend-1118` (a refused repository shows the `git refused` marker, posts the one-time note, and `/git` answers with the sentence; it still gets no git action) named on ADR 0108's Status line and reading order; dated `> **Amended 2026-11-18 (git-refusal-silent-footer).**` blockquotes under ADR 0056 Decision 4's refusal paragraph and under adr-0049-amend-1103's key-set paragraph pointing at adr-0012-amend-1118. Update item 6's Goal, Files, Acceptance (`capcom know show "ADR 0012"` and `"ADR 0108"` name their amendments) and Sources accordingly. This supersedes, by amendment, ADR 0108 Decision 3 ("A refused repository gets no action, as it gets no git segment") and ADR 0056 Decision 4's "every repo-local key whose value is a program git executes". The prose rule's grep also covers the living design docs and layout.md (`docs/design/*.md`, not `archived/`; today `docs/design/confinement-execution-contract.md`'s staging hardened-argv paragraph); layout.md lines stay item 5's. `adr-0056.md` and `adr-0049.md` join Files: each Status line names its 2026-11-18 amendment (git-refusal-silent-footer). `adr-0012-amend-1118` states that `merge.<x>.driver` stays in the probe source because the probe is what lists the names to neutralise (an exception to amend-1103/1114's split rule), or a dated blockquote goes under amend-1114's rule sentence too.
**Goal:** a new Note `adr-0012-amend-1118` records the gitexec posture (merge drivers neutralised per call, merge-capable argv refused, unwind flags exempt, other keys still refuse the repository) and is named on ADR 0012's Status line and reading order; a new Note `adr-0108-amend-1118` records the footer side (a refused repository shows the `git refused` marker, posts the one-time note, `/git` answers with the sentence, still no git action) and is named on ADR 0108's Status line and reading order; ADR 0056 Decision 4's refusal paragraph and adr-0049-amend-1103's key-set paragraph each carry a dated `> **Amended 2026-11-18 (git-refusal-silent-footer).**` blockquote pointing at `adr-0012-amend-1118`, named on ADR 0056's and ADR 0049's Status lines; `adr-0012-amend-1118` states why `merge.<x>.driver` stays in the probe source. `docs/manual/building.md` no longer says a repo-local capcom-log driver breaks apogee's git tools (global stays recommended for the operator's own git merges); `docs/manual/commands.md` and `docs/manual/configuration.md` describe the marker, the note, the neutralisation and the global-config workaround; the glossary `confinement` Note matches.
**Approach (assumed at the header base):** `capcom know add --id adr-0012-amend-1118 --parent adr-0012 --title "…"` and `capcom know add --id adr-0108-amend-1118 --parent adr-0108 --title "…"`, then edit each parent's Status line and reading-order list. Prose rule: every manual, glossary or living design-doc line stating that a refused repository shows no git segment, or that any command-valued key refuses every git tool (`rg -n "refused|command-valued|merge\.\*\.driver|capcom-log" docs/manual docs/design/*.md layout.md .capcom/knowledge/glossary`). Historical text and closed ADR text stay as written, except the two dated blockquotes above.
**Depends on:** items 1 and 5.
**Files:** .capcom/knowledge/decisions/adr-0012.md, .capcom/knowledge/decisions/adr-0012/adr-0012-amend-1118.md, .capcom/knowledge/decisions/adr-0108.md, .capcom/knowledge/decisions/adr-0108/adr-0108-amend-1118.md, .capcom/knowledge/decisions/adr-0056.md, .capcom/knowledge/decisions/adr-0056/adr-0056-decision.md, .capcom/knowledge/decisions/adr-0049.md, .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md, docs/manual/building.md, docs/manual/commands.md, docs/manual/configuration.md, .capcom/knowledge/glossary/glossary-safety/confinement.md, docs/design/confinement-execution-contract.md
**Read first:** .capcom/knowledge/decisions/adr-0012.md — Status line, reading order; .capcom/knowledge/decisions/adr-0108/adr-0108-decision.md — decision 3; .capcom/knowledge/decisions/adr-0056/adr-0056-decision.md — decision 4 "Amended 2026-08-26";
  .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md — "Which keys."; .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1114.md — split rule; docs/manual/building.md — "The capcom merge driver.";
  docs/manual/commands.md — "They are your own acts."; docs/manual/configuration.md — "The hardened git reads run free in every mode"

**Tests:** none (docs).

**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0012"` names `adr-0012-amend-1118`; `capcom know show "ADR 0108"` names `adr-0108-amend-1118`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Drift|Doc' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|Dangerous' ./internal/config/`

**Commit:** `docs(git): record merge-driver neutralisation and the footer refusal marker`
