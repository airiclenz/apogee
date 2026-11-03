# Code-audit fixes plan (2026-11-03 audit)

**Goal:** Fix every finding of the 2026-11-03 whole-repository code audit in the owner's order: the bwrap confinement gap, the secret-guard bypasses, the read DoS paths, the slow-box defects, the one-line correctness bugs, then the two design-call findings.
**Date:** 2026-11-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 04458f9e

**Sources:**
- Issues: one per item, named on its **Closes:** line (`capcom ready --planned`; `capcom show <id>`)
- `docs/reviews/code-audit-2026-11-03.md` (the findings; its "Recommended Action Order" is the item order, and its "`:545`" second release-gate site does not exist)
- ADR 0081 (bwrap backend), ADR 0080 (commit-secrets), ADR 0049 + note 0914 (shell write view), ADR 0012 (footgun guard), ADR 0020 §2 (journal before label), ADR 0071 D1(c) (a Floor guard cannot regress Bypass), ADR 0056 D4 + its 2026-08-26/09-23 amendments (the floor's 2 s git contract; item 10 amends it for the probe)
- `docs/design/confinement-execution-contract.md`; AGENTS.md "A slow machine makes apogee slower, never broken"

**Ratified design calls** (owner, 2026-11-03):
- **bwrap:** add `--unshare-pid`; amend ADR 0081 and the contract, which said never.
- **ConfineToWorkspace:** `domain.Config` takes `Unconfined bool` (zero value confines); the host maps `confine-to-workspace:` to it by negation; the Engine/Agent `ConfineToWorkspace()` accessors keep their names.
- **Read cache:** declines whenever the earlier copy is not whole in the request (elided in the store, or over the result cap's ceiling); one shared predicate.
- **PDF quadratic scan:** in scope.
- **alias.\*:** stays allowed; only keys git runs unprompted join the write view.
- **SafeReadFile cap:** SafeReadFile is non-blocking and regular-file-only everywhere but stays uncapped (journal capture, read-back, undo readCurrent keep covering large files); a new capped variant (64 MiB, `ErrTooLarge`) serves only the tool-facing reads: edit, find-replace and diagnostics.
- **mcpauth:** login required only for OAuth `invalid_grant`/`invalid_client` (any 4xx) or a 400/401 without a parseable code; 408, 429 and other 4xx are a plain error naming the status.
- **Budgets:** `go vet` 300 s; the git work-tree probe its own 30 s constant; the per-snapshot 2 s stays.

**Standing requirements:**
- skills: coding-standards
- `make test` is the only supported whole-suite run; every per-item `go test` names one package and a `-run` filter.
- A deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `alias.*` as a control-plane write.
- The bench's own `domain.Config` literals (they live in the `apogee-sim` repo). After item 19 a bench Config that omits the field fences Auto and argv reactions, so the bench (apogee-sim) must set `Unconfined: true` where it means unconfined at its next apogee bump; item 19's CHANGELOG entry names the `domain.Config` API change for embedders.

**Regression check (2026-11-03, 04458f9e):**
- 1: guard folded
- 2: guard folded
- 3: guard folded
- 4: recast (backslash+LF only; the CR-LF branch dropped)
- 5: guard folded
- 6: guard folded (Acceptance split)
- 6: rejected — exempt the boolean `core.fsmonitor` values because the item breaks `TestGit_RepoLocalFsmonitorTrueIsNotRefused`; that test sets the key with `runInRepo` and drives the `git_status` probe (gitexec), never the shell write view, so item 6 leaves it untouched
- 7: recast (SafeReadFile stays uncapped; a capped variant serves the tool-facing reads)
- 8: guard folded
- 9: guard folded
- 10: recast (bounded retry; supersedes ADR 0056 D4's 2 s contract for the probe by amendment)
- 12: guard folded
- 13: guard folded
- 14: guard folded
- 15: guard folded
- 16: recast (`$$` only in argv[1:])
- 18: guard folded
- 19: guard folded (plus the bench Driver note)
- 6: writer decision — git-boolean `core.fsmonitor` values (true/false/yes/no/on/off/1/0) are exempt from the write view: they toggle git's builtin daemon and run no workspace script; `./x` still hard-refuses
- 7: guard folded (SafeReadFile's unbounded-contract comments kept; only tool-facing "no bound" comments reworded; FIFO test named under an Acceptance prefix; supersedes the diagnostics.go `diagnoseGo` "No size bound is added" comment)
- 10: guard folded (gitexec sentinel for TimedOut/DrainWedged; a scriptedGit variant scripts the probe's own calls, one TempDir gitPath per row)

## 1. Run the bwrap backend in its own PID namespace — ✅ DONE (2026-11-03)

NOTES (2026-11-03): consequential edit — .capcom/knowledge/decisions/adr-0081/adr-0081-consequences.md: made necessary by adding `--unshare-pid` (its "`--unshare-pid` … out of scope and stay so" bullet became false; a dated blockquote amends it, the bullet text is untouched)
NOTES (2026-11-03): the no-survivor test also backgrounds a `setsid` sleep and covers the clean-exit path as well as cancel; survivors are found by unique fractional sleep durations scanned in host /proc/*/cmdline. Both new tests and TestNamespaceArgv were confirmed to fail with the flag removed. The `/proc/<pid>/root` write did not succeed even pre-fix on this host (EACCES), so the status-read assertion is what discriminates here
NOTES (2026-11-03): docs/manual/reactions.md "`setsid` … is the one way to leave something running" left as is — it describes your own (unconfined) `run:` entries, which the bwrap change does not reach

**What:** Fix (audit High): bwrap runs without `--unshare-pid` while `--proc /proc` mounts the host process table, so a confined same-uid command reaches `/proc/<apogee-pid>/root/…` (writes outside the box) and, with `ptrace_scope=0`, apogee's memory.
**Regression guard.** The doc rule is: every hit of `grep -rn 'unshare-pid' internal docs/design .capcom/knowledge` no longer forbids the flag, save amendment text, testdata fixtures and the ADR 0081 Decision text (`adr-0081-decision.md:77`, superseded by `adr-0081-amend-1103`, never rewritten); the contract's line-wrapped "never / `--unshare-pid`" at :231-232 and :1178-1179 are sites; docs/plans and docs/reviews stay out of the check. Every comment stating a setsid escapee survives the call notes the bwrap exception (`grep -rn 'survives the call\|outside that reach' internal docs/design`). The no-survivor test finds survivors from the host side (a unique argv marker in host `/proc/*/cmdline`, or the outer bwrap's pgid), never by a pid printed inside the box.
**Goal:** every bwrap-confined run (and the probe) carries `--unshare-pid`; a confined command can neither see apogee's PID in `/proc` nor write a host path through `/proc/<pid>/root`; teardown still kills the whole confined tree; the docs no longer say "never `--unshare-pid`".
**Approach (assumed at the header base):** add `--unshare-pid` to the fixed prefix in `namespaceArgv` (internal/platform/namespace_linux.go) beside `--die-with-parent`; the probe reuses `namespaceArgv`, so a host refusing CLONE_NEWPID falls to `CauseLaunchRefused` and the selector's next rung. Rewrite the file header and `namespaceArgv` doc (the "hides the process group" rationale and the "fresh procfs … its own process tree" claim). Amend ADR 0081 with a dated child Note `adr-0081-amend-1103` (`capcom know add --id adr-0081-amend-1103 --parent adr-0081`), named on the record's Status line and reading-order list; amend the contract's §2 namespace paragraph, its Linux done-criteria and the §2.4 setsid residual (the namespace now dies with its pid 1). Every doc or comment stating "never `--unshare-pid`" changes: `grep -rn 'unshare-pid' internal docs/design .capcom/knowledge` (exceptions in the guard above).
**Files:** internal/platform/namespace_linux.go; internal/platform/namespace_linux_test.go; internal/platform/teardown_unix.go; internal/platform/doc.go; internal/subprocess/subprocess.go; internal/console/doc.go; internal/console/process.go; docs/design/confinement-execution-contract.md; .capcom/knowledge/decisions/adr-0081.md; .capcom/knowledge/decisions/adr-0081/adr-0081-amend-1103.md
**Read first:** internal/platform/namespace_linux.go — namespaceArgv, probeNamespace; internal/platform/namespace_linux_test.go — TestNamespaceArgv; internal/platform/teardown_unix.go — NewProcessTeardown, pgroupTeardown.Reap; internal/platform/teardown_unix_test.go — setsid escape test; docs/design/confinement-execution-contract.md — §2 namespace paragraph; .capcom/knowledge/decisions/adr-0081/adr-0081-decision.md
**Tests:** `TestNamespaceArgv` asserts `--unshare-pid` present (it asserts absent today); new real-bwrap tests (skip without bwrap, like `TestNamespaceProbe`): a confined `sh -c` cannot write a host temp file through `/proc/<os.Getpid()>/root/<path>` and `/proc/<os.Getpid()>/status` does not name apogee's process; a confined `sh -c 'sleep 600 & sleep 600'` killed through the teardown path leaves no survivor, found from the host side (a unique argv marker scanned in host `/proc/*/cmdline`, or the outer bwrap's pgid), never by a pid printed inside the box.
**Acceptance:** `go test -count=1 -run 'TestNamespace|TestResolveBwrap|TestSetConfinedPgid' ./internal/platform/`; `go vet ./internal/platform/`; every hit of `grep -rn 'unshare-pid' internal docs/design .capcom/knowledge` outside amendment text, testdata fixtures and the ADR 0081 Decision Note no longer forbids the flag (contract :231-232 and :1178-1179 included); every hit of `grep -rn 'survives the call\|outside that reach' internal docs/design` that states the setsid escapee survives notes the bwrap exception.
**Closes:** bwrap-host-pid-namespace
**Commit:** `fix(platform): run bwrap confinement in its own PID namespace`

## 2. List staged names NUL-delimited in the commit-secrets pre-check — ✅ DONE (2026-11-03)

NOTES (2026-11-03): the `-z` listing splits through a new `nulFields` helper beside `scanStagedSecrets` (no TrimSpace, so a path's own edge whitespace survives); `nonEmptyLines` is unchanged and keeps serving the rev-parse output. The new test pins `core.quotePath=true` repo-locally so a global `core.quotePath=false` cannot hide the bite; it fails (0 approvals) with the fix reverted.

**What:** Fix: `git diff --cached --name-only` C-quotes non-ASCII paths, so `données/.env` reaches `isSecretBearingName` as `.env"` and skips the forced look (ADR 0080).
**Regression guard.** `writeRepoFile` is a bare `os.WriteFile`: the test creates `données/` and `日本語/` (os.MkdirAll, or a MkdirAll in `writeRepoFile`) and stages them with `mustGit add`, never `stageSecrets` (its id_rsa + keys.go force the look on their own and hide the bite); the test is named `TestCommitSecrets…` so the Acceptance filter runs it.
**Goal:** a `git_commit` staging a secret-named file under a non-ASCII or quote-bearing directory forces the commit-secrets approval exactly as an ASCII path does.
**Approach (assumed at the header base):** in `scanStagedSecrets` (internal/agent/secretsguard.go) add `-z` to the `--name-only` diff and split its output on `\x00` (precedent: `treesnapshot.go`'s porcelain `-z` split), not through `nonEmptyLines`; `nonEmptyLines` keeps serving the rev-parse output. `addedFilePath` already unquotes headers — leave it.
**Files:** internal/agent/secretsguard.go; internal/agent/secretsguard_test.go
**Read first:** internal/agent/secretsguard.go — scanStagedSecrets, nonEmptyLines; internal/security/secrets.go — isSecretBearingName, addedFilePath; internal/agent/treesnapshot.go — porcelain `-z` split; internal/agent/secretsguard_test.go — newSecretsRepo, writeRepoFile; internal/agent/treesnapshot_test.go — mustGit
**Tests:** new real-repo test `TestCommitSecrets…` (helpers `newSecretsRepo`, `writeRepoFile` after an `os.MkdirAll` of each directory, `mustGit add`; not `stageSecrets`) staging only `données/.env` and `日本語/server.pem` with harmless contents → `GuardForceApproval` naming each path unquoted.
**Acceptance:** `go test -count=1 -run 'CommitSecrets' ./internal/agent/`
**Closes:** staged-names-quoted-paths
**Commit:** `fix(agent): read staged names NUL-delimited in the commit-secrets check`

## 3. Treat `+++ ` as a file header only after a `--- ` line — ✅ DONE (2026-11-03)

**What:** Fix: `SecretFindings` skips every line starting `+++ `, so an added content line `++ AKIA…` is never scanned and resets the attributed path.
**Regression guard.** A removed `-- x` line renders `--- x` inside a hunk, so "previous line began `--- `" alone still takes a following `+++ AKIA…` as a header: a `+++ ` line is a header only when the previous line began `--- ` AND no `@@` line has come since the last `diff --git` line (or since the start of the diff).
**Goal:** an added line whose content begins `++ ` is scanned as content and attributed to its file; real `+++ b/<path>` headers still set the path; ADR 0080 D3's finding order is unchanged.
**Approach (assumed at the header base):** in `SecretFindings` (internal/security/secrets.go) track header state: a `+++ ` line is a header only when the previous line began `--- ` and no `@@` line has come since the last `diff --git` line (or the start); otherwise it is content (`line[1:]`).
**Files:** internal/security/secrets.go; internal/security/secrets_test.go
**Read first:** internal/security/secrets.go — SecretFindings, addedFileHeader, addedFilePath; internal/security/secrets_test.go — TestSecretFindingsMatchesEachClass, addedLineDiff ("the +++ header never matches", "a quoted header path…" rows have no `diff --git` line); internal/agent/secretsguard.go — scanStagedSecrets
**Tests:** rows in `TestSecretFindingsMatchesEachClass`: an added `++ AKIA…` line is found under its file; an added `++ x` line between two files does not re-attribute a later finding; a hunk whose removed `-- x` line is followed by an added `++ AKIA…` line finds the key under its file; the existing "the +++ header never matches" row still passes.
**Acceptance:** `go test -count=1 -run 'TestSecretFindings|TestSecretsHint' ./internal/security/`
**Closes:** secret-scan-plusplus-lines
**Commit:** `fix(security): scan added lines that begin with ++ in the secrets diff`

## 4. Consume backslash-newline in the shell tokenizer — ✅ DONE (2026-11-03)

NOTES (2026-11-03): added rows beyond the plan's list — a double-quoted continuation (`rm -rf ".git/\⏎hooks"` → `.git/hooks`), a single-quoted one that stays in the word (`rm 'a\⏎b'`), and a TestWriteTargetsOf twin of the backslash-CR-LF row — because the plan's `echo 'a\⏎b' > f` row yields `f` whatever the quoted text holds.

**What:** Recast at the regression check (2026-11-03). Fix: `scan` and `scanDoubleQuoted` keep `\`+newline as a literal newline word, so `cd .git && \⏎ ls hooks` is falsely refused and `rm -rf .git/\⏎hooks` hides its target.
**Regression guard.** Only backslash+LF joins lines; drop the backslash-CR-LF branch (sh treats it as escaped CR then a command-ending LF). Add a regression row: `echo ok \<CR><LF>rm -rf .git/hooks` still hard-refuses write-git-control-plane; fix the Goal line to match. The TestWriteTargetsOf row `cd .git && \⏎ ls hooks` expects ".git" (cd is not a read leader); the rule-level row stays TierNone.
**Goal:** a backslash-newline (backslash+LF only; a backslash-CR-LF stays an escaped CR followed by a command-ending LF, as in `sh`) outside single quotes joins lines as `sh` does: it emits nothing, ends no command and splits no word; `WithoutHeredocBodies` keeps its rune spans exact.
**Approach (assumed at the header base):** in `scan` and `scanDoubleQuoted` (internal/security/shellwrites.go), when `\` is followed by `\n` advance past both and write nothing (a following `\r\n` is not joined); single quotes stay literal. `WithoutHeredocBodies` (used by internal/tools/terminal.go) cuts by rune index, so skip indices, never rebuild the string.
**Files:** internal/security/shellwrites.go; internal/security/shellwrites_test.go; internal/security/dangerous_test.go
**Read first:** internal/security/shellwrites.go — shellTokenizer.scan, scanDoubleQuoted, skipHeredocBodies, WithoutHeredocBodies, endCommand; internal/security/allowmatch.go — allowCandidates, isSameWordList (second splitSimpleCommands reader; scanMatchableShellLine already refuses `\` and newline, so allowmatch_test.go "a line continuation joins what the view splits" stays false); internal/security/shellwrites_test.go — TestWriteTargetsOf, TestWithoutHeredocBodies; internal/security/dangerous_test.go — TestShellWriteViewJudgesWhatTheCommandWrites; internal/security/rules.go — write-git-control-plane; internal/tools/terminal.go — preflightCommandLine
**Tests:** `TestWriteTargetsOf` rows: `cd .git && \⏎ ls hooks` → `.git` (cd is not a read leader); `rm -rf .git/\⏎hooks` → `.git/hooks`; `echo 'a\⏎b' > f` keeps the quoted text. `TestShellWriteViewJudgesWhatTheCommandWrites`: the first stays TierNone, the second hard-refuses `write-git-control-plane`, and `echo ok \<CR><LF>rm -rf .git/hooks` still hard-refuses `write-git-control-plane`. `TestWithoutHeredocBodies` row with a continuation before a heredoc.
**Acceptance:** `go test -count=1 -run 'TestWriteTargetsOf|TestShellWriteView|TestWithoutHeredocBodies' ./internal/security/`
**Closes:** shell-line-continuation-words
**Commit:** `fix(security): join backslash-newline continuations in the shell write view`

## 5. Skip wrapper option values in `stripWrappers` — ✅ DONE (2026-11-03)

NOTES (2026-11-03): short option clusters follow getopt: a cluster whose first value letter is its last letter (`sudo -Eu root`) consumes the next word, one with the value letter earlier (`-uroot`) carries its value attached; long options (`--user root`) still strip bare — only the plan's short-option table is consumed.
NOTES (2026-11-03): `env -S 'rm -f x'` now consumes the split string as a value, leaving no leader; its words are not re-split into a command (no target before or after, so unchanged behaviour).

**What:** Fix: `stripBareOptions` drops wrapper options without their values, so `env -u echo rm -f .git/hooks/pre-commit` takes `echo` as the leader and emits no target. Depends on item 4.
**Regression guard.** The Goal is bounded to the per-wrapper value table: value-taking options of listed wrappers not in it (`exec -a`, GNU `time -o`/`-f`) stay bare, as the Approach's ADR 0012 line says. The `xargs` row is spelled with the value separate (`xargs -I {} rm {}`: "rm {}" today, "{}" after); the attached `xargs -I{} rm {}` stays a separate unchanged-behaviour row (stripBareOptions already drops `-I{}`).
**Goal:** for each option in the per-wrapper value table, a value-taking option consumes its value word; the wrapped leader is the true command for every form listed below.
**Approach (assumed at the header base):** one per-wrapper table of value-taking short options beside `wrapperLeaders` (pattern: `intoDirectoryValueOptions`): `env -u -C -S`, `sudo -u -g -h -p -C -D -R -T -U`, `nice -n`, `xargs -I -n -s -P -d -E -a -L`; `stripWrappers` consumes the value word for a listed option given separately; `--opt=value` and attached `-uroot` stay one word. Options not listed stay bare (ADR 0012: footgun guard).
**Files:** internal/security/shellwrites.go; internal/security/shellwrites_test.go; internal/security/dangerous_test.go
**Read first:** internal/security/shellwrites.go — stripWrappers, stripBareOptions, wrapperLeaders, intoDirectoryValueOptions, readsStdinScript; internal/security/shellwrites_test.go — TestWriteTargetsOf (wrapper rows); internal/security/dangerous_test.go — TestShellWriteViewJudgesWhatTheCommandWrites; internal/security/allowmatch.go — allowCandidates
**Tests:** `TestWriteTargetsOf` rows: `env -u echo rm -f .git/hooks/pre-commit`, `sudo -u root rm .git/config`, `nice -n 5 tee .git/hooks/x`, `xargs -I {} rm {}` each yield the real target; `xargs -I{} rm {}` yields "{}" before and after (unchanged-behaviour row); rule-level row: the `env -u` form hard-refuses `write-git-control-plane`; existing wrapper rows unchanged.
**Acceptance:** `go test -count=1 -run 'TestWriteTargetsOf|TestShellWriteView|TestProjectConfig' ./internal/security/`
**Closes:** wrapper-option-values-leader
**Commit:** `fix(security): consume wrapper option values before naming the leader`

## 6. Count `core.fsmonitor` and include paths as git-control-plane writes

**What:** Fix: `git config core.fsmonitor ./x` synthesises no `.git/config` target, so `write-git-control-plane` never fires on a key git runs unprompted. Depends on item 5.
**Regression guard.** Every per-item `go test` names one package: the tools half of the Acceptance runs alone (`./internal/tools/`), and `internal/gitexec` is dropped from it (no test there matches `Fsmonitor|HooksPath`). A `git config core.fsmonitor <value>` whose value is a git boolean (true/false/yes/no/on/off/1/0, case-insensitive) is not a control-plane write — it toggles git's builtin daemon and runs no workspace script — so the write view exempts boolean values for core.fsmonitor only; add rows: `git config core.fsmonitor true` and `git config core.fsmonitor false` stay unrefused, `git config core.fsmonitor ./x` hard-refuses.
**Goal:** `git config <key> <value>` (repo-local) for `core.fsmonitor`, `interactive.difffilter`, `include.path` and `includeIf.<cond>.path` hard-refuses `write-git-control-plane` (a git-boolean `core.fsmonitor` value excepted); `alias.*` and the repo-probe refusal set are unchanged; ADR 0049 records the synthesis.
**Approach (assumed at the header base):** extend only the write-view suffix of `security.GitCommandConfigName` (`|core\.hookspath`) in internal/security/shellwrites.go; never `GitCommandConfigNameSource` (the `gitexec` probe would refuse such repos). The boolean exemption checks the value word for `core.fsmonitor` only, in `gitConfigWritesCommandKey`. Add a dated amendment `adr-0049-amend-1103` (child Note, named on the Status line and reading-order list) recording that the write view synthesises `.git/config` for command-valued and include keys, superseding note 0914's "Nothing is synthesised" for that case (first done by commit af21d0be).
**Files:** internal/security/shellwrites.go; internal/security/shellwrites_test.go; internal/security/dangerous_test.go; .capcom/knowledge/decisions/adr-0049.md; .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md
**Read first:** internal/security/shellwrites.go — GitCommandConfigName, GitCommandConfigNameSource, gitConfigWritesCommandKey; internal/gitexec/gitexec.go — CommandConfigName comment, hardeningOptions; internal/tools/git_test.go — TestGit_RepoLocalFsmonitorTrueIsNotRefused; .capcom/knowledge/decisions/adr-0049.md — Status line, reading-order list
**Tests:** `TestWriteTargetsOf` and `TestShellWriteViewJudgesWhatTheCommandWrites` rows for each key; `git config alias.co checkout`, `git config --global core.fsmonitor x`, `git config core.fsmonitor true` and `git config core.fsmonitor false` stay unrefused; `git config core.fsmonitor ./x` hard-refuses `write-git-control-plane`.
**Acceptance:** `go test -count=1 -run 'TestWriteTargetsOf|TestShellWriteView' ./internal/security/`; `go test -count=1 -run 'Fsmonitor|HooksPath' ./internal/tools/`
**Closes:** git-config-fsmonitor-write
**Commit:** `fix(security): treat core.fsmonitor and include paths as control-plane writes`

## 7. Make `SafeReadFile` and the copy source non-blocking and bounded

**What:** Recast at the regression check (2026-11-03). Fix: `SafeReadFile` and `openCopySource` open blocking, so a planted FIFO wedges a Turn, and `SafeReadFile` reads unbounded.
**Regression guard.** Ratified design call (owner, 2026-11-03) — "**SafeReadFile cap:** SafeReadFile is non-blocking and regular-file-only everywhere but stays uncapped (journal capture, read-back, undo readCurrent keep covering large files); a new capped variant (64 MiB, `ErrTooLarge`) serves only the tool-facing reads: edit, find-replace and diagnostics." A 64 MiB cap on SafeReadFile itself would reach undo `readCurrent`/`tracked.plan` and write_target's `capturePreImage`/`commitReadBack`, silently losing the journal of a delete, copy, move or write on a large file. SafeReadFile's unbounded-contract comments stay (safeio.go package SCOPE note and the SafeReadFile doc, "applies no size bound of its own"), naming the capped variant there as the bounded alternative; reword only comments saying a tool-facing read (edit, find-replace, diagnostics) takes no bound: `grep -rniE 'unbounded|no size bound|must use SafeOpen' internal/security/safeio.go internal/tools/diagnostics.go internal/tools/file_edit.go internal/tools/find_replace.go internal/tools/write_target.go`. This supersedes `diagnoseGo`'s comment (internal/tools/diagnostics.go) "No size bound is added — a cap would stop a large but legitimate source file from being diagnosable"; its rewording says so. The FIFO tool test is named `TestEditExistingFile_RefusesAFIFOWithoutBlocking` so the Acceptance regex runs it.
**Goal:** `SafeReadFile` refuses a FIFO, socket, device or directory without blocking (`ErrNotRegular`) and stays uncapped, so journal capture, read-back and undo `readCurrent` keep covering large files; a new capped variant refuses the same kinds and a file over 64 MiB (new sentinel `ErrTooLarge`, "file too large: N bytes (max M)") and serves only edit, find-replace and diagnostics; the copy source refuses a FIFO without blocking and wraps `ErrNotRegular`; edit and find-replace show both refusals verbatim, never "file not found".
**Approach (assumed at the header base):** route `SafeReadFile` (internal/security/safeio.go) through `SafeOpen` and refuse `IsDir`, reading whole (no cap). Add the capped variant beside it in internal/security, reading via a LimitReader of cap+1 (pattern: `readAllBounded`, internal/tools/path_read.go); new constant `maxSafeReadBytes = 64 << 20`. Edit and find-replace read through a capped `writeTarget` read (e.g. `writeTarget.readBounded` beside `writeTarget.read`, internal/tools/write_target.go) and `diagnoseGo` through the capped variant; `capturePreImage`, `commitReadBack` and internal/undo (`readCurrent`, `tracked.plan`) keep SafeReadFile. `openCopySource` opens `O_RDONLY|openNonblock` and wraps `ErrNotRegular`. `escapeOrMessage` (internal/tools/path_read.go) passes `ErrTooLarge` through. Keep SafeReadFile's unbounded-contract comments, naming the capped variant as the bounded alternative; reword only the tool-facing "no bound" comments (the grep in the Regression guard). Callers keep their error handling: write_file still writes when the before-read fails.
**Files:** internal/security/safeio.go; internal/security/safeio_fifo_unix_test.go; internal/security/safeio_test.go; internal/tools/path_read.go; internal/tools/write_target.go; internal/tools/file_edit.go; internal/tools/find_replace.go; internal/tools/diagnostics.go; internal/tools/file_edit_unix_test.go (new)
**Read first:** internal/security/safeio.go — SafeReadFile, SafeOpen, ErrNotRegular, openCopySource, package SCOPE note; internal/tools/write_target.go — writeTarget.read, capturePreImage, commitReadBack; internal/tools/file_edit.go — Execute (target.read, notFound); internal/tools/find_replace.go — target.read (both tools);
internal/tools/diagnostics.go — diagnoseGo; internal/tools/path_suggest.go — notFoundOrRefusal; internal/tools/path_read.go — escapeOrMessage, readAllBounded; internal/undo/journal.go — readCurrent
**Tests:** `safeOpenWithin`-style FIFO tests for `SafeReadFile`, the capped variant and `CopyFile` (no block, `errors.Is(err, ErrNotRegular)`); over-cap sparse file → `ErrTooLarge` from the capped variant, while `SafeReadFile` has no size refusal; tool-level, in a new untagged-unix `internal/tools/file_edit_unix_test.go` (pattern: `read_file_unix_test.go`; `syscall.Mkfifo` does not build on Windows): `TestEditExistingFile_RefusesAFIFOWithoutBlocking`: `file_edit` on a FIFO answers "not a regular file: <path>" within the deadline; existing internal/undo `TestRevert*`/`TestRedo*` pass.
**Acceptance:** `go test -count=1 -run 'TestSafeReadFile|TestSafeOpen|TestSafeCopyFile' ./internal/security/`; `go test -count=1 -run 'TestEditExistingFile|TestSingleFindReplace|TestMultiFindReplace|TestFindReplace|TestWriteFile|TestWriteTarget|TestDiagnostics' ./internal/tools/`; `go test -count=1 -run 'TestRevert|TestRedo' ./internal/undo/`
**Closes:** safe-read-fifo-unbounded
**Commit:** `fix(security): refuse non-regular and oversized files in SafeReadFile`

## 8. Cap Anthropic reasoning blocks per reply

**What:** Fix: `openReasoning` charges nothing for a `thinking` block, so endless block starts grow memory without bound.
**Regression guard.** The per-block cost is a named constant beside `maxReasoningBlocks`, and a Tests row exercises the text-cap clause (N blocks plus thinking text sized to exactly `maxReplyTextBytes - N*cost + 1` trips the MiB message): at any cost under 32 KiB the 256-block count trips first, so no other row observes it. The `textBytes` field comment (wire_anthropic_stream.go:131-134) names the per-block charge.
**Goal:** a reply opening more than `maxReasoningBlocks` (256) reasoning blocks in total, or exceeding the text cap through per-block charges, ends with a FaultLocal `DeltaError`; a normal interleaved-thinking reply is unaffected.
**Approach (assumed at the header base):** in internal/provider/wire_anthropic_stream.go count every reasoning block opened in the reply (open/close cycles included, not only `len(s.reasoning)`) and charge a fixed per-block cost through `s.charge`; the trip message is `apogee: streamed reply opened more than 256 reasoning blocks`, built from the constant beside `maxOpenToolCalls` in internal/provider/client.go.
**Files:** internal/provider/wire_anthropic_stream.go; internal/provider/client.go; internal/provider/wire_anthropic_stream_test.go
**Read first:** internal/provider/wire_anthropic_stream.go — anthropicStream.openReasoning, anthropicStream.charge, anthropicStream.stopBlock; internal/provider/client.go — maxOpenToolCalls, maxReplyTextBytes; internal/provider/wire_anthropic_stream_test.go — anthropicToolUseOpeningsSSE, TestAnthropicParseSSE_ReplyTextIsCapped; internal/provider/stream_test.go — assertReplyTextCapDeltas
**Tests:** builder like `anthropicToolUseOpeningsSSE` for thinking openings: 257 never-closed blocks trip; 257 open-close cycles trip; 256 flush normally; N blocks plus thinking text to exactly `maxReplyTextBytes - N*cost + 1` trip the text-cap message; existing `TestAnthropicParseSSE_Thinking` and `_ReplyTextIsCapped` pass.
**Acceptance:** `go test -count=1 -run 'TestAnthropicParseSSE' ./internal/provider/`
**Closes:** anthropic-thinking-block-cap
**Commit:** `fix(provider): cap reasoning blocks in a streamed Anthropic reply`

## 9. Expand each PDF reference array once and honour cancellation

**What:** Fix: `decodedReferences` re-scans a referenced array for every `/Contents` or `/ToUnicode` site naming it, with no ctx check, so a hostile PDF stalls a Turn quadratically.
**Regression guard.** `TestExtractPDF_HonoursACancelledContext` already gets `pdfCancelledCause` at base (largestInflation's ctx check, pdf.go:555-557) and nothing lets a test cancel mid-scan, so the cancellation row tests `decodedReferences(ctx, …)` directly with a pre-cancelled ctx on the 5k×5k fixture, expecting the ctx error; the item's bite comes from the `testtime.Budget` timing case alone.
**Goal:** each referenced object's members are scanned at most once per preflight, and a cancelled ctx stops the site loop with the existing cancel cause.
**Approach (assumed at the header base):** in internal/doctext/pdf.go give `decodedReferences` a ctx and a separate `expanded` set (never reuse `named`: a number first seen as a member must still have its own members expanded); `preflightPDF` passes its ctx and maps the error to `pdfCancelledCause`.
**Files:** internal/doctext/pdf.go; internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdf.go — decodedReferences, preflightPDF, pdfReferencesAt, indexPDFObjects, chargesInflation; internal/doctext/pdf_test.go — hostilePDF, TestExtractPDF_HonoursACancelledContext, TestExtractPDF_ChargesAContentStreamWhateverItsLabel
**Tests:** `hostilePDF` with one array of 5k references and 5k sites naming it extracts within a `testtime.Budget` bound (the item's bite); a member-first-then-site case still decodes; a new `TestDecodedReferences…` test calling `decodedReferences(ctx, …)` directly with a pre-cancelled ctx on the 5k×5k fixture returns the ctx error.
**Acceptance:** `go test -count=1 -run 'TestExtractPDF|TestDecodedReferences' ./internal/doctext/`
**Closes:** pdf-reference-quadratic-scan
**Commit:** `fix(doctext): scan each PDF reference array once and honour cancellation`

## 10. Retry the git work-tree probe after a transient failure

**What:** Recast at the regression check (2026-11-03). Fix (slow-box): `active()` runs under `sync.Once`, so one timed-out or cancelled `rev-parse` turns the tracked-file mutation floor off for the Agent's life.
**Regression guard.** Bound the retry: after 3 consecutive transient probe failures the probe latches "no" for the Agent's life; add a scriptedGit row (3 TimedOut rev-parses, then the next call runs no rev-parse). Classify CommandConfigIncompleteRefusal (incompleteRefusalLead) as transient and CommandConfigRefusal as definitive, preferring a gitexec sentinel over prefix matching. The cancelled-first-call row drives a.tree.beforeCall/active directly with a scriptedGit variant whose Spawn returns ctx.Err() when ctx is done. Guard rule: every comment in internal/agent describing the probe as once-per-Agent or 2 s-bounded is rewritten (`grep -n 'once\|2-second\|probeOnce' internal/agent/treesnapshot.go`). Supersede ADR 0056 D4's 2 s floor-git contract for the probe explicitly: add a dated child Note `adr-0056-amend-1103` (capcom know add --id adr-0056-amend-1103 --parent adr-0056), named on the record's Status line and reading-order list, recording the probe's own 30 s budget and bounded retry (snapshots keep 2 s); add ADR 0056 to the header Sources and the amendment files to item 10's Files. In gitexec.queryDiagnosed, wrap the TimedOut and DrainWedged errors in an exported sentinel, keeping Error() text unchanged; treesnapshot classifies errors.Is(sentinel) || runCtx.Err() != nil as transient. The incomplete-refusal and command-config-refusal rows use a scriptedGit variant that scripts the probe's own calls (TimedOut on `rev-parse --git-path config` for incomplete, a listing naming core.sshcommand for refusal); each row takes its own TempDir gitPath so gitexec's commandConfigProbes memo cannot carry over.
**Goal:** only a definitive answer latches (a work tree; git exit non-zero for "not a work tree"; git absent; a planted git); a timeout, wedged drain, cancelled ctx or incomplete command-config check leaves the probe unresolved so the next subprocess call retries, until 3 consecutive transient failures latch "no" for the Agent's life; the probe has its own 30 s budget (ADR 0056 D4 amended for it).
**Approach (assumed at the header base):** in internal/agent/treesnapshot.go replace `probeOnce` with a mutex-guarded unknown/yes/no state; classify by `ctx.Err()`/run-ctx deadline, a new exported `gitexec` sentinel wrapping the TimedOut/DrainWedged errors of `queryDiagnosed` (transient) and a `gitexec` sentinel for the incomplete-check refusal (transient) versus the command-config refusal (definitive), in preference to matching `incompleteRefusalLead`; count consecutive transient failures and latch "no" at 3. New `treeProbeTimeout = 30 * time.Second`; `treeSnapshotTimeout` stays 2 s for snapshots. Rewrite every comment describing the probe as once-per-Agent or 2 s-bounded (the struct and `active()` docs, the 2-second contract at :39). Amend ADR 0056 with `adr-0056-amend-1103`.
**Files:** internal/agent/treesnapshot.go; internal/agent/treesnapshot_test.go; internal/gitexec/gitexec.go; .capcom/knowledge/decisions/adr-0056.md; .capcom/knowledge/decisions/adr-0056/adr-0056-amend-1103.md
**Read first:** internal/agent/treesnapshot.go — treeSnapshotter, active, git, beforeCall, treeSnapshotTimeout; internal/gitexec/gitexec.go — queryDiagnosed, probeCommandConfig, probeGit, CommandConfigIncompleteRefusal, IsIncompleteRefusal; internal/agent/treesnapshot_test.go — scriptedGit, withEngineGit, TestTreeSnapshot_PlantedGitTurnsTheFloorOff, TestTreeSnapshot_OverrunListingSkipsTheCheck;
internal/agent/dispatch.go — floorCtx, a.tree.beforeCall; internal/tools/git.go — RunGitQuery; .capcom/knowledge/decisions/adr-0056/adr-0056-decision.md — decision 4 and its amendments
**Tests:** with `scriptedGit`: a TimedOut first `rev-parse` then a good one → the second call warns; 3 TimedOut rev-parses, then the next call runs no rev-parse; an incomplete command-config refusal is retried, a command-config refusal latches (both through a scriptedGit variant that scripts the probe's own calls — TimedOut on `rev-parse --git-path config`, a listing naming core.sshcommand — each row with its own TempDir gitPath); a cancelled first call (driving `a.tree.beforeCall`/`active` directly with a scriptedGit variant whose Spawn returns `ctx.Err()` when ctx is done) then a good one → warns; exit 128 latches off and is not re-run; `TestTreeSnapshot_PlantedGitTurnsTheFloorOff` passes.
**Acceptance:** `go test -count=1 -run 'TestTreeSnapshot' ./internal/agent/`
**Closes:** worktree-probe-negative-latch
**Commit:** `fix(agent): retry the work-tree probe after a transient git failure`

## 11. Report a timed-out `go vet` as skipped

**What:** Fix (slow-box): `runGoVet` ignores `TimedOut`/`DrainWedged`, so a killed vet reads "go vet reported problems (exit code -1)" after a fixed 30 s.
**Goal:** a timed-out or wedged vet yields an ok result: the syntax verdict plus `go vet skipped: timed out after 5m0s` (or `… left its output pipe held open`), with no "go vet checked" line; `vetTimeout` is 300 s.
**Approach (assumed at the header base):** in internal/tools/diagnostics.go check `res.TimedOut` and `res.DrainWedged` in `runGoVet` before the exit-code branch and return a skip note `diagnoseGo` appends the way it appends the existing "go vet skipped: …" notes; set `vetTimeout = 300 * time.Second`.
**Files:** internal/tools/diagnostics.go; internal/tools/diagnostics_test.go
**Read first:** internal/tools/diagnostics.go — runGoVet, diagnoseGo, goVetSpec, vetTimeout; internal/tools/exec_host_test.go — capturedRunHost; internal/tools/diagnostics_test.go — TestDiagnostics_VetRunsThroughTheHost; internal/subprocess/subprocess.go — SubprocessResult.TimedOut, SubprocessResult.DrainWedged
**Tests:** a host whose `run` returns `{TimedOut: true, ExitCode: -1}` → ok result with the skip line and no "go vet checked"; same for `DrainWedged`; `TestDiagnostics_VetRunsThroughTheHost` still matches `goVetSpec`.
**Acceptance:** `go test -count=1 -run 'TestDiagnostics' ./internal/tools/`
**Closes:** go-vet-timeout-misreported
**Commit:** `fix(tools): report a timed-out go vet as skipped, not as findings`

## 12. Read release-gate paths NUL-delimited

**What:** Fix (slow-box/fail-closed): `touchesOnlyPeelablePaths` splits `git diff-tree --name-only` with `strings.Fields`, so a spaced house plan path (`2026-11-03 - 00 - x.md`) stops the peel and the gate reports a clear release as un-run by CI.
**Regression guard.** The diff-tree fixture constants (gate_test.go, shared through `fakeRepo` by `clearedRepo` and `newPublishFake`) become NUL-terminated, re-copied from real `-z` output so the bumpPaths "copied from real output" comment stays true; `fakeRepo.respond` and `TestGatePeelsOnlyTrailingChangelogAndVersionCommits` move to the new indices (len 6, parent `args[4]`, commit `args[5]`); Acceptance covers `TestStatus` and `TestPublish`.
**Goal:** the gate peels a prose-only commit whose paths contain spaces or non-ASCII bytes exactly as it peels ASCII ones.
**Approach (assumed at the header base):** in `peelBumps` (cmd/release/gate.go) call `git diff-tree -r -z --name-only <parent> <commit>`; `touchesOnlyPeelablePaths` splits on `\x00` and drops empty fields. Update `fakeRepo.respond` and `TestGatePeelsOnlyTrailingChangelogAndVersionCommits`, which match diff-tree by argument index.
**Files:** cmd/release/gate.go; cmd/release/gate_test.go
**Read first:** cmd/release/gate.go — peelBumps, touchesOnlyPeelablePaths; cmd/release/cifilter.go — isSkippedByCI; cmd/release/gate_test.go — fakeRepo.respond, path fixture consts, TestGatePeelsOnlyTrailingChangelogAndVersionCommits; cmd/release/status_test.go — clearedRepo; cmd/release/publish_test.go — newPublishFake
**Tests:** gate test with a trailing prose commit touching `docs/plans/2026-11-03 - 00 - x.md` → peeled and gate passes; a non-ASCII path row; fixture constants NUL-terminated; existing gate, status and publish tests pass.
**Acceptance:** `go test -count=1 -run 'TestGate|TestCIPushPaths|TestStatus|TestPublish' ./cmd/release/`
**Closes:** release-gate-spaced-paths
**Commit:** `fix(release): split gate path lists on NUL, not whitespace`

## 13. Open a new tool call when an indexed fragment brings a new id

**What:** Fix: `openToolCalls.address` merges a fragment into the call at its wire index even when its non-empty id differs, folding shims' all-`index:0` parallel calls into one.
**Regression guard.** An index hit opens a new call only when the differing non-empty id comes WITH a function name (a call's start); an id-only fragment keeps today's continue-and-never-overwrite rule (amend the stream.go:540-541 comment). On an index hit with a differing id, first continue an entry carrying that index AND that id; open only when none exists, so `0/a name`, `0/b name`, `0/a args` never yields a duplicate id `a`.
**Goal:** two streamed calls sharing a wire index but carrying different non-empty ids emit as two calls with their own ids, names and arguments; an empty-id continuation joins the most recently opened call at that index; Anthropic streams are unaffected.
**Approach (assumed at the header base):** in internal/provider/stream.go `address`: an index hit whose entry id is non-empty and differs from the fragment's non-empty id first continues an entry with that index and that id; failing that, a fragment that also carries a function name opens a new call (through `open`, keeping the `maxOpenToolCalls` cap), and an id-only fragment continues the hit as today; `atIndex` returns the latest entry at that index.
**Files:** internal/provider/stream.go; internal/provider/stream_test.go
**Read first:** internal/provider/stream.go — openToolCalls.address, openToolCalls.atIndex, openToolCalls.fold, openToolCalls.open, openToolCalls.withID; internal/provider/wire_anthropic_stream.go — anthropicStream.foldCall; internal/provider/stream_test.go — sseServer, TestStream_RepeatedIDContinuesOneCall
**Tests:** `sseServer` replay: two `index:0` chunks with ids `a`/`b`, then an empty-id arguments chunk → two calls, args valid JSON, continuation on `b`; `0/a name+args`, then `0/b` args-only with no name → one call `a` with valid JSON; `0/a name`, `0/b name`, `0/a args` → two calls, no duplicate id; `0/a name`, `0/a args` → one call; existing `TestStream_*` pass.
**Acceptance:** `go test -count=1 -run 'TestStream_' ./internal/provider/`
**Closes:** stream-index-zero-ids
**Commit:** `fix(provider): keep distinct-id tool calls apart at a shared index`

## 14. Refuse MCP server names that make tool names ambiguous

**What:** Fix: `<alias>__<tool>` collides when an alias contains `__` (`a__b`+`c` vs `a`+`b__c`) or ends in `_` (`a_`+`b` vs `a`+`_b`).
**Regression guard.** Project-config entries never reach `fileMCPServers`: `checkRepoServer` (internal/config/execlayer.go) calls `ValidateName` too, so a bad project name drops that one entry with the `ignoring mcp-servers entry "a_"` notice instead of `validateServers` refusing the whole `ConnectWith`. `TestServerToolNameDistinct` asserts its pairs differ and stays as is; the rule is tested in a separate `TestServerConfigValidateName` plus `qualifyToolName("a","b__c") == "a__b__c"`.
**Goal:** an `mcp-servers:` entry whose `name` contains `__` or ends in `_` is refused at config load and at `Connect` with a message naming the entry; with valid names no two qualified tool names collide.
**Approach (assumed at the header base):** a `ServerConfig.ValidateName` method in internal/mcp, called from `validateServers` (internal/mcp/client.go), from `fileMCPServers` (internal/config/config.go) and from `checkRepoServer` (internal/config/execlayer.go) beside the other `Validate*` calls. Document the rule in docs/manual/configuration.md's `mcp-servers` `name` paragraph.
**Files:** internal/mcp/client.go; internal/mcp/transport.go; internal/mcp/mcp_test.go; internal/mcp/tool_test.go; internal/config/config.go; internal/config/config_test.go; internal/config/execlayer.go; internal/config/execlayer_test.go; docs/manual/configuration.md
**Read first:** internal/mcp/client.go — validateServers, ConnectWith; internal/config/execlayer.go — checkRepoServer, repoMCPServers; internal/config/config.go — fileMCPServers; internal/mcp/tool.go — qualifyToolName; internal/mcp/tool_test.go — TestServerToolNameDistinct; internal/config/execlayer_test.go — TestRepoMCPServerRefusedAloneKeepsTheTightenOnlyKeys
**Tests:** `TestConnect_RejectsBadServerNames` subtests for `a__b` and `a_`; a config-load test refusing the same; a `TestRepoMCPServerRefusedAloneKeepsTheTightenOnlyKeys` row expecting the `ignoring mcp-servers entry "a_"` notice; new `TestServerConfigValidateName` (refuses `a__b`, `a_`; accepts `a_b`, `a.b`) plus `qualifyToolName("a","b__c") == "a__b__c"`; `TestServerToolNameDistinct` unchanged.
**Acceptance:** `go test -count=1 -run 'TestConnect_RejectsBadServerNames|TestServerToolName|TestServerConfigValidate' ./internal/mcp/`; `go test -count=1 -run 'MCP' ./internal/config/` (matches `TestRepoMCPServer…`)
**Closes:** mcp-tool-name-collision
**Commit:** `fix(mcp): refuse server names that make tool names ambiguous`

## 15. Reserve "login required" for refusals a login fixes

**What:** Fix: `requestRefresh` maps every 4xx from the token endpoint to `ErrLoginRequired`, so a 429 or 408 tells the user to run `apogee mcp login`.
**Regression guard.** The Goal is scoped to a refresh the token endpoint answers with 4xx: the handler's other `loginRequired` paths (insufficient_scope 403, record gone, client changed, no refresh token, no token endpoint) stay. `requestRefresh`'s doc comment (handler.go:249-250, "A 4xx answer … is ErrLoginRequired") is rewritten to the new rule.
**Goal:** a refresh the token endpoint answers with 4xx returns `LoginRequiredError` only when the OAuth `error` is `invalid_grant` or `invalid_client` (any 4xx) or a 400/401 has no parseable code; 408, 429 and other 4xx return a plain error naming the status and the server's description.
**Approach (assumed at the header base):** in internal/mcpauth/handler.go have `refreshRejection` (or a sibling) return the parsed `tokenErrorResponse.Error` code, and branch in `requestRefresh` on code then status; rewrite its doc comment to match.
**Files:** internal/mcpauth/handler.go; internal/mcpauth/handler_test.go
**Read first:** internal/mcpauth/handler.go — Handler.requestRefresh, refreshRejection, tokenErrorResponse, loginRequired, Handler.refreshLocked; internal/mcpauth/handler_test.go — fakeTokenServer.serveToken, TestHandlerNeedsALogin; cmd/apogee/mcp_cmd_test.go — TestStartupMCPLoginRetriesAConnectWhoseRefreshWasRejected
**Tests:** `fakeTokenServer` gains a status/body knob: 400 `invalid_grant`, 401 no body, 400 `invalid_client` → login required; 429, 408, 400 `invalid_request` → not `ErrLoginRequired`, message names the status; `TestHandlerNeedsALogin` passes.
**Acceptance:** `go test -count=1 -run 'TestHandler' ./internal/mcpauth/`; `go test -count=1 -run 'TestStartupMCPLogin' ./cmd/apogee/`
**Closes:** mcpauth-4xx-login-required
**Commit:** `fix(mcpauth): report login required only for refusals a login fixes`

## 16. Escape `%` and `$` in systemd ExecStart arguments

**What:** Recast at the regression check (2026-11-03). Fix: `systemdQuote` triggers quoting on `%` and `$` but escapes only `\` and `"`; systemd expands both inside double quotes.
**Regression guard.** Escape `%`→`%%` in every argv element, but `$`→`$$` only in argv[1:] (systemd never env-expands the executable path); unitFieldsFor passes the index. Add a linux row asserting a `$` Executable stays single-`$`, and fix the Goal line accordingly.
**Goal:** a binary or `--config` path containing `%` renders as `%%`, and an argument after the executable containing `$` renders as `$$`, in the unit's `ExecStart` (the executable path keeps a single `$`: systemd never env-expands it), so systemd runs each path as written.
**Approach (assumed at the header base):** in `systemdQuote` (cmd/apogee/daemoninstall.go) add `%`→`%%` for every argv element and `$`→`$$` for argv[1:] only; `unitFieldsFor` passes the element's index.
**Files:** cmd/apogee/daemoninstall.go; cmd/apogee/daemoninstall_test.go
**Read first:** cmd/apogee/daemoninstall.go — systemdQuote (its only caller is unitFieldsFor), unitFieldsFor, renderDaemonUnit; cmd/apogee/daemoninstall_test.go — TestDaemonInstallQuotesAPathWithSpaces, installFixture, TestDaemonInstallRendersTheGoldenUnitForEveryOS, TestDaemonInstallLeavesTheHomeUnpinnedWhenNoFlagWasPassed; cmd/apogee/testdata/apogee-daemon.service.golden — ExecStart
**Tests:** `TestDaemonInstallQuotesAPathWithSpaces`-style linux rows for `/opt/a%b/apogee` (→ `%%`) and a `--config` under `/home/$x/.apogee` (→ `$$`) asserting the rendered `ExecStart`; a row whose Executable is under a `$`-named dir (`/home/u/d$x/apogee`) asserting it stays single-`$`; the golden unit test passes.
**Acceptance:** `go test -count=1 -run 'TestDaemonInstall' ./cmd/apogee/`
**Closes:** systemd-quote-percent-dollar
**Commit:** `fix(apogee): escape percent and dollar in systemd unit arguments`

## 17. Roll the label journal back when its flush fails

**What:** Fix (ADR 0020 §2 journal-before-label): `Journal.record` keeps the new entry in memory when `flush` fails, so a retry sees it, skips the flush and labels a path no on-disk journal names.
**Goal:** after a failed flush the in-memory entries equal those before the call, and a retried `record` flushes again.
**Approach (assumed at the header base):** in `Journal.record` (internal/platform/winlabel/session.go) copy `j.rec.Entries` before `recordEntry` (it promotes in place) and restore the copy on a flush error.
**Files:** internal/platform/winlabel/session.go; internal/platform/winlabel/journal_state_test.go
**Read first:** internal/platform/winlabel/session.go — Journal.record, Journal.flush, Journal.unwind, Journal.Entries; internal/platform/winlabel/journal.go — recordEntry, WriteJournal, JournalPath; internal/platform/winlabel/journal_state_test.go — TestJournalWithoutAHomeIsNotWritableAndFlushesNothing
**Tests:** untagged test: force `WriteJournal` to fail (journal directory path is a regular file) → `record` errors, `Entries()` unchanged; fix the path, retry → `changed=true` and the file holds the entry; same for a descendant-promotion case.
**Acceptance:** `go test -count=1 -run 'TestJournal' ./internal/platform/winlabel/`; `GOOS=windows go vet ./internal/platform/winlabel/`
**Closes:** winlabel-journal-flush-rollback
**Commit:** `fix(winlabel): roll the journal back when its flush fails`

## 18. Decline the read cache when the earlier copy is not whole

**What:** Fix (ADR 0071 D1(c) breach): `priorSuccessfulReadUnchanged` checks only for a prune stub, so a read result the tool-result cap elides (or `requestRoom.fit` already elided) still counts as in the conversation and a needed re-read is capped to one line.
**Regression guard.** Share only the ceiling half: `CapToolResults` keeps its `len(content) > maxChars` trim condition through an `overCeiling` helper, and the read-cache predicate is `IsPruneStub || elided || overCeiling`; routing the trim through the whole "not whole" predicate would send an elided result under the ceiling through `TruncateToolResult` (which trims unconditionally), breaking resultcap.go's "only a result STRICTLY over the ceiling is trimmed". The agent-level Acceptance filter is `TestFloorGuard_ToolResultCap`.
**Goal:** the read cache leaves a re-read untouched when the earlier copy carries an elision marker or exceeds `capMaxChars(view.Budget())` (when non-zero); both guards share the ceiling test (`overCeiling`); the read cache alone adds the stub and elision checks.
**Approach (assumed at the header base):** export an elision test from internal/context covering `TruncateToolResult`'s and `ElideToBytes`'s markers; in internal/floor an unexported `overCeiling` helper (`len(content) > maxChars`) that `CapToolResults`'s trim condition keeps using, and the read-cache predicate `IsPruneStub || elided || overCeiling` used by `priorSuccessfulReadUnchanged`.
**Files:** internal/context/toolresult.go; internal/context/toolresult_test.go; internal/floor/readcache.go; internal/floor/resultcap.go; internal/floor/readcache_test.go; internal/agent/floorguards_test.go
**Read first:** internal/floor/readcache.go — CacheRead, priorSuccessfulReadUnchanged; internal/floor/resultcap.go — CapToolResults, capMaxChars; internal/context/toolresult.go — TruncateToolResult, ElideToBytes; internal/context/prune.go — IsPruneStub; internal/agent/floorguards_test.go — TestFloorGuard_ToolResultCapTrimsAnOlderResultUnderBypass
**Tests:** `CacheRead` with an earlier copy over the ceiling, or carrying either marker → untouched; under the ceiling → still capped; `FakeLoopView.BudgetValue` unknown window → today's behaviour; agent-level `TestFloorGuard_ReadCache*` row driving a large earlier read; an elided result under the ceiling is not trimmed by `CapToolResults`; resultcap tests pass.
**Acceptance:** `go test -count=1 -run 'TestCacheRead|TestCapToolResults|TestCapMaxChars' ./internal/floor/`; `go test -count=1 -run 'TestFloorGuard_ReadCache|TestFloorGuard_ToolResultCap' ./internal/agent/`; `go test -count=1 -run 'Elid|Truncate' ./internal/context/`
**Closes:** read-cache-elided-copy
**Commit:** `fix(floor): skip the read cache when the earlier copy is not whole`

## 19. Make a zero `domain.Config` confine Auto

**What:** Fix: `Config.ConfineToWorkspace` is a plain bool documented as defaulting to true, so a Driver that omits it runs Auto unconfined. The bench Driver (apogee-sim) changes too: a bench Config that omits the field now fences Auto and argv reactions, so the bench must set `Unconfined: true` where it means unconfined at its next apogee bump; the item's CHANGELOG sidecar names the `domain.Config` API change for embedders.
**Regression guard.** The flag is read in every mode (`syncPermitCtx` fences sync argv reactions and `RunProjectReaction`): every test Config omitting the field, in ANY mode, that runs an argv reaction, or a ModeAuto Config, gets `Unconfined: true` where it meant unconfined (`adviseArgvAgent`, `gateConfig`, sessionadvise_user_test's "exited 3" row among them). Every Go reader or writer of domain/apogee.Config's field changes (`grep -rn 'ConfineToWorkspace[^(]' --include='*.go' . | grep -v 'config.Options\|probe.Inputs'`, including agent.go:145,148's comments). `projectionOptions` (wire_config_test.go:29) and wire_firing_test.go:84 flip to `ConfineToWorkspace: false` and assert `Unconfined == true`, so the projected value stays off its zero.
**Goal:** `domain.Config` has `Unconfined bool` and no `ConfineToWorkspace` field; a zero Config with `ModeAuto` and a Confiner fences; every shipped Driver keeps its configured behaviour; the host key `confine-to-workspace:` and the Engine/Agent `ConfineToWorkspace()` accessors are unchanged.
**Approach (assumed at the header base):** in internal/domain/config.go replace the field (doc: zero value confines, as `FloorConfig`'s Disable fields; precedent `internal/daemon` `Unconfined`); `construct.go` seeds `confineToWorkspace: !cfg.Unconfined`. Producers: `projectConfig` (cmd/apogee/wire_config.go), `(*lateEngine).seedConfig` (cmd/apogee/wire_engine.go), `childCfg` (internal/agent/subagent.go). Consumers beyond construct read the Agent accessor and stay. Every Go reader or writer of the domain/apogee.Config field flips (among them internal/agent dispatch_test, hookpermit_test, recipe_test, setmode_test, syncexec_test; internal/run/run_test.go; cmd/apogee wire_server_test, wire_config_test, wire_firing_test, schedule_test, confinement_e2e_test); every test Config omitting it that runs an argv reaction in any mode, or runs ModeAuto, now confines — add `Unconfined: true` where a test meant unconfined. CHANGELOG.md names the `domain.Config` API change for embedders and the bench. Every doc naming the domain field changes: `grep -rn 'Config.ConfineToWorkspace\|ConfineToWorkspace:' --include='*.go' --include='*.md' .`
**Files:** internal/domain/config.go; internal/agent/construct.go; internal/agent/subagent.go; internal/agent/agent.go; cmd/apogee/wire_config.go; cmd/apogee/wire_engine.go; internal/agent/*_test.go; internal/run/run_test.go; cmd/apogee/wire_server_test.go; cmd/apogee/wire_config_test.go; cmd/apogee/wire_firing_test.go; cmd/apogee/schedule_test.go; cmd/apogee/confinement_e2e_test.go
**Read first:** internal/domain/config.go — Config.ConfineToWorkspace; internal/agent/construct.go — confineToWorkspace seed; internal/agent/dispatch.go — syncPermitCtx; internal/agent/subagent.go — newChildAgentOn; cmd/apogee/wire_config.go — projectConfig; cmd/apogee/wire_engine.go — lateEngine.seedConfig; cmd/apogee/wire_config_test.go — projectionOptions, assertCarriesProjection
**Tests:** new internal/agent test: zero Config + ModeAuto + fake Confiner → a subprocess call runs confined; `Unconfined: true` → unconfined row; `assertCarriesProjection` and wire_firing_test assert `Unconfined == true` from a `ConfineToWorkspace: false` fixture; existing confine tests pass.
**Acceptance:** `go build ./...`; `go vet ./internal/agent/ ./internal/domain/ ./cmd/apogee/ ./internal/run/`; `go test -count=1 -run 'Confine|SetMode|Dispatch|Recipe|SyncExec|HookPermit|AdviseArgv|Gate|SessionAdvise|SessionStart|Reactions|WriteEscape|SecretsGuard|Guardrails|WriteDetection|Background' ./internal/agent/`; `go test -count=1 -run 'Config|Server|Firing|Schedule|E2EAuto' ./cmd/apogee/`; `go test -count=1 ./internal/run/`
**Closes:** zero-config-auto-unconfined
**Commit:** `fix(domain): make a zero Config confine Auto mode`
