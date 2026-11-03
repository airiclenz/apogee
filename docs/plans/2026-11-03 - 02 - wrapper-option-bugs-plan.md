# Wrapper option bugs — `time -o` output file, `sudo -h` optional host

**Goal:** close the two open shell-write-view bugs in `internal/security/shellwrites.go`: the file GNU `time -o` / `--output` writes becomes a write target, and sudo's `-h` is read as an optional-argument option.
**Date:** 2026-11-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 88b30ef5

**Regression check (2026-11-03, 88b30ef5):**
- 1: guard folded (output-name resolver beside `resolveLongOption`; output values ordered before the leader's operand targets, emitted once)

**Sources:**
- `.capcom/issues/time-output-file-unreported.md`
- `.capcom/issues/sudo-h-optional-argument.md`
- ADR 0049 note 0914 (`capcom know show adr-0049-note-0914`) — the shell write view
- ADR 0012 — the guard is a footgun guard, not a parser of every wrapper's grammar

**Ratified design calls:**
- **Scope:** only the two `type: bug` issues ready on 2026-11-03; the seven ready feature issues are out (owner request "the ready bugs", 2026-11-03).
- **Which views see `time`'s output file:** both write views — `writeTargetsOf` (`write-git-control-plane`) and `projectConfigTargetsOf` (`write-project-config`) — since the project-config view is documented as reading the line as `writeTargetsOf` does save three named differences (writer's call, 2026-11-03).
- **Bare `sudo -h word …`:** the strip consumes no next word, so `word` is the leader the view judges — over-reports (sudo prints help and runs nothing), fails closed (issue acceptance).

**Standing requirements:**
- skills: coding-standards
- This box: run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run <TestX> ./internal/security/` — never a package pattern, never two runs at once.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The seven ready feature issues (`context-fill-bench-arm`, `lsp-symbol-navigation`, `non-tool-advise-slot`, `pointer-shapes-on-hover`, `serve-http-driver`, `user-view-reaction-cell`, `windows-journal-remove-check`).
- `exec -a`'s short value option, and any wrapper other than `time` and `sudo`.
- bash's `time` keyword grammar (`time -p`): the view reads GNU `time`'s options for every `time` leader.

## 1. `time -o` / `--output`: report the output file and read `-o` / `-f` values — ✅ DONE (2026-11-03)

NOTES (2026-11-03): `operandTargets` is split into the strip-and-prepend shell plus `leaderOperandTargets` (the unchanged leader switch), and `projectConfigOperandTargets` drops its unused `words` parameter and calls `leaderOperandTargets`, so the project-config view emits a wrapper's output file once on every path (its install/mv/ln/default branches used to re-strip through `operandTargets`). `resolveLongOption` keeps its `=` short-circuit and delegates name matching to the new sibling `longOptionName`.

**What:**
**Goal:** `writeTargetsOf` and `projectConfigTargetsOf` report the file a `time` wrapper's `-o FILE`, `-oFILE`, `--output FILE`, `--output=FILE` or unique-prefix long form (`--out FILE`) names, and `time -o FILE cmd …` / `time -f FMT cmd …` take `cmd` as the wrapped leader.
**Approach (assumed at the header base):**
Fix for issue `time-output-file-unreported` (found after `74189cf7`, not a regression). Two causes: `wrapperValueLetters` has no `"time"` entry, so `-o` / `-f` strip bare and their value is taken as the leader; and `stripWrappers` / `stripBareOptions` drop `time`'s output option with its value without emitting it.
- Add `"time": "of"` to `wrapperValueLetters`; update the `wrapperLongOptions` doc comment, which says `time -o/-f` "stay unread".
- One walk, no second parser: the wrapper strip reports the values of the wrapper options that name a file the wrapper writes — a per-wrapper table (only `time`: short `o`, long `output`) beside `wrapperValueLetters` — as it strips them. Give `stripWrappers` (or a sibling that shares `stripBareOptions`) a way to return those values; `operandTargets` and `projectConfigCommandTargets` append them to the targets they return, including when nothing follows the wrapper (`time -o out.txt` alone). The two lenient callers (`cdTarget`'s caller at the `stripWrappers(cmd.words, lenientStrip)` site, `readsStdinScript`) ignore them.
- Value forms: next word (`-o F`, `--output F`, `--out F`), attached (`-oF` — the cluster's rest after the first value letter), and `--output=F` / `--out=F` (the `=` half). A prefix resolves through `resolveLongOption` against the existing `wrapperLongOptions["time"]` table; the output value is reported only when the option resolves to `output`.
**Regression guard.** `resolveLongOption` returns only an argument kind and short-circuits any `=` word to `(noArgument, true)` (shellwrites.go:1591-1596), so it cannot name `output`: add a sibling resolver that returns the matched long name (exact, else unique prefix, cut at `=`) for the output check, and leave `resolveLongOption`'s `=` short-circuit as is — the pinned row `env --frobnicate=x cat .git/config` → `""` (shellwrites_test.go:80) must stay green.
The wrapper's output values go BEFORE the wrapped leader's operand targets (after the redirects `writeTargetsOf` puts first), not appended after them, so `time -o out.txt rm .git/hooks/x` → `out.txt .git/hooks/x`; and `projectConfigCommandTargets`' empty-words path (shellwrites.go:110, which calls `operandTargets`) must not add them a second time.
**Files:** internal/security/shellwrites.go; internal/security/shellwrites_test.go
**Read first:** internal/security/shellwrites.go — stripWrappers, stripBareOptions, resolveLongOption, wrapperValueLetters, operandTargets, projectConfigCommandTargets, writeTargetsOf; internal/security/shellwrites_test.go — TestWriteTargetsOf
**Tests:** rows in `TestWriteTargetsOf`: `time --output=out.txt ls` → `out.txt`; `time --output out.txt rm .git/hooks/x` → `out.txt .git/hooks/x`; `time -o out.txt ls` → `out.txt`; `time -o out.txt rm .git/hooks/x` → `out.txt .git/hooks/x`; `time -oout.txt ls` → `out.txt`; `time --out out.txt ls` → `out.txt`; `time -f %e rm .git/hooks/x` → `.git/hooks/x`; `/usr/bin/time -o .git/hooks/pre-commit true` → `.git/hooks/pre-commit`. One row in `TestProjectConfigControlPlaneTargets` (or that view's table test): `time -o .apogee/config.yaml ls` reports `.apogee/config.yaml`, and `time -o .apogee/config.yaml` alone reports it exactly once. One `TestShellWriteViewRefusesOutputWrites` row: the `write-git-control-plane` rule refuses `time -o .git/hooks/pre-commit true`. Every new row fails at the base; the existing `env --frobnicate=x cat .git/config` → `""` row still passes.
**Acceptance:**
- `go build ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWriteTargetsOf|TestProjectConfigControlPlaneTargets|TestShellWriteViewRefusesOutputWrites' ./internal/security/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/security/`
**Closes:** time-output-file-unreported
**Commit:** `fix(security): report the file time -o writes and read time's -o/-f values`

## 2. `sudo -h`: an optional-argument option

**What:**
**Goal:** the wrapper strip reads sudo's `-h` as getopt `h::` does: `-hHOST` consumes only its own word, a bare `-h` consumes no next word, and `h` ends a short cluster (`-hu root rm .git/hooks/x` is host `u`, leader `root`).
**Approach (assumed at the header base):**
Fix for issue `sudo-h-optional-argument` (found after `74189cf7`, not a regression). `wrapperValueLetters["sudo"]` lists `h`, so `takesNextWordValue` takes the next word as its value. Remove `h` from that string and add a per-wrapper optional-value letter set (only `sudo`: `h`) read by `takesNextWordValue`: the cluster's first letter that is in either set decides — a required letter takes the next word only when it is the cluster's last letter (as today), an optional letter never takes the next word. `--host` stays `requiredArgument` in `wrapperLongOptions`. Update the `wrapperValueLetters` doc comment to name the optional set.
Depends on item 1 (same file).
**Files:** internal/security/shellwrites.go; internal/security/shellwrites_test.go
**Read first:** internal/security/shellwrites.go — takesNextWordValue, stripBareOptions, wrapperValueLetters, wrapperLongOptions; internal/security/shellwrites_test.go — TestWriteTargetsOf (sudo -u / -Eu / -uroot / -r / --h rows)
**Tests:** `TestWriteTargetsOf` rows: `sudo -h rm .git/hooks/x` → `.git/hooks/x`; `sudo -hhost rm .git/hooks/x` → `.git/hooks/x`; `sudo -hu rm .git/hooks/x` → `.git/hooks/x` (host `u`, the `-h` takes no next word); `sudo --host h rm .git/hooks/x` → `.git/hooks/x`; existing `sudo -u` / `sudo -r` rows still pass. The bare-`-h` row fails at the base.
**Acceptance:**
- `go build ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWriteTargetsOf' ./internal/security/`
- `GOMEMLIMIT=2GiB go test -count=1 ./internal/security/`
**Closes:** sudo-h-optional-argument
**Commit:** `fix(security): read sudo -h as an optional-argument option`
