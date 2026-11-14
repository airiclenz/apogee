# git remote/url/submodule program keys + smoke GO111MODULE plan

**Goal:** No repo-local git config key reaches a program through apogee's own git unconfined: `remote.<n>.vcs` is refused by the gitexec probe, and the shell write view treats a repo-local write of the named program-reaching families — `remote.<n>.uploadpack`/`receivepack`, `protocol.allow`/`protocol.ext.allow`, `ext::` url rewrites, and a `!` submodule update — as a git control-plane write. smoke's goproxy check survives an inherited `GO111MODULE=off`.
**Date:** 2026-11-14
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 219e0e49
**Closes:** git-remote-program-keys

**Sources:**
- `.capcom/issues/git-remote-program-keys.md`, `.capcom/issues/smoke-goproxy-go111module.md`
- `docs/reviews/code-audit-2026-11-10.md` (Medium, "Git command-config denylist omits keys that make git run a program")
- ADR 0049 and `adr-0049-amend-1103` (`capcom know show "ADR 0049" --with-children`)
- Sibling fix commit `8e9f23cf` (gpg.ssh.defaultKeyCommand)

**Verified at write time (git 2.47.3):** the git actions/status hosts already pin `-c protocol.ext.allow=never -c protocol.git.allow=never -c submodule.recurse=false`, `--upload-pack=git-upload-pack`, `--receive-pack=git-receive-pack`, `--recurse-submodules=no`, so uploadpack/receivepack, ext:: (via `url.<x>.insteadOf` or `protocol.ext.allow`) and `submodule.<n>.update=!cmd` reach no program through gitexec callers. `remote.origin.vcs = a/b` with an executable `./git-remote-a/b` DOES run on the actions host's fetch (git resolves a slashed helper name against cwd).

**Ratified design calls:**
- **vcs gap:** refuse repo-local `remote.<n>.vcs` by name in `GitCommandConfigNameSource`; no `GIT_ALLOW_PROTOCOL` allowlist (owner, 2026-11-14).
- **Key split:** follow `adr-0049-amend-1103`: keys already neutralised per verb go in the shell write view's suffix only, never in the probe source (owner brief, 2026-11-14).
- **Program-reaching subset:** the write-view suffix judges only values that name a program (protocol ext/allow, ext:: rewrites, `!` submodule updates); benign rewrites and update modes stay clear (writer decision on regression check, 2026-11-14, under amend-1103's rule).

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi 5: never `go test -race`/`-cover` over a whole package or pattern; one package, one test: `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; never two test runs at once.
- No AI attribution in commits.

**Out of scope:**
- `GIT_ALLOW_PROTOCOL`; moving per-verb hardening into gitexec.
- VERSION or release changes.
- `mergetool.<tool>.path` / `difftool.<tool>.path` (run only when the operator types `git mergetool`/`difftool`, like alias.*).

**Regression check (2026-11-14, 219e0e49):**
- 2: recast — suffix narrowed to the program-reaching subset (`protocol.allow`/`protocol.ext.allow`, `ext::` rewrites, `!` submodule updates); yields to `adr-0049-amend-1103` "What stays clear"; acceptance re-pointed, gitexec comments added to Files.
- 3: guard folded — the amendment lists item 2's final suffix; `capcom know add` gains the required `--title`.
- 4: guard folded — Long-help width gets an acceptance check; the CHANGELOG sidecar amends the existing goproxy-check line.
- 2 (re-check): guard folded — a `protocol.allow` / `protocol.ext.allow` value of `never` is exempt (clear rows in both test tables); Tests say clear rows name no `.git/config`.
- 2 (re-check): decided — removal forms (`--unset`/`--unset-all`/`unset`) stay judged as writes for every key, no new exemption; one test row pins `git config --unset remote.origin.uploadpack` as naming `.git/config`.

## 1. Refuse a repo-local remote.<n>.vcs — ✅ DONE (2026-11-14)

**What:** fix: the gitexec command-config denylist misses `remote.<n>.vcs`, so a planted helper name runs unconfined on the git actions host's fetch.
**Goal:** `security.GitCommandConfigNameSource` matches `remote.<n>.vcs` (any remote name, lowered); the git tool and the git actions host refuse a repository carrying it, naming the key, and a planted `git-remote-<vcs>` never runs.
**Approach (assumed at the header base):** extend the `remote\..*\.proxy` alternative in `GitCommandConfigNameSource` (internal/security/shellwrites.go) to `remote\..*\.(proxy|vcs)` (POSIX-ERE only). Update every comment that enumerates the class: `GitCommandConfigNameSource`, `gitexec.CommandConfigName` and `internal/gitexec/doc.go`; grep `rg -n "proxy command|gpg.program" internal/gitexec internal/security cmd/apogee` for others. Correct the `gitFetchArgs` comment in cmd/apogee/gitstatus_host.go so it states the probe now covers `remote.<n>.vcs` while uploadpack and ext:: stay neutralised per verb.
**Files:** internal/security/shellwrites.go, internal/gitexec/gitexec.go, internal/gitexec/doc.go, internal/gitexec/gitexec_test.go, internal/tools/git_test.go, cmd/apogee/gitstatus_host.go, cmd/apogee/gitactions_host_test.go
**Read first:** internal/security/shellwrites.go — GitCommandConfigNameSource; internal/gitexec/gitexec.go — CommandConfigName; internal/gitexec/gitexec_test.go — TestCommandConfigRefusal_NamesTheSshSigningKeyCommand;
internal/tools/git_test.go — TestGit_RefusesRepoLocalCommandConfig; cmd/apogee/gitactions_host_test.go — TestGitActionsHostRebaseRefusesAnSshSigningKeyCommand; cmd/apogee/gitactions_host.go — gitActionsHost.Fetch;
cmd/apogee/gitstatus_host.go — gitFetchArgs; cmd/apogee/gitstatus_host_test.go — writeFixtureFile (writes 0o644: chmod the planted helper)

**Tests:**
- gitexec_test.go: `TestCommandConfigRefusal_NamesTheRemoteHelper` modelled on `TestCommandConfigRefusal_NamesTheSshSigningKeyCommand` (`[remote "origin"]\n\tvcs = a/b\n` → `CommandConfigRefusal([]string{"remote.origin.vcs"})`).
- tools/git_test.go: add `{key: "remote.origin.vcs", value: "a/b", canonical: "remote.origin.vcs"}` to `TestGit_RefusesRepoLocalCommandConfig`.
- gitactions_host_test.go: `TestGitActionsHostFetchRefusesARemoteHelper` modelled on `TestGitActionsHostRebaseRefusesAnSshSigningKeyCommand`: plant `git-remote-a/b` (POSIX script touching a marker, skip on Windows), exclude it via `.git/info/exclude`, set `remote.origin.vcs a/b`, call the host's fetch; assert the error names `remote.origin.vcs` and the marker does not exist. Must fail on the base tree.

**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestCommandConfigRefusal' ./internal/gitexec/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGit_RefusesRepoLocalCommandConfig' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGitActionsHost' ./cmd/apogee/`

**Commit:** `fix(security): refuse a repo-local remote.<name>.vcs helper`

## 2. Shell write view: remote/url/protocol/submodule program keys are control-plane writes

**What:** Recast at the regression check (2026-11-14). fix: a shell `git config` write of `remote.<n>.uploadpack`, `remote.<n>.receivepack`, `url.<x>.insteadOf`, `protocol.<x>.allow` or `submodule.<n>.update` into the repository's own config is not judged a git control-plane write; the operator's next ordinary git runs what it names.
**Regression guard.** The suffix covers only names that reach a program, per adr-0049-amend-1103's "what stays clear" rule: `remote\..*\.(uploadpack|receivepack)`, `protocol\.allow|protocol\.ext\.allow` (never other protocol.<x>.allow), `url\.ext::.*\.(insteadof|pushinsteadof)` (only a rewrite whose base starts with the ext:: transport; a benign `url.git@github.com:.insteadOf` stays clear — URL-form helper names cannot contain a slash, so ext:: is the only rewrite that names a program), and `submodule\..*\.update` with a value exemption in gitConfigWritesCommandKey beside the fsmonitor-boolean one: only a value starting with `!` is a control-plane write (`none`, `rebase`, `merge`, `checkout` stay clear). Tests cover each clear case too. The item yields to `.capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md` "What stays clear". A second value exemption sits beside the fsmonitor and `!` ones: a `protocol.allow` / `protocol.ext.allow` whose value word lowers to `never` is no write (a hardening line names no program). Removal forms keep today's behaviour: `--unset`/`--unset-all`/`unset` stay judged as writes for every key, the new families included (no new exemption).
**Goal:** `security.GitCommandConfigName` matches those five name families (lowered) in addition to everything it matched before, so `gitConfigWritesCommandKey` synthesises `.git/config` and `write-git-control-plane` hard-refuses e.g. `git config url.ext::sh.insteadOf origin`; a `--global` write and every read form stay clear.
**Approach (assumed at the header base):** append `remote\..*\.(uploadpack|receivepack)|url\.ext::.*\.(insteadof|pushinsteadof)|protocol\.allow|protocol\.ext\.allow|submodule\..*\.update` (with the `!`-value exemption above) to the suffix of `GitCommandConfigName` in internal/security/shellwrites.go — never to `GitCommandConfigNameSource`. Extend the `GitCommandConfigName` comment to say why each family is suffix-only (neutralised per verb by the git hosts' `-c protocol.ext.allow=never`, `--upload-pack`/`--receive-pack`, `submodule.recurse=false`). `url.<x>.pushInsteadOf` joins the `url` family (same rewrite).
**Depends on:** item 1 (same file).
**Files:** internal/security/shellwrites.go, internal/security/shellwrites_test.go, internal/security/dangerous_test.go, internal/gitexec/gitexec.go, internal/gitexec/doc.go (rule: every comment enumerating the write view's suffix keys — `rg -n 'interactive\.diff[Ff]ilter' internal cmd`)
**Read first:** internal/security/shellwrites.go — GitCommandConfigName, gitConfigWritesCommandKey, gitConfigReadForms, gitConfigOtherFileOptions; internal/security/shellwrites_test.go — TestWriteTargetsOf;
internal/security/dangerous_test.go — TestShellWriteViewJudgesWhatTheCommandWrites; internal/gitexec/gitexec.go — CommandConfigName (doc comment);
.capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md — What stays clear

**Tests:**
- shellwrites_test.go (`TestWriteTargetsOf`): table cases beside the existing `core.fsmonitor` / `include.path` ones: each family set repo-locally names `.git/config` (`remote.origin.uploadpack`, `remote.origin.receivepack`, `protocol.allow always`, `protocol.ext.allow always`, `url.ext::sh.insteadOf`, `url.ext::sh.pushInsteadOf`, `submodule.lib.update '!cmd'`); mixed case (`url.ext::X.insteadOf`) matches; clear cases name no `.git/config` (every git config row keeps its operands): `protocol.file.allow always`, `protocol.ext.allow never`, `protocol.allow never`, `url.git@github.com:.insteadOf https://github.com/`, `submodule.lib.update` with `none`, `rebase`, `merge`, `checkout`; `--global` and `--get` forms name nothing; `git config --unset remote.origin.uploadpack` names `.git/config` (removal forms stay writes).
- dangerous_test.go (`TestShellWriteViewJudgesWhatTheCommandWrites`): one end-to-end case per family through the guard, asserting the `write-git-control-plane` hard refusal, beside the existing `git config core.fsmonitor ./x` case, plus the clear cases above passing unrefused.

**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWriteTargetsOf|TestShellWriteViewJudgesWhatTheCommandWrites' ./internal/security/`

**Commit:** `fix(security): judge remote, url, protocol and submodule program keys as git control-plane writes`

## 3. ADR 0049 amendment: the key list widens

**What:** record items 1–2 as a dated amendment to ADR 0049 (the key list in `adr-0049-amend-1103` changes).
**Regression guard.** the amendment lists item 2's final suffix exactly as decided above, including the submodule value exemption, and says so in its Goal. `capcom know add` requires `--title`: run `capcom know add --id adr-0049-amend-1114 --parent adr-0049 --title "ADR 0049 amendment of 2026-11-14: ..." --body-file -`.
**Goal:** a child Note `adr-0049-amend-1114` under `adr-0049` states the new keys, which side each lives on (probe source: `remote.<n>.vcs`; write-view suffix: item 2's final suffix exactly — `remote.<n>.uploadpack`/`receivepack`, `protocol.allow`/`protocol.ext.allow`, `url.ext::<x>.insteadOf`/`pushInsteadOf`, and `submodule.<n>.update` only for a value starting with `!`) and why; the record's Status line names it and its reading-order list ends with it; `capcom check` is clean.
**Approach (assumed at the header base):** `capcom know add --id adr-0049-amend-1114 --parent adr-0049 --title "ADR 0049 amendment of 2026-11-14: ..." --body-file -`, then edit `.capcom/knowledge/decisions/adr-0049.md` Status line and reading-order sentence. Cite issue `git-remote-program-keys` and this plan.
**Depends on:** items 1, 2.
**Files:** .capcom/knowledge/decisions/adr-0049.md, .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1114.md
**Read first:** .capcom/knowledge/decisions/adr-0049.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0049/adr-0049-amend-1103.md — "Which keys", "What stays clear"

**Tests:** none (docs).

**Acceptance:**
- `capcom know show "ADR 0049" --with-children | grep -n "amend-1114"`
- `capcom check`

**Commit:** `docs(decisions): ADR 0049 amendment — remote, url, protocol and submodule program keys`

## 4. smoke's goproxy check pins GO111MODULE

**What:** fix: `goProxyEnv` (cmd/release/smoke.go) does not pin `GO111MODULE`, so an inherited `GO111MODULE=off` fails `go list -m` and the goproxy check reports FAIL; the smoke command's Long help has one 133-column line.
**Regression guard.** the item's CHANGELOG sidecar entry amends the existing [Unreleased] line (CHANGELOG.md, goproxy check env pins) to include GO111MODULE rather than adding a separate contradictory line. The Long-help width rule counts the string content ("text"), not source columns, and an Acceptance command checks it.
**Goal:** `goProxyEnv` contains `GO111MODULE=on`; `TestSmokeGoProxyPinsTheProxyOutsideTheCheckout` asserts it with the other pins; no line of the smoke command's Long help string exceeds its neighbours' width (≤ 92 columns of text per line).
**Approach (assumed at the header base):** add the entry to `goProxyEnv` and its comment; reflow the `Long:` concatenation in the smoke command constructor without changing the words.
**Closes:** smoke-goproxy-go111module
**Files:** cmd/release/smoke.go, cmd/release/smoke_test.go
**Read first:** cmd/release/smoke.go — goProxyEnv, checkGoProxy, newSmokeCommand; cmd/release/smoke_test.go — TestSmokeGoProxyPinsTheProxyOutsideTheCheckout, runTestSmoke; cmd/release/runner.go — runner.run, execRunner.run;
CHANGELOG.md — [Unreleased] goproxy-check entry

**Tests:** extend `TestSmokeGoProxyPinsTheProxyOutsideTheCheckout` to require `GO111MODULE=on`.

**Acceptance:**
- `go build ./cmd/release/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSmoke' ./cmd/release/`
- `sed -n '/Long: "smoke proves/,/last step/p' cmd/release/smoke.go | sed -E 's/^[^"]*"//; s/(\\n)?" ?\+?,?$//' | awk 'length > 92 {exit 1}'`

**Commit:** `fix(release): pin GO111MODULE for smoke's goproxy check`
