# Plan: `golangci-lint` and `govulncheck` in `make check` and CI

**Goal:** make lint and dependency-vulnerability signal a standing gate — `make check` and CI run `golangci-lint` and `govulncheck` as one pinned command each, the tree is clean under them, and the one reachable CVE the first scan found is closed. Closes the last IDEAS.md entry (no audit host had either tool; the 2026-08-25 `dependency-surface` family went unaudited).
**Date:** 2026-09-01
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base commit:** 6e9c461b (every line number below was read at this commit; the linter's own output is the locator, never the number)

**Sources:**
- `Makefile` (`ACTIONLINT` — the pinned `go run` pattern every new tool copies), `.github/workflows/ci.yml`, `scripts/check-pins.sh`
- Baseline at base commit, uncapped (`--max-issues-per-linter 0 --max-same-issues 0`): `golangci-lint v2.13.2` default set → 179 findings (120 errcheck, 45 staticcheck of which 23 are QF*, 11 ineffassign, 3 unused) = 156 non-QF sites; `govulncheck v1.7.0` → GO-2026-5970 reachable via `internal/security/urlsafety.go:259` (`golang.org/x/text v0.37.0`, fixed in v0.39.0)
- `docs/manual/building.md` §Makefile table + "what `make check` covers"; `docs/design/test-drivers.md` gate tables (~:781, ~:853)

**Ratified design calls** (owner, 2026-09-01, via AskUserQuestion):
- **Lint baseline:** fix every finding; no `--new-from-rev` baseline, no per-class exclusion (unchecked `Close` included).
- **QF checks:** the staticcheck QF* quickfix group is disabled in `.golangci.yml`; every other default check stays on.
- **Tool delivery:** `go run <module>@<pinned version>` from the Makefile, one version literal each; CI calls the make targets. No third-party action.
- **Offline `govulncheck`:** hard fail — `make check` fails with the tool's own error when the vuln DB is unreachable.
- **Windows-only symbols** (writer, keeps behaviour): `unused` findings on symbols referenced only from `_windows.go` files get `//nolint:unused // used by the windows build (<file>)`, never deletion and never a second `GOOS=windows` lint pass (it would mirror the problem onto Linux-only symbols).
- **Announced error strings** (writer, keeps announced wording): the three ST1005 sites end with a key's own colon or a paragraph break by design; they keep their wording and get `//nolint:staticcheck // ST1005: <reason>`.
- **Discarded test models** (writer): an ineffectual `m = …` in a test is deleted, no assertion invented — in every site the assertions that follow read another value.
- **Unchecked status prints** (writer, keeps output): the `fmt.Fprint*` errcheck sites in `cmd/apogee` write status lines to the command's injected output writer; they become `_, _ = fmt.Fprintln(...)` — no errcheck exclusion, consistent with "fix every finding". `os.Setenv` in a `TestMain` (`cmd/apogee/main_test.go:60,61`) becomes `_ = os.Setenv(...)` (t.Setenv is unavailable there).

**Regression check (2026-09-01, 6e9c461b):** two independent reviewers at base 6e9c461b. Both found the draft's counts and site lists came from golangci-lint's default-capped output (max-issues-per-linter 50, max-same-issues 3); uncapped, the base tree has 156 non-QF findings, 17 of them in packages the draft covered nowhere. The writer confirmed the numbers, corrected the baseline, re-partitioned the lint-fix items and renumbered the plan to 13 items (old 1→1, 2→2, 3→3, 4→4+5, 5→6, 6→7+9, 7→8+10+11, 8→12, 9→13); the ids below are the NEW numbers.
- 2: recast (uncapped `issues:` caps in `.golangci.yml`, counts 156 / 0, `-QF*` wildcard binding) — guards folded (`make` exits 2 on a failed recipe ⇒ `test $? -ne 0`; `help` prints ANSI colour ⇒ strip before grepping).
- 3: recast (six capped-away `safeio` sites, `safeio_test.go` added) — guard folded (landlock :302/:374 are deferred closes with the fd used after — deferred idiom, never a bare `_ = unix.Close`).
- 4: recast (config only; five ST1005 sites, conversions are package-`config` types) — guards folded (rule not list for ST1005; no `domain.PromptSource`).
- 5: recast (new item: provider, domain, judge, library, keystore) — guard folded (keystore `recordInvocation` has no `t`).
- 6: recast (capped-away present/skills sites added).
- 7: recast (tools only; 17 sites, five ST1018 literals).
- 8: recast (tui only; capped-away test sites, `filecache.go:90`, tui ST1018 fixtures) — guards folded (`settings_test.go:3271` rewrite; `firstVisibleLine` comment at model_test.go:5340).
- 9: recast (new item: agent, mcp, stubllm, mechanisms — the packages no draft item covered).
- 10: recast (new item: `cmd/apogee` production prints under the "unchecked status prints" call).
- 11: recast (new item: root tests, cmd tests, cmd/stubllm, tuitest).
- 12: guard folded (`make -n check | grep -cE 'lint|vulncheck'` is already 3 at base — match the two recipe lines exactly); depends on 3–11.
- 13: depends on 12 (renumbering only).
Round 2 (2026-09-01, 6e9c461b): two independent reviewers re-read the renumbered items 2–11 against the uncapped base; every site list and count matched the linter byte-for-byte, items 3, 5–10 SAFE.
- 2: decision applied — the acceptance no longer asserts the literal count; `make lint` output must EQUAL an explicit uncapped run (the literal 156 is a parenthetical, pinned to base 6e9c461b, as is every "at base: N" count in items 3–11 — the binding scope of each fix item is "fix every finding the linter reports").
- 4: guard folded (`configsplice.go:213` is a `defer os.Remove` with the temp file used after — deferred idiom, never a bare `_ = os.Remove`; item 6's error-path removes stay bare).
- 11: guard folded (the acceptance lints `cmd/apogee` production files too — depends on items 2 and 10).

**Standing requirements:**
- skills: coding-standards
- Idiom for a discarded close: `defer func() { _ = h.Close() }()` for a deferred read-only handle; `_ = h.Close()` on an error path that already returns an error; a writer's success path returns or joins the `Close` error. Never `//nolint:errcheck`.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Linters beyond golangci-lint's `standard` set (gosec, revive, …); a `nolintlint` gate.
- Fixing the QF* suggestions; a `GOOS=windows` lint pass; running `govulncheck` in `make live-eval` or `make dist`.
- Version bump (see closing note); editing the code-audit skill (outside the repo).

## 1. Close GO-2026-5970: bump `golang.org/x/text` to v0.39.0 — ✅ DONE (2026-09-02)

NOTES (2026-09-02): `govulncheck -show verbose` still lists one required-but-uncalled module vulnerability — GO-2026-5942 in `golang.org/x/net@v0.55.0` (fixed in v0.56.0), an indirect dependency. It does not fail the gate (scan exits 0, "No vulnerabilities found" in the symbol results) and bumping it is not this item.
NOTES (2026-09-02): `go mod tidy` also refreshed the `go.sum` hash lines for `golang.org/x/tools` v0.44.0 → v0.47.0. This is a graph-only consequence of the bump (`golang.org/x/text@v0.39.0` requires `golang.org/x/tools@v0.47.0`); `x/tools` is not listed in `go.mod` and no build input changed. Only the two prescribed commands were run.

**What:** fix — `govulncheck` at base reports GO-2026-5970 (infinite loop on invalid input in `golang.org/x/text`) reachable from `security.normalizeHostName` → `idna.Profile.ToASCII` (`internal/security/urlsafety.go:259`). Run `go get golang.org/x/text@v0.39.0 && go mod tidy`; touch nothing else. If `-show verbose` still lists a required-but-uncalled module vulnerability, name it in a NOTES line (it does not fail the gate; bumping it is not this item).

**Files:** `go.mod`, `go.sum`

**Tests:** existing `internal/security` suite (urlsafety tests exercise IDNA normalisation).

**Acceptance:**
```
go build ./... && go test -race -count=1 ./internal/security/...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...      # exit 0, "No vulnerabilities found" or informational only
grep -n 'golang.org/x/text v0.39.0' go.mod
```

**Commit:** `fix(deps): bump golang.org/x/text to v0.39.0 (GO-2026-5970)`

## 2. `.golangci.yml` and the `make lint` / `make vulncheck` targets — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the plan's parenthetical "156 at base 6e9c461b" now reads 157 — one errcheck site (`internal/stubllm/server_test.go:834`, `resp.Body.Close`) landed after the base commit. The binding acceptance holds: `make lint`'s count EQUALS the explicit uncapped run (diff produced no output), so both `issues:` caps are lifted. Item 9 owns the extra site under its "fix every finding the linter reports for these packages" scope, which makes its "18 at base" read 19.
NOTES (2026-09-02): no CHANGELOG entry in this sidecar — item 13's text assigns the single `[Unreleased] → Added` entry for the two gates, the `.golangci.yml` and the x/text bump to item 13's sidecar.

Depends on item 1 (`make vulncheck` must exit 0 here).

**What:** Recast at the regression check (2026-09-01). add `.golangci.yml` at the repo root (`version: "2"`; `linters.default: standard`; `linters.settings.staticcheck.checks: ["all", "-ST1000", "-ST1003", "-ST1016", "-ST1020", "-ST1021", "-ST1022", "-QF*"]` — the first six are golangci-lint v2's own defaults, restated because setting `checks` replaces them; the `-QF*` wildcard is binding, v2.13.2 accepts it; `issues.max-issues-per-linter: 0` and `issues.max-same-issues: 0` so `make lint` never hides a finding). In `Makefile`, beside `ACTIONLINT`: `GOLANGCI_LINT_VERSION := v2.13.2`, `GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)`, `GOVULNCHECK_VERSION := v1.7.0`, `GOVULNCHECK = go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)`, each with a comment in the `ACTIONLINT` comment's shape (pinned by module version, CI calls the target, cached after first run; the vulncheck comment states the network need and that offline is a failure by design). Two new `## `-documented `.PHONY` targets: `lint: $(GOLANGCI_LINT) run ./...` and `vulncheck: $(GOVULNCHECK) ./...`. Do NOT add them to `check` yet (item 12) — `lint` is red until items 3–11 land.

**Regression guard.** golangci-lint's defaults cap output (max-issues-per-linter 50, max-same-issues 3) — the draft's 87 / 78 were the cap, not the tree; the two `issues:` keys lift it and the counts below are the uncapped base (156 non-QF). A failed recipe makes `make` exit 2 (`make: *** [lint] Error 1`), never 1 — the acceptance tests `-ne 0`. `help` (Makefile:89) prints `\033[36m` between the indent and the target name — strip ANSI before grepping. The finding count is never a literal in the acceptance: `make lint` output must EQUAL an explicit uncapped run of the same linter, which proves the config lifts both caps and cannot go stale as unrelated commits land (156 at base 6e9c461b).

**Files:** `.golangci.yml`, `Makefile`

**Tests:** none (build tooling); acceptance is the observable output.

**Acceptance:**
```
make vulncheck                                                            # exit 0
make help | sed 's/\x1b\[[0-9;]*m//g' | grep -E '^\s+(lint|vulncheck)\s'  # both listed
make lint > /tmp/lint.txt 2>&1; test $? -ne 0                             # red by design until item 11
grep -cE '\.go:[0-9]+:[0-9]+:' /tmp/lint.txt > /tmp/lint-count.txt         # (156 at base 6e9c461b)
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --max-issues-per-linter 0 --max-same-issues 0 ./... 2>&1 | grep -cE '\.go:[0-9]+:[0-9]+:' | diff - /tmp/lint-count.txt   # no output: `make lint` count EQUALS the explicit uncapped count (the config lifts both caps)
grep -c 'QF1' /tmp/lint.txt                                               # 0
```

**Commit:** `build: pinned golangci-lint and govulncheck targets with a v2 config`

## 3. Lint clean: `internal/security`, `internal/platform` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the uncapped run reported exactly the 26 sites the item lists, in the 8 listed files — no extra site, no site the item named that the linter did not report. All 26 are now clean (`0 issues.`, exit 0).
NOTES (2026-09-02): `winlabel/journal_state_test.go:54` — the ineffectual `got = append(got, …)` was removed as a WHOLE statement; dropping only the `got =` would leave a bare `append(...)` expression that does not compile. The `got[0].Path` mutation above it still carries the "a caller mutated its copy" premise the surviving assertion reads.
NOTES (2026-09-02): `confinetest.go:86/92` — the two success subtests now assert the `runWriteProbe` error with `t.Fatalf`, matching the sibling subtests' style; no assertion was invented anywhere else.
NOTES (2026-09-02): no CHANGELOG entry in this sidecar — item 13's text assigns the single `[Unreleased]` entry for the whole plan.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding `$(GOLANGCI_LINT) run ./internal/security/... ./internal/platform/...` reports (at base, uncapped: 26 sites in 8 files). `safeio.go` — eight deferred read-handle closes (:124, :352, :380, :434, :439, :453, :569, :614) take the standing discard idiom; six error-path closes (:315, :320, :480, :484, :524, :528) become `_ = f.Close()` (the error already returned wins; the success paths already `return f.Close()`). `safeio_test.go:558,623` — test fixtures (`defer f.Close()`), discard idiom. `landlock_linux.go` :302/:374 — the deferred idiom, see the guard. `lock.go:97` per its path (error path ⇒ discard). `confinetest/confinetest.go` :86/:92 — the `runWriteProbe` error is asserted (`t.Fatalf`) in the success subtests; :241 `ln.Close`, :403 `conn.Close` discard. `host.go:46` `finalPath` (set in `platform_windows.go:27`, read in `confiner_windows.go:375`) and `winlabel/session.go:113` `unwind` (called from `walk_windows.go:87`) take the ratified `//nolint:unused` line. `winlabel/journal_state_test.go:54` — delete the ineffectual assignment to `got`.

**Regression guard.** `landlock_linux.go:302` and `:374` are `defer unix.Close(fd)` / `defer unix.Close(rootFD)` and the descriptors are used after (:312 `allowWriteBeneath(fd, …)`, :378 `Parent_fd: rootFD`); a bare `_ = unix.Close(fd)` would close the ruleset fd before any rule is added and every confined exec on Linux would fail with EBADF. Spell both as `defer func() { _ = unix.Close(fd) }()` (and `rootFD`), the same as the safeio deferred closes. The site list is every errcheck/unused/ineffassign line the uncapped run (`--max-issues-per-linter 0 --max-same-issues 0`) reports for the two trees — the linter's output is the locator.

**Files:** `internal/security/safeio.go`, `internal/security/safeio_test.go`, `internal/platform/confinetest/confinetest.go`, `internal/platform/landlock_linux.go`, `internal/platform/lock.go`, `internal/platform/host.go`, `internal/platform/winlabel/session.go`, `internal/platform/winlabel/journal_state_test.go`

**Tests:** existing suites; the Windows-tagged files must still vet.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/security/... ./internal/platform/...   # exit 0
go test -race -count=1 ./internal/security/... ./internal/platform/...
GOOS=windows go vet ./internal/platform/...
```

**Commit:** `refactor(security,platform): lint-clean under golangci-lint standard set`

## 4. Lint clean: `internal/config` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): all 11 findings the uncapped linter reports for the package are fixed; the five ST1005 sites the run reported are exactly the five the item names (config.go response-reserve, configmigrate.go server:/llama-launcher:/model-profile:, keyresolve.go api-key-env:), each keeping its wording behind a reasoned nolint line. Line numbers moved since the plan's base commit (config.go:1673→1706, :1924/:1930→1957/:1963, registry.go:658→676); the linter's output was the locator, per the plan header.
NOTES (2026-09-02): `configsplice.go` `defer os.Remove(name)` became `defer func() { _ = os.Remove(name) }()` with its trailing comment kept inline, so the temp file is still unlinked only after the rename; `registry.go`'s read-only `defer f.Close()` took the same deferred idiom, and the two error-path closes (`configmigrate.go` backup write, `configsplice.go` temp write) became bare `_ = …Close()`.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for this package (at base, uncapped: 11 sites in 5 files). ST1005 at `config.go:1673` (ends with `response-reserve:`), `configmigrate.go:228` (ends with `server:`), `:469` (paragraph break), `:581` (the retired model-profile refusal) and `keyresolve.go:266` (ends with the `api-key-cmd:` and `api-key-env:` key spellings by design) take the ratified `//nolint:staticcheck // ST1005: <reason>` line — all five are deliberate announced wording (`:469` pinned by `configmigrate_test.go:375,430`); no wording changes. S1016 at `config.go:1924,1930` become the conversions the linter names (`PromptSource(e)`, `SystemPromptLayer(l)`). `configmigrate.go:378` `f.Close` per path; `configsplice.go:213` `defer func() { _ = os.Remove(name) }()` (the deferred idiom — see the guard), `:216` `tmp.Close` per path; `registry.go:658` `f.Close` per path.

**Regression guard.** The "announced error strings" call applies to every ST1005 the linter reports in this package (five at base, not the draft's three) — the rule is `grep ST1005` of the run, never a closed list; each keeps its wording and takes the nolint line with its reason. `PromptSource` and `SystemPromptLayer` live in package `config` (config.go:86, :100); `internal/domain` has no such types, so the conversions are unqualified. `configsplice.go:213` is `defer os.Remove(name)` and the temp file is used after it (`tmp.Write` :215, `os.Chmod(name, …)` :222) — dropping the `defer` unlinks it first and every atomic config rewrite fails ENOENT; spell the site as `defer func() { _ = os.Remove(name) }()`, as item 3 does for landlock. The bare error-path `os.Remove`/`os.RemoveAll` sites in item 6 (`scheme/store.go:157`, `skills/export.go:96`) stay `_ = …`.

**Files:** `internal/config/config.go`, `internal/config/configmigrate.go`, `internal/config/configsplice.go`, `internal/config/keyresolve.go`, `internal/config/registry.go`

**Tests:** existing suites; `configmigrate_test.go` and the keyresolve tests pin the strings and stay green unchanged; the `writeConfigAtomically` callers' tests (`configedit_test.go`, `configwrite_scalar_test.go`) stay green — they fail ENOENT if the `defer` is dropped.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/config/...   # exit 0
go test -race -count=1 ./internal/config/...
```

**Commit:** `refactor(config): lint-clean under golangci-lint standard set`

## 5. Lint clean: `internal/provider`, `domain`, `judge`, `library`, `keystore` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): `keystore_test.go:255` — the item's guard spells the fix `_ = fmt.Fprintln(file, string(line))`, which does not compile (`Fprintln` returns `(int, error)`); written as `_, _ = fmt.Fprintln(...)`, matching the plan's ratified "unchecked status prints" call.
NOTES (2026-09-02): `domain/config_test.go:21` — SA4006 resolved by DROPPING `got = got[:1]` per the ratified "discarded test models" call; the assertions that follow read `again`/`next`, and re-slicing a local slice header can never reach `ModeLadder`'s source, so no coverage is lost.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for these packages (at base, uncapped: 14 sites in 8 files). `provider/client.go:340` `resp.Body.Close` (discard idiom), `:586` S1016 — the conversion the linter names (`chatToolFunction(t)`, `provider/wirejson.go:87`); `provider/discovery.go:248,290` and `provider/stream.go:84` `resp.Body.Close` — discard idiom; at `stream.go:84` read the path: a body close on the error path of a stream setup is `_ = resp.Body.Close()`, a deferred one takes the deferred idiom. `domain/config_test.go:21` SA4006 (use or drop `got`). `judge.go:101,184` `client.Close` per path; `library/fingerprint.go:150` deferred read close; `library/store_test.go:421` discard idiom; `keystore_test.go:122,129,254,255` (`fmt.Fprintln` into a fixture file — check with `t.Fatalf` where a `t` is in scope; see the guard).

**Regression guard.** `keystore_test.go:255` (`fmt.Fprintln`) and `:254` (`defer file.Close()`) sit in `recordInvocation(argv []string, stdin string)` (:241) — no `*testing.T` in scope, so "check with `t.Fatalf`" cannot compile there: write `_ = fmt.Fprintln(file, string(line))` (a fake-subprocess log line) and the deferred discard idiom, or have `recordInvocation` return the error to its fake main. `:122`/`:129` sit in a helper that already returns `error` — the standing idiom per path.

**Files:** `internal/provider/client.go`, `internal/provider/discovery.go`, `internal/provider/stream.go`, `internal/domain/config_test.go`, `internal/judge/judge.go`, `internal/library/fingerprint.go`, `internal/library/store_test.go`, `internal/keystore/keystore_test.go`

**Tests:** existing suites.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/provider/... ./internal/domain/... ./internal/judge/... ./internal/library/... ./internal/keystore/...   # exit 0
go test -race -count=1 ./internal/provider/... ./internal/domain/... ./internal/judge/... ./internal/library/... ./internal/keystore/...
```

**Commit:** `refactor(provider,judge,library,keystore): lint-clean under golangci-lint standard set`

## 6. Lint clean: `internal/present`, `internal/scheme`, `internal/skills` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): all 18 sites the linter reported for these packages are exactly the 18 the item enumerates; no extra sites, no site skipped. Both best-effort cleanup removes (`scheme/store.go:157`, `skills/export.go:96`) already carried the one-line comment the item asks for, so none was added.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for these packages (at base, uncapped: 18 sites in 6 files). `present/server.go` :185/:189/:230/:361 `doc.Close` — read the handle's role: an opened document served to a client is read-only ⇒ discard idiom; a handle written to ⇒ its success path returns the error. `present/detect.go:169` `conn.Close` discard. `scheme/store.go:157` `_ = os.Remove`, `skills/export.go:96` `_ = os.RemoveAll` (best-effort cleanup — keep a one-line comment saying so where none exists). `skills/load.go:278,428` `root.Close`/`base.Close` (`os.Root` handles ⇒ discard idiom), `:484` `f.Close` per path. Tests: `present/server_test.go` :64/:214/:476/:738/:933/:958 `resp.Body.Close`, :673 `extra.Close`, :865 `occupied.Close` — discard idiom.

**Files:** `internal/present/server.go`, `internal/present/detect.go`, `internal/present/server_test.go`, `internal/scheme/store.go`, `internal/skills/export.go`, `internal/skills/load.go`

**Tests:** existing suites.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/present/... ./internal/scheme/... ./internal/skills/...   # exit 0
go test -race -count=1 ./internal/present/... ./internal/scheme/... ./internal/skills/...
```

**Commit:** `refactor(present,scheme,skills): lint-clean under golangci-lint standard set`

## 7. Lint clean: `internal/tools` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the five ST1018 fixture lines were rewritten as `\u202e` / `\u200b` escapes with the literals' bytes unchanged; trailing comments name the characters on the three sites (network_test.go:367,368,384) whose surrounding prose does not — the pair at :241,:242 is already named by the comment two lines above. `gofmt -w` realigned the :367/:368 trailing-comment column; both lines were already changed, so `git diff --stat -- internal/tools/network_test.go` stays at exactly the five flagged lines (5 insertions, 5 deletions).

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for this package (at base, uncapped: 17 sites in 10 files). `tools/find_files.go:186`, `tools/grep.go:220,432`, `tools/list_dir.go:95,203`, `tools/path_read.go:42`, `tools/path_read_test.go:456`, `tools/path_safety.go:194`, `tools/path_suggest.go:49`, `tools/path_virtual.go:142,152` — every one a read handle (file or directory) close per the standing idiom; `tools/network.go:260` `resp.Body.Close` — discard idiom per path. ST1018 at `tools/network_test.go:241,242,367,368,384` — five literals: rewrite the invisible Unicode format characters as the escapes the linter names (`\u202e` for U+202E RIGHT-TO-LEFT OVERRIDE, `\u200b` for U+200B ZERO WIDTH SPACE, and so on) so the literal's BYTES are unchanged — these are the bidi/zero-width fixtures the url-safety guard is tested against, and the test must keep feeding the exact same input. Add a trailing comment naming the character where the escape is not self-explanatory.

**Files:** `internal/tools/find_files.go`, `internal/tools/grep.go`, `internal/tools/list_dir.go`, `internal/tools/network.go`, `internal/tools/path_read.go`, `internal/tools/path_read_test.go`, `internal/tools/path_safety.go`, `internal/tools/path_suggest.go`, `internal/tools/path_virtual.go`, `internal/tools/network_test.go`

**Tests:** existing suites — the rewritten fixtures must pass with no assertion changed.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/tools/...   # exit 0
go test -race -count=1 ./internal/tools/...
git diff --stat -- internal/tools/network_test.go   # only the five flagged literal lines change
```

**Commit:** `refactor(tools): lint-clean under golangci-lint standard set`

## 8. Lint clean: `internal/tui` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the SA4006 site the item text located at `picker_test.go:2845` is at `:2904` in the tree — per the plan header the linter's output is the locator, never the number; all 18 reported sites were fixed and the count matches the item's "18 sites in 10 files".

NOTES (2026-09-02): deleting `firstVisibleLine` left `charm.land/bubbles/v2/viewport` imported and unused in `model_test.go` (it was that helper's only package reference — the file's other `viewport` hits are `m.viewport` field accesses and two comment mentions); the import is removed in the same edit, without which the package does not compile. `gofmt` realigned the trailing-comment column on `settings_test.go:1176`, a line already changed, so no unflagged line moved.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for this package (at base, uncapped: 18 sites in 10 files). ineffassign/SA4006 discarded-model sites — the ratified rule (delete the ineffectual assignment: `m, _ = stepCmd(...)` ⇒ `stepCmd(...)` discarding both, or drop the trailing `m = next`; invent no assertion) at `tui/autotitle_test.go:781`, `model_test.go:2591,2606`, `picker_test.go:525,2013,2845`, `runview_test.go:739`, `schedule_test.go:360`, `sessions_test.go:253,1245`, `settings_test.go:1176,1448,3271,3272` (for :3271–3272 see the guard). `model_test.go:4714` `firstVisibleLine` — delete the dead helper (and its :5340 mention, see the guard). `tui/filecache.go:90` `r.Close` (production `os.Root` handle — discard idiom per path). ST1018 at `tui/reasoning_test.go:47` and `tui/toolpresent_test.go:2775` — item 7's rule: rewrite the invisible characters as the escapes the linter names so the literal's BYTES are unchanged; a trailing comment names the character where the escape is not self-explanatory.

**Regression guard.** `settings_test.go:3271–3272` as "drop the trailing `m = next`" alone leaves `next, cmd = stepCmd(...)` assigning a `next` nothing reads — SA4006 still fires; rewrite the pair as `_, cmd = stepCmd(t, m, configChangedMsg{alive: false})` and delete `m = next`. Deleting `firstVisibleLine` leaves the comment at `model_test.go:5340` ("so firstVisibleLine (viewport-only) cannot see it") naming a helper that no longer exists — reword it ("the viewport alone cannot see it") in the same edit.

**Files:** `internal/tui/autotitle_test.go`, `internal/tui/model_test.go`, `internal/tui/picker_test.go`, `internal/tui/runview_test.go`, `internal/tui/schedule_test.go`, `internal/tui/sessions_test.go`, `internal/tui/settings_test.go`, `internal/tui/filecache.go`, `internal/tui/reasoning_test.go`, `internal/tui/toolpresent_test.go`

**Tests:** the touched tests themselves; the two rewritten fixtures pass with no assertion changed.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/tui/...   # exit 0
go test -race -count=1 ./internal/tui/...
git diff --stat -- internal/tui/reasoning_test.go internal/tui/toolpresent_test.go   # only the flagged literal lines change
```

**Commit:** `test(tui): lint-clean under golangci-lint standard set`

## 9. Lint clean: `internal/agent`, `internal/mcp`, `internal/stubllm`, `internal/mechanisms` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's site list named 18 findings; the uncapped linter reports 19 for these packages — `internal/stubllm/server_test.go:834` (`defer resp.Body.Close()`) is a capped-away sibling of the four listed `server_test.go` sites and was folded in under the enumeration-is-a-floor rule and the header's "fix every finding the linter reports" scope. It is in a file the item already lists, so the FILES list is unchanged.

NOTES (2026-09-02): the four non-deferred `mcp/transport_test.go` sites (:200, :221, :241, :309) are error paths that `t.Fatal` immediately after; they took the bare `_ = resp.Body.Close()` form per the standing idiom, the other fourteen took `defer func() { _ = h.Close() }()`.

NOTES (2026-09-02): `filehint_test.go:240` carries a trailing `// U+202E RIGHT-TO-LEFT OVERRIDE` comment naming the character, matching item 7's convention, because no surrounding prose names it; `git diff --stat` on that file is exactly 1 insertion / 1 deletion.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for these packages (at base, uncapped: 18 sites in 9 files — none of them covered by the draft). `agent/contextfiles.go:105` and `agent/loop.go:1156` `f.Close` (production read handles — the standing idiom per path); `agent/library_corrupt_store_test.go:34`, `agent/subagent_test.go:1456` `child.Close` — discard idiom. `mcp/transport_test.go:182,200,221,241,297,309,474` `resp.Body.Close` — discard idiom. `stubllm/record_test.go:300`, `stubllm/server_test.go:351,367,584,616` `resp.Body.Close` — discard idiom. `mechanisms/filehint_test.go:240` ST1018 — item 7's rule: rewrite the invisible character as the escape the linter names so the literal's BYTES are unchanged (the file-hint guard's fixture keeps feeding the exact same input; a trailing comment names the character). `mechanisms/retired_test.go:238` `r.Close` — discard idiom.

**Files:** `internal/agent/contextfiles.go`, `internal/agent/loop.go`, `internal/agent/library_corrupt_store_test.go`, `internal/agent/subagent_test.go`, `internal/mcp/transport_test.go`, `internal/stubllm/record_test.go`, `internal/stubllm/server_test.go`, `internal/mechanisms/filehint_test.go`, `internal/mechanisms/retired_test.go`

**Tests:** existing suites — the rewritten fixture must pass with no assertion changed.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./internal/agent/... ./internal/mcp/... ./internal/stubllm/... ./internal/mechanisms/...   # exit 0
go test -race -count=1 ./internal/agent/... ./internal/mcp/... ./internal/stubllm/... ./internal/mechanisms/...
git diff --stat -- internal/mechanisms/filehint_test.go   # only the flagged literal line changes
```

**Commit:** `refactor(agent,mcp,stubllm,mechanisms): lint-clean under golangci-lint standard set`

## 10. Lint clean: `cmd/apogee` production files — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the coding-standards Go extension says "never discard with `_`"; the plan's ratified "unchecked status prints" call overrides it for these fifteen status writes to injected output writers (`_, _ =`, no errcheck exclusion) — followed the plan.

Depends on item 2.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports in the non-test files of `cmd/apogee` (at base, uncapped: 15 sites in 8 files). `cmd/apogee/daemon.go:171,221,243,620`; `cmd/apogee/daemoninstall.go:201,217,218,220,221`; `cmd/apogee/headless.go:444`; `cmd/apogee/keymigrate.go:98`; `cmd/apogee/probe.go:140`; `cmd/apogee/probemodel.go:169`; `cmd/apogee/probeterminal.go:83`; `cmd/apogee/wire.go:148` — all `fmt.Fprint*` status writes to the command's injected output writer: the ratified "unchecked status prints" call, `_, _ = fmt.Fprintln(...)` (no errcheck exclusion). Announced output text does not change.

**Files:** `cmd/apogee/daemon.go`, `cmd/apogee/daemoninstall.go`, `cmd/apogee/headless.go`, `cmd/apogee/keymigrate.go`, `cmd/apogee/probe.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/probeterminal.go`, `cmd/apogee/wire.go`

**Tests:** the headless/daemon/probe tests that pin that output stay green unchanged.

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./cmd/apogee/... 2>&1 | grep -vE '_test\.go:' | grep -cE '\.go:[0-9]+:'   # 0 (the package is fully green only after item 11)
go build ./cmd/apogee && go test -race -count=1 -run 'TestHeadless|TestDaemon|TestProbe' ./cmd/apogee/...
```

**Commit:** `refactor(cmd): lint-clean under golangci-lint standard set`

## 11. Lint clean: root tests, `cmd/apogee` tests, `cmd/stubllm`, `internal/tuitest` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): all 19 uncapped findings the item lists were the 19 the linter reported at this tree; no site was added or dropped.

Depends on items 2 and 10.

**What:** Recast at the regression check (2026-09-01). fix every finding the linter reports for these trees (at base, uncapped: 19 sites in 12 files, all in tests). Root `apogee_test.go:146`, `benchreadiness_test.go:374,396,523,586,663`, `example_test.go:222` — `agent.Close`/arm closes: discard idiom (in `example_test.go` keep the example readable: `defer func() { _ = ag.Close() }()`). `cmd/apogee/e2e_newcomer_test.go:219` discard idiom; `cmd/apogee/keysource_test.go:50` `_ = os.RemoveAll`; `cmd/apogee/main_test.go:60,61` `_ = os.Setenv(...)` (the ratified call — a `TestMain`, no `t`, never `t.Setenv`), `:71` `_ = os.RemoveAll`; `cmd/apogee/probeterminal_test.go:25,30`, `cmd/apogee/readme_test.go:23`, `cmd/apogee/wire_helpers_test.go:42` — discard idiom; `cmd/apogee/schedule_test.go:1199` S1040 — drop the same-type assertion. `cmd/stubllm/main_test.go:124` discard idiom. `internal/tuitest/driver_test.go:243` `Screen.Write` — check the error (`t.Fatalf`).

**Regression guard.** The acceptance lints `./cmd/...`, which includes `cmd/apogee`'s production files — the 15 sites item 10 owns (`daemon.go:171` … `wire.go:148`). Run before item 10 it exits 1 on those sites, so this item depends on items 2 AND 10; the acceptance is the full-package exit 0, not a `_test.go` filter.

**Files:** `apogee_test.go`, `benchreadiness_test.go`, `example_test.go`, `cmd/apogee/e2e_newcomer_test.go`, `cmd/apogee/keysource_test.go`, `cmd/apogee/main_test.go`, `cmd/apogee/probeterminal_test.go`, `cmd/apogee/readme_test.go`, `cmd/apogee/schedule_test.go`, `cmd/apogee/wire_helpers_test.go`, `cmd/stubllm/main_test.go`, `internal/tuitest/driver_test.go`

**Tests:** the touched tests themselves; `go vet` still accepts `example_test.go` (an Example's `// Output:` block is unchanged).

**Acceptance:**
```
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run . ./cmd/... ./internal/tuitest/...   # exit 0
go test -race -count=1 . ./cmd/... ./internal/tuitest/...
```

**Commit:** `test(cmd,tuitest): lint-clean under golangci-lint standard set`

## 12. Gate: `make check` and CI run `lint` and `vulncheck` — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the two new CI jobs are placed between `check` and `cross` in `ci.yml`; job order is presentational only (they run in parallel).

Depends on items 3, 4, 5, 6, 7, 8, 9, 10, 11.

**What:** `Makefile` `check` gains `@echo "==> golangci-lint"` + `@$(MAKE) --no-print-directory lint` right after the two `go vet` steps, and `@echo "==> govulncheck"` + `@$(MAKE) --no-print-directory vulncheck` right after `go build ./...`; update the `## check:` help line to list both. `.github/workflows/ci.yml` gains two jobs beside `check`, `cross` and `test-windows` — `lint` (`name: golangci-lint`, step `run: make lint`) and `vulncheck` (`name: govulncheck`, step `run: make vulncheck`) — each with the same SHA-pinned `actions/checkout` (`persist-credentials: false`) and `actions/setup-go` (`go-version: '1.26.x'`) steps the `check` job uses, and a comment in the actionlint step's shape (why a make target, why no third-party action). Separate jobs, not steps of `check`: they run in parallel and a lint failure is named by its own job. Update ci.yml's header comment to mention the two gates.

**Regression guard.** `make -n check | grep -cE 'lint|vulncheck'` is already 3 at base (the `actionlint` echo and target lines match `lint`), so it cannot fail against the pre-item tree — the acceptance matches the two new recipe lines exactly: `make -n check | grep -cE -- '--no-print-directory (lint|vulncheck)$'` = 2 (0 at base). `make check` grows by roughly the linter's link time plus one vuln-DB fetch; the recipe order keeps the cheap gates (gofmt, vet, lint) before the race suite so a lint failure surfaces in seconds.

**Files:** `Makefile`, `.github/workflows/ci.yml`

**Tests:** none (build tooling); the workflow gates below.

**Acceptance:**
```
make lint && make vulncheck                                                   # both exit 0
make -n check | grep -cE -- '--no-print-directory (lint|vulncheck)$'          # 2
./scripts/check-pins.sh && make actionlint                                    # the new jobs' pins and syntax
grep -nE '^\s+(lint|vulncheck):' .github/workflows/ci.yml                     # two jobs
```

**Commit:** `build(ci): run golangci-lint and govulncheck in make check and CI`

## 13. Docs: the manual and the test-drivers gate tables name the two gates

Depends on item 12.

**What:** `docs/manual/building.md` — the `make check` table row lists `golangci-lint` and `govulncheck` among its gates; add `make lint` and `make vulncheck` rows; the "what `make check` covers" paragraph gains one sentence each (lint = golangci-lint's standard set under `.golangci.yml`, pinned by module version like actionlint; vulncheck = `govulncheck` against the Go vulnerability DB, needs the network and fails offline by design). `docs/design/test-drivers.md` — the "workflow gates" row (~:853) and the T-21 row (~:781) name the two new gates alongside `check-pins.sh`/`actionlint`, or a new row "lint and dependency vulnerabilities" is added to the same table (keep the table's column shape). `CHANGELOG.md` entry travels in the sidecar: `[Unreleased] → Added`: the two gates, the `.golangci.yml`, the x/text bump (GO-2026-5970).

**Files:** `docs/manual/building.md`, `docs/design/test-drivers.md`

**Tests:** `TestManualListsEveryEnvironmentOverride` and the docs-env tests must stay green (no env variable is introduced).

**Acceptance:**
```
grep -n 'golangci-lint' docs/manual/building.md docs/design/test-drivers.md   # ≥ 1 hit in each
grep -n 'govulncheck' docs/manual/building.md docs/design/test-drivers.md     # ≥ 1 hit in each
go test -race -count=1 -run 'TestManual|TestDocsEnv' ./cmd/apogee/...
```

**Commit:** `docs(manual,design): name the lint and vulncheck gates`

---

**Suggested version bump:** patch (`v0.19.11`) — a dependency CVE fix plus new standing gates; no user-facing behaviour changes. The owner decides.
