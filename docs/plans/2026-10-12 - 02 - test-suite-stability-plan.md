# test-suite stability, speed and parity — plan

**Goal:** retire the recurring flake classes structurally: one scalable test-time discipline enforced by a guard, readiness signals in place of quiet-as-ready, the Windows ConPTY flake and its load amplifier gone, line endings pinned repo-wide. Then cut suite wall time where it is measured to go: the tui sweep critical path, per-chunk e2e builds, idle snapshot work and typed prompts.
**Date:** 2026-10-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7f22d7df
**Regression check (2026-10-12, 7f22d7df):**
- 2: guard folded
- 3: guard folded
- 4: guard folded
- 5: guard folded
- 6: recast
- 8: recast
- 9: recast
- 10: guard folded
- 11: guard folded
- 12: guard folded
- 13: guard folded
- 14: guard folded
- 15: guard folded
- 16: guard folded
- 17: guard folded
- 18: guard folded
- 19: guard folded
- 20: guard folded
- 21: guard folded
- 22: guard folded
- 23: guard folded (supersedes docs/design/test-drivers.md "The binary is built once per `go test` run")
- 24: recast
- 25: recast
- 26: recast
- 27: guard folded
- 28: guard folded
- 30: guard folded
- 31: guard folded
- re-check 6: recast (testtime.Eventually and testtime.Expiry never scale; callers pass an already-budgeted value)
- re-check 8: guard folded (rule (a) covers Eventually's and Expiry's duration argument)
- re-check 9: guard folded (no raw literal, no double Budget; binds items 10–20)
- re-check 22: guard folded (styled render built on demand only, memoised on the cache entry)
- re-check 24: guard folded; yields to the header's ratified WaitQuiet call and awaitApprovalPane's doc (cmd/apogee/e2e_approval_test.go:452, "returns the settled frame it painted")
- re-check 25: guard folded (absence reads wait on waitIdle; the backend-state read set defined)
- re-check 26: guard folded (as 25)
- re-check fold (2026-10-12): 6, 8, 22, 24 — guards displaced by decisions folded.

**Sources:**
- `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` — findings, CI census, measurements
- `docs/handoffs/2026-10-12 - 02 - test-suite-stability-speed-memory-investigation.md`
- `docs/design/test-drivers.md`; `scripts/test-shards.sh` header; `docs/manual/building.md`
- `.capcom/issues/snapshot-frame-cache.md`, `.capcom/issues/e2e-typed-prompts.md`

**Ratified design calls (owner, 2026-10-12):**
- **Scale knob:** a new `APOGEE_TEST_TIME_SCALE` (a number, default 1) scales every test budget. `APOGEE_TEST_SLOW=1` implies 2 unless the knob is set explicitly.
- **Enforcement:** a Go AST guard test, not forbidigo.
- **Migration:** every existing site in this plan, not a ratchet.
- **Exceptions:** marked by a wrapper at the call site (`testtime.Fixed`), never by an allowlist in the guard.
- **Windows vet:** inside `make check`, widened to `./...`.
- **Typed prompts:** profile first. Paste only if typing is at least 10% of the measured e2e time.
- **WaitQuiet:** three bounded slices: armed→key, before `Quit`, before a backend-state read. The waits before goldens and whole-frame asserts stay.
- **snapshot-frame-cache:** included.
- **No CI retry-on-fail** (review §8).

**Standing requirements:**
- skills: coding-standards
- This box (Pi, 8 GB): never run `-race` over a whole heavy package (`./cmd/apogee`, `./internal/tui`) or a package pattern. Narrow every heavy-package run with `-run`, always under `GOMEMLIMIT=2GiB`, and never two test runs at once. The full suite runs only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`.
- Bubble Tea is v2 (`charm.land/bubbletea/v2`; `tea.KeyPressMsg`, `tea.PasteMsg`).
- The Windows-only files cannot run here. Their per-item gate is `GOOS=windows go vet` on the package; CI runs them.

**Out of scope:**
- `tui-model-frame-size`; auto-retry in CI; `scripts/test-shards.sh` on Windows; `GOMEMLIMIT` in CI.
- Shrinking any sweep's coverage: the slow tests are split, never cut.
- The ~90 `WaitQuiet` sites before goldens and whole-frame asserts.

## 1. ConPTY command test drains before it closes — ✅ DONE (2026-10-12)

**What:** Fixes the 2026-10-12 CI flake `TestStartPseudoConsoleRunsTheCommand`. The test calls `pty.Close()` before its drain finishes, so `CloseOutput` cancels the read in flight and loses conhost's final frame (the path `conpty-final-frame-lost` fixed in production).
**Goal:** `TestStartPseudoConsoleRunsTheCommand` releases the console, waits for the drain to reach EOF, then calls `CloseOutput`, and asserts the marker. A `t.Cleanup` closes the console on every Fatal path.
**Approach (assumed at the header base):** in `internal/platform/conpty_windows_test.go`, replace `pty.Close()` before `output.wait(t)` with `pty.Release()` → `output.wait(t)` → `pty.CloseOutput()`, the same order `TestPseudoConsoleReleaseKeepsTheFinalFrame` uses, and register `t.Cleanup(func() { _ = pty.Close() })` right after start. `TestPseudoConsoleCloseClosesTheOutput` keeps its deliberate close-first shape.
**Files:** internal/platform/conpty_windows_test.go
**Read first:** internal/platform/conpty_windows_test.go — TestStartPseudoConsoleRunsTheCommand, TestPseudoConsoleReleaseKeepsTheFinalFrame, drainPseudoConsole, pseudoConsoleOutput.wait; internal/platform/conpty_windows.go — PseudoConsole.Close, PseudoConsole.Release, PseudoConsole.CloseOutput (all idempotent via sync.Once, so the t.Cleanup Close after Release+CloseOutput is a no-op)
**Closes:** conpty-test-drain-order
**Tests:** the changed test itself; it runs in CI's `windows tests` job.
**Acceptance:** `GOOS=windows go vet ./internal/platform/`; `GOOS=windows go test -c -o /dev/null ./internal/platform/`
**Commit:** `fix(platform): drain the ConPTY command test before closing its output`

## 2. Windows CI runs the ConPTY packages one at a time — ✅ DONE (2026-10-12)

**What:** The `test-windows` job runs one raw `go test -race` over five trees at once. Its two ConPTY-heavy packages, `internal/platform` and `internal/console`, then share 4 vCPU with each other's 50-run cmd.exe/conhost loops.
**Regression guard.** today one `go test` over all five trees reports every package even when `internal/platform` fails; split in two, a ConPTY failure would skip the second step and drop the probe/auditsplit/cmd/release results from that run. The second step carries `if: ${{ !cancelled() }}` (or the three light trees run first and the ConPTY step carries it), and the job comment says so.
**Goal:** the `windows tests` job runs `internal/platform/...` and `internal/console/...` in one step with `-p 1` and `-timeout 30m`, and the remaining three trees in a second step. All five trees are still covered with `-race -count=1`.
**Approach (assumed at the header base):** in `.github/workflows/ci.yml`, job `test-windows`, split the `go test (race)` step in two, and update the job's comment to say why the ConPTY packages run serially.
**Files:** .github/workflows/ci.yml
**Read first:** .github/workflows/ci.yml — job test-windows, step `go test (race)` and its comment block; docs/manual/building.md — "Windows-tagged tests run on a `windows-latest` job" paragraph; scripts/check-pins.sh — pinned regex (no new `uses:` added)
**Closes:** windows-ci-conpty-load
**Tests:** none (CI configuration).
**Acceptance:** `make actionlint`; `./scripts/check-pins.sh`
**Commit:** `ci: run the ConPTY packages one at a time in the windows job`

## 3. make check vets the whole module for Windows — ✅ DONE (2026-10-12)

**What:** `make check` cross-vets only the five Windows trees.
**Regression guard.** `docs/manual/building.md` describes the Windows vet in two places — the `make check` table row and the "Windows-tagged tests run on a `windows-latest` job" paragraph ("vets the five trees that job tests …"); both are rewritten, and the five-tree list then lives only in ci.yml's test-windows step.
**Goal:** the `check` target runs `GOOS=windows go vet ./...`, and `docs/manual/building.md`, wherever it describes the steps of `make check`, says so.
**Approach (assumed at the header base):** in the `Makefile` `check` recipe, widen the existing `GOOS=windows go vet …` line to `./...` and update its comment.
**Files:** Makefile, docs/manual/building.md
**Read first:** Makefile — target check (the `GOOS=windows go vet` line and the "Plain `go vet ./...` has no step of its own" comment above it); docs/manual/building.md — `make check` table row, "Windows-tagged tests run on a `windows-latest` job" paragraph; .github/workflows/ci.yml — test-windows comment ("`make check` … can only compile them")
**Tests:** none.
**Acceptance:** `GOOS=windows GOMEMLIMIT=2GiB go vet ./...` exits 0; `grep -n 'GOOS=windows go vet ./\.\.\.' Makefile`; `grep -n 'auditsplit/\.\.\.' docs/manual/building.md` returns nothing
**Commit:** `build: vet the whole module for Windows in make check`

## 4. Line endings pinned repo-wide — ✅ DONE (2026-10-12)

NOTES (2026-10-12): renormalize verified in a throwaway detached worktree at HEAD carrying the new .gitattributes (the main tree holds a wave sibling's uncommitted edits, which `git add --renormalize .` would stage): it staged only .gitattributes; check-attr, the i/crlf count (0) and the makeWin.bat CRLF checkout (`\r \n`) all matched Acceptance there.

**What:** `.gitattributes` pins LF one path at a time, and every new byte-compared path has broken Windows CI once before it got its pin.
**Regression guard.** `git add --renormalize .` stages nothing, `makeWin.bat` included: its index copy stays LF (`eol=crlf` converts on checkout only), so it has no diff to commit and leaves **Files:**; its CRLF checkout is checked with `rm makeWin.bat && git checkout makeWin.bat && od -c makeWin.bat | head -1`. `binary` does not clear `eol`, so the expected `check-attr` lines are spelled out in Acceptance (probed with git 2.47).
**Goal:** `.gitattributes` opens with `* text=auto eol=lf`. It marks `*.bat` `text eol=crlf`, and `*.png *.gif *.jpg *.pdf *.ttf *.ico` as `binary`, and keeps the `.capcom/issues/**/*.md merge=capcom-log` line. Per-path `eol=lf` lines that the default makes redundant are removed, and `git add --renormalize .` stages nothing.
**Approach (assumed at the header base):** rewrite `.gitattributes` with a short comment stating the rule. Run `git add --renormalize .` and inspect `git status --porcelain`; nothing may change.
**Files:** .gitattributes
**Read first:** .gitattributes — every per-path `text eol=lf` block, the testdata `binary` lines, `.capcom/issues/**/*.md merge=capcom-log`; makeWin.bat; `git ls-files --eol` (at base: 0 i/crlf, all forced-text files detect as i/lf, binaries = graphics/demo.gif, graphics/demo/fonts/*.ttf, testdata *.pdf)
**Closes:** crlf-per-path-pins
**Tests:** none.
**Acceptance:** `git check-attr text eol -- internal/probe/testdata/x.golden makeWin.bat graphics/demo.gif` prints `x.golden: eol: lf`, `makeWin.bat: text: set` and `makeWin.bat: eol: crlf`, and `graphics/demo.gif: text: unset`; `git ls-files --eol | grep -c 'i/crlf'` is 0; `rm makeWin.bat && git checkout makeWin.bat && od -c makeWin.bat | head -1` shows `\r \n`
**Commit:** `build: pin LF line endings repo-wide`

## 5. CI check job: default lanes and its own race cache — ✅ DONE (2026-10-12)

NOTES (2026-10-12): the Acceptance grep over `docs .github` cannot return nothing literally: `docs/plans/` (this plan) and `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` quote the old value as history; ran it excluding those two directories, and it returns nothing.

**What:** The check job forces `APOGEE_TEST_LANES: 5` on 4 vCPU. It also shares one setup-go cache key with the lint, vulncheck and cross jobs, so its race build objects are rarely the ones saved.
**Regression guard.** with the env gone, `docs/manual/building.md` ("CI runs `make test` under `APOGEE_TEST_LANES=5`: a hosted runner is a 4 vCPU box…") and the step comment justifying "five lanes" are false; both are rewritten (the default on 4 vCPU is 3 lanes: `scripts/test-shards.sh` budget = nproc − 1). The cache key hashes `go.sum` + `go.mod`, never `scripts/test-shards.sh`: setup-go has no restore-keys, so every edit to that script (item 7 edits it) would run a cold race build.
**Goal:** the `check` job sets no `APOGEE_TEST_LANES`, so lanes come from nproc as on every other box, and its setup-go cache key differs from the other ubuntu jobs'.
**Approach (assumed at the header base):** in `.github/workflows/ci.yml`, remove the `APOGEE_TEST_LANES` env from the `make test` step. Give the `check` job's `actions/setup-go` a `cache-dependency-path` of `go.sum` plus `go.mod` (multi-line), which changes the hash without adding an action.
**Files:** .github/workflows/ci.yml, docs/manual/building.md
**Read first:** .github/workflows/ci.yml — job check, its actions/setup-go `with:` block, step `go test (race, in lanes)` env and comment; scripts/test-shards.sh — lane sizing (budget, is_slow_box, APOGEE_TEST_LANES); docs/manual/building.md — knobs table and the "CI runs `make test` under" paragraph
**Closes:** ci-check-lanes-cache
**Depends on:** item 2
**Tests:** none.
**Acceptance:** `make actionlint`; `./scripts/check-pins.sh`; `grep -rn 'LANES=5\|LANES: 5\|five lanes' docs .github` returns nothing
**Commit:** `ci: default lanes and a race-build cache key for the check job`

## 6. internal/testtime: scaled budgets, fixed durations, Eventually — ✅ DONE (2026-10-12)

NOTES (2026-10-12): parseScale also rejects "NaN" and "Inf" (both parse as floats; NaN is not above 0 and an infinite scale overflows a Duration), and the parse table carries those two rows beyond the plan's six.
NOTES (2026-10-12): Scale keeps the parse error from its one sync.Once read and panics on every call, not only the first, so a bad value cannot fall through to a zero scale on a later call.

**What:** Recast at the regression check (2026-10-12). The single home for test timing (review §4, class A).
**Regression guard.** testtime also exports `Expiry(budget time.Duration) func() bool` — a non-failing deadline that reports true once the budget has elapsed since the call — for polls that cannot fail the test themselves (code off the test goroutine, or kit helpers such as tuitest's WaitFor and checkLeaks that report their own failure); unit-tested beside Eventually. Binding convention — testtime.Eventually and testtime.Expiry never scale; their duration argument is an already-budgeted value (a testtime.Budget/Fixed call or an identifier initialised from one, e.g. tuitest.DefaultTimeout); the package doc states this. Budget delegates to an unexported `scaled(d, s)` helper the tests drive directly, and they assert `Budget(d) == scaled(d, Scale())`, so they hold at any `APOGEE_TEST_TIME_SCALE` (Scale has no setter, and after item 7 a SLOW box runs every `make test` at scale 2).
**Goal:** package `internal/testtime` exports:
- `Scale() float64`: reads `APOGEE_TEST_TIME_SCALE` once. Empty means 1; a value that does not parse to a number above 0 panics, naming the variable and the value.
- `Budget(d time.Duration) time.Duration`: `d` scaled. Used for deadlines and waits that end on a condition.
- `Fixed(d time.Duration) time.Duration`: `d` unchanged, marking a semantic duration such as pacing, a "nothing happens within" window, a timeout meant to expire, or a fixture age.
- `Eventually(t testing.TB, budget time.Duration, cond func() bool, what string)`: polls every 20 ms. On timeout it calls `t.Fatalf` with "timed out after <budget> waiting for <what>".

It has unit tests, and it imports nothing from the module.
**Approach (assumed at the header base):** use `sync.Once` for the env read. The package doc states the rule: every timing in a test goes through Budget or Fixed, and a poll goes through Eventually. Do not export a setter; tests of Scale call an unexported parse helper.
**Files:** internal/testtime/testtime.go, internal/testtime/testtime_test.go
**Read first:** internal/tuitest/wait.go — WaitFor, waiter.failure, DefaultTimeout, pollInterval; internal/tuitest/leak.go — checkLeaks, leakGrace; internal/tuitest/leak_test.go — recordingTB (the embedded-testing.TB fake shape Eventually's timeout test needs); internal/provider/stream_test.go — dripServer
**Tests:** a parse table (empty, "2", "0.5", "0", "-1", "x"); Budget under a scale of 2; Fixed is the identity; Eventually succeeds and times out, the latter driven through a `testing.TB` fake, its message naming the budget as passed; Expiry reports false before and true after its budget, unscaled at any scale.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/testtime/`; `go vet ./internal/testtime/`; `! go list -f '{{join .Imports "\n"}}' ./internal/testtime/ | grep -q 'airiclenz/apogee'` (the package imports nothing from the module)
**Commit:** `feat(testtime): add the shared test timing package`

## 7. tuitest budgets and the shard script honour the scale

**What:** Wires the knob through.
**Goal:**
- `tuitest.DefaultTimeout`, `teardownTimeout` and `leakGrace` are derived from `testtime.Budget`.
- `scripts/test-shards.sh` exports `APOGEE_TEST_TIME_SCALE=2` under `APOGEE_TEST_SLOW=1` unless the caller already set it, and its `==>` line names any scale other than 1.
- `docs/manual/building.md`'s knob table documents `APOGEE_TEST_TIME_SCALE`, and says `APOGEE_TEST_SLOW` now scales budgets as well as concurrency.
**Approach (assumed at the header base):** in `internal/tuitest/wait.go`, `DefaultTimeout` and `teardownTimeout` move out of the const block into vars initialised with `testtime.Budget(…)`. In `internal/tuitest/leak.go`, `leakGrace` follows `DefaultTimeout` as a var. `pollInterval` stays a const.
**Files:** internal/tuitest/wait.go, internal/tuitest/leak.go, scripts/test-shards.sh, docs/manual/building.md
**Read first:** internal/tuitest/wait.go — DefaultTimeout, teardownTimeout; internal/tuitest/leak.go — leakGrace; internal/tuitest/pty.go — PTYDriver.awaitDrain; cmd/apogee/e2e_stream_test.go — streamReplyWait (package var built from tuitest.DefaultTimeout; no const user exists); scripts/test-shards.sh — is_slow_box, "==> go test -race" echo; docs/manual/building.md — knob table (APOGEE_TEST_SLOW row)
**Depends on:** item 6
**Tests:** a tuitest test that `DefaultTimeout` equals `testtime.Budget(60*time.Second)`.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tuitest/`; `bash -n scripts/test-shards.sh`; `APOGEE_TEST_SLOW=1 APOGEE_TEST_LANES=1 ARGS='-run ^TestNothing$' make test 2>&1 | grep -i scale`
**Commit:** `test(tuitest): derive the kit's budgets from testtime`

## 8. Guard: no fixed timing in tests

**What:** Recast at the regression check (2026-10-12). The structural ban (class A), modelled on `cmd/apogee/seams_guard_test.go`'s AST walk but repo-wide.
**Regression guard.** rule (b) names testtime.Eventually and testtime.Expiry as the sanctioned replacements; findings in a pending package never fail but are still printed with t.Logf as `file:line: …`, so a per-file migration goal is checkable with `go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/ | grep <file>`. Rule (a) resolves a `:=` or local `var` identifier (driver.go's `wait`, pty.go's `remaining`) to its initialiser in the enclosing function, else accepts it; the Bites fixture holds one such local. The guard applies its literal rule (a) to the duration argument of testtime.Eventually and testtime.Expiry as well, so a literal passed to them unwrapped is a finding; the Bites fixture covers it. The walk runs the module-relative directory through `filepath.ToSlash` before the `pendingPackages` lookup and in every reported `file:line`, so the set's forward-slash keys match on Windows too, where `makeWin.bat`'s target_test (`test`/`check`) runs `go test ./...` and `filepath.Rel` yields `internal\tui`.
**Goal:** `TestNoFixedTimingInTests` in `internal/testtime` walks the module from its root and parses every `_test.go` file plus the non-test files of `internal/tuitest`, skipping `internal/testtime`. It reports a `file:line` finding for each violation of these rules:
- (a) A call to `time.Sleep`, `time.After`, `time.NewTimer`, `time.AfterFunc`, `time.NewTicker`, `time.Tick`, `context.WithTimeout`, `context.WithDeadline` or `tuitest.Within` (bare `Within` inside tuitest) whose duration argument contains a `time.<Unit>` selector or a numeric literal outside a `testtime.Budget(…)` or `testtime.Fixed(…)` call. Same-package identifiers are resolved to their const or var initialiser and checked recursively. `tuitest.DefaultTimeout` is accepted.
- (b) Any `time.Now().Before(…)`, `time.Now().After(…)` or `time.Until(…)`; use `testtime.Eventually` or `testtime.Expiry` instead.

A `pendingPackages` set names, by module-relative directory, every package that has findings when this item lands. Those packages are skipped, and a pending package with zero findings is itself a failure ("remove it from pendingPackages"). `TestNoFixedTimingInTestsBites` pins the exact findings for a string fixture covering each banned and each allowed form.
**Approach (assumed at the header base):** use `go/parser` with `SkipObjectResolution` and `ast.Inspect`. Find the module root by walking up to `go.mod`. Fill the pending set from the guard's own first run.
**Files:** internal/testtime/guard_test.go
**Read first:** cmd/apogee/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam, TestNoParallelTestSwapsAPackageSeamBites, packageGoFiles, locallyDeclared; internal/tuitest/driver.go — escapeGap; internal/tuitest/pty.go — PTYDriver.awaitDrain; cmd/apogee/e2e_seat_test.go — Within(2*seatBeat+tuitest.DefaultTimeout); makeWin.bat — target_test
**Depends on:** item 7
**Tests:** the guard and its Bites test (a local-identifier case among the fixtures, a literal passed unwrapped to `testtime.Eventually` and to `testtime.Expiry` as findings beside budgeted arguments accepted, and a pending package's findings logged, not failed).
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'TestNoFixedTimingInTests' ./internal/testtime/`
**Commit:** `test(testtime): guard tests against fixed sleeps and deadlines`

## 9. Migrate internal/tuitest

**What:** Recast at the regression check (2026-10-12). The first package off the pending list. Every migration item (9–20) applies the same rule:
- A deadline or a wait that ends on a condition → `testtime.Budget`.
- A hand-rolled poll loop → `testtime.Eventually`.
- Pacing, a negative window, a timeout meant to expire, or a fixture → `testtime.Fixed`.
- Named budget constants get `Budget`/`Fixed` initialisers.
- No assertion is weakened and no budget shortened.
- A package leaves `pendingPackages` in the item that finishes its last file.
**Regression guard.** the migration rule gains: a poll that cannot call t.Fatal (off the test goroutine, or a kit helper that reports its own failure such as WaitFor or checkLeaks) uses testtime.Expiry. WaitFor and checkLeaks loop on Expiry and keep their own failure (the frame and artifacts via `waiter.failure`; Errorf + stacks, as `leak_test.go` pins), never Eventually; checkLeaks keeps its 50 ms cadence (`testtime.Fixed(50*time.Millisecond)` or a named `leakPoll` const), not `pollInterval`. Callers of Eventually/Expiry pass Budget(...) or a budgeted identifier, never a raw literal, and never wrap an already-budgeted identifier in Budget again (no double scaling); this convention binds items 10–20 through item 9's rule.
**Goal:** `internal/tuitest` has no guard findings and is not in `pendingPackages`.
**Approach (assumed at the header base):** `escapeGap` → `Fixed`. The poll sleep in `wait.go` → `Fixed(pollInterval)`; the one in `leak.go` → `Fixed(50*time.Millisecond)`. The self-tests in `pty_test.go`, `screen_test.go` and `wait_test.go` follow the rule.
**Files:** internal/tuitest/driver.go, internal/tuitest/pty.go, internal/tuitest/wait.go, internal/tuitest/leak.go, internal/tuitest/pty_test.go, internal/tuitest/screen_test.go, internal/tuitest/wait_test.go, internal/testtime/guard_test.go
**Read first:** internal/tuitest/wait.go — WaitFor, waiter.failure, pollInterval; internal/tuitest/leak.go — checkLeaks, leakGrace; internal/tuitest/leak_test.go — selfTestGrace, recordingTB; internal/tuitest/driver.go — escapeGap
**Depends on:** item 8
**Tests:** the existing tuitest tests, plus a test that a timed-out `WaitFor` on a screen reports through `waiter.failure` (a `testing.TB` fake capturing Fatalf).
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tuitest/ ./internal/testtime/`
**Commit:** `test(tuitest): route every timing through testtime`

## 10. Migrate cmd/apogee, part 1 (e2e core)

**What:** Item 9's rule. Includes the derived budgets `settled`, `repaintBudget`, `typingAllowance`, `toolTurnAllowance` and `streamReplyWait`.
**Regression guard.** Acceptance adds a check that `go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/` prints no finding naming any of the item's files; every cmd/apogee race run carries `-parallel 1`. A poll whose timeout is an answer, not a failure (awaitRepaint, waitForFrameChange, waitForScroll, stepSettings/awaitIdlePrompt, allowIfAsked, answerAdoptionArmed's window, holdKey's off-goroutine pump) uses testtime.Expiry; a negative window such as paneNeverShows is bounded by a timer on testtime.Fixed; Eventually only where a timeout fails the test on the test goroutine. Wrapping `settled` makes it a var, so `pace`, `promptly` (e2e_approval_test.go) and `repaintBudget` (e2e_support_test.go) become vars: `promptly` a Budget ceiling, `pace` and `repaintBudget` Fixed "no movement" windows.
**Goal:** these files have no guard findings: e2e_support, e2e_smoke, e2e_stream, e2e_hostile, e2e_approval, e2e_parallel, e2e_console, e2e_announced (`cmd/apogee/*_test.go`).
**Approach (assumed at the header base):** apply item 9's rule site by site. `settled` and `serialWatch` used as "nothing moved" windows → `Fixed`. Their use as a ceiling → `Budget`.
**Files:** cmd/apogee/e2e_support_test.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_stream_test.go, cmd/apogee/e2e_hostile_test.go, cmd/apogee/e2e_approval_test.go, cmd/apogee/e2e_parallel_test.go, cmd/apogee/e2e_console_test.go, cmd/apogee/e2e_announced_test.go
**Read first:** cmd/apogee/e2e_smoke_test.go — settled, typingAllowance, stepSettings, awaitIdlePrompt; cmd/apogee/e2e_support_test.go — repaintBudget, awaitRepaint; cmd/apogee/e2e_approval_test.go — answerAdoptionArmed, holdKey; cmd/apogee/e2e_parallel_test.go — serialWatch, paneNeverShows
**Depends on:** item 9
**Tests:** the existing tests in those files, and the Adoption, LiveState, Width, Outcome and Delegation e2e tests that call the changed helpers.
**Acceptance:** `go vet ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestE2E(Smoke|Stream|Hostile|Approval|Adoption|Parallel|Console|Announced|LiveState|Width|Outcome|Delegation)' ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/ | grep -E 'cmd/apogee/e2e_(support|smoke|stream|hostile|approval|parallel|console|announced)_test.go'` prints nothing
**Commit:** `test(apogee): route the e2e core's timing through testtime`

## 11. Migrate cmd/apogee, part 2 (wiring and delegation)

**What:** Item 9's rule.
**Regression guard.** Acceptance adds a check that `go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/` prints no finding naming any of the item's files; every cmd/apogee race run carries `-parallel 1`. The `-run` also names the six edited tests no keyword matches (TestCaptureStderrRestoresOnGoexit, TestFiringHooksRefusesADenyListedWebhookEndpoint, TestASessionRaisedFiringRunsThroughTheBootsRunner, and three in wire_settings_test.go).
**Goal:** these files have no guard findings: e2e_egress, delegation, schedule, title, wire_boot, wire_firing, wire_live, wire_settings (`cmd/apogee/*_test.go`).
**Approach (assumed at the header base):** site by site, per item 9. The `time.After(time.Millisecond)` select ticks in `schedule_test.go` → `Fixed`. `gateSettleWindow` → `Fixed`.
**Files:** cmd/apogee/e2e_egress_test.go, cmd/apogee/delegation_test.go, cmd/apogee/schedule_test.go, cmd/apogee/title_test.go, cmd/apogee/wire_boot_test.go, cmd/apogee/wire_firing_test.go, cmd/apogee/wire_live_test.go, cmd/apogee/wire_settings_test.go
**Read first:** cmd/apogee/schedule_test.go — awaitRequest, gateSettleWindow; cmd/apogee/title_test.go — stalledReplyBackstop (titleRequestTimeout lives in title.go); cmd/apogee/delegation_test.go — racedClearAgainstALanding; cmd/apogee/wire_boot_test.go — TestCaptureStderrRestoresOnGoexit; cmd/apogee/e2e_egress_test.go — TestE2EEgressLongStreamIsNotDeadlined; cmd/apogee/wire_settings_test.go — TestHookRunnerReplaceNeverReportsOnTheCallersGoroutine
**Depends on:** item 10
**Tests:** the existing tests in those files.
**Acceptance:** `go vet ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '(Egress|Delegat|Schedule|Title|Wire|CaptureStderr|FiringHooksRefuses|SessionRaisedFiring|ApplySettingReactions|ReactionsRowReload|HookRunnerReplace)' ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/ | grep -E 'cmd/apogee/(e2e_egress|delegation|schedule|title|wire_(boot|firing|live|settings))_test.go'` prints nothing
**Commit:** `test(apogee): route wiring and delegation timing through testtime`

## 12. Migrate cmd/apogee, part 3, and release the package

**What:** Item 9's rule, and cmd/apogee leaves `pendingPackages`.
**Regression guard.** every cmd/apogee race run carries `-parallel 1`. **Files:** gains e2e_seat_test.go (`tuitest.Within(2*seatBeat+…)`), update_test.go and e2e_newcomer_test.go, which the guard names; daemonfire_test.go has no site. The `-run` also names TestWatchedProjectConfig* and TestE2ESeat*, whose names match no listed keyword.
**Goal:** `cmd/apogee` has no guard findings and is not in `pendingPackages`.
**Approach (assumed at the header base):** the remaining files are daemon, undo, projectrules, configwatch_apply, mcp_login_fetch, mcp_cmd, probemodel, update, headless, e2e_seat and e2e_newcomer `_test.go`, plus any file the guard still names.
**Files:** cmd/apogee/daemon_test.go, cmd/apogee/undo_test.go, cmd/apogee/projectrules_test.go, cmd/apogee/configwatch_apply_test.go, cmd/apogee/mcp_login_fetch_test.go, cmd/apogee/mcp_cmd_test.go, cmd/apogee/probemodel_test.go, cmd/apogee/headless_test.go, cmd/apogee/e2e_seat_test.go, cmd/apogee/update_test.go, cmd/apogee/e2e_newcomer_test.go, internal/testtime/guard_test.go
**Read first:** internal/testtime/guard_test.go — pendingPackages; cmd/apogee/configwatch_apply_test.go — configWatchTestSettle, configWatchTestDeadline, reloadWhole; cmd/apogee/projectrules_test.go — waitForChildRequest; cmd/apogee/mcp_login_fetch_test.go — awaitOutcome; cmd/apogee/e2e_seat_test.go — seatBeat; cmd/apogee/update_test.go — runUpdateCommand
**Depends on:** item 11
**Tests:** the existing tests in those files.
**Acceptance:** `go vet ./cmd/apogee/`; `GOMEMLIMIT=2GiB go test -count=1 -run 'TestNoFixedTimingInTests' ./internal/testtime/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '(Daemon|Undo|ProjectRule|ConfigWatch|MCP|ProbeModel|Headless|Update|TestWatchedProjectConfig|TestE2ESeat)' ./cmd/apogee/`
**Commit:** `test(apogee): finish routing cmd/apogee timing through testtime`

## 13. Migrate internal/tui, part 1

**What:** Item 9's rule. Includes the named budgets `pumpTimeout`, `reseatDeadline` and `namingCallDeadline`.
**Regression guard.** Acceptance adds the per-file guard-output check of item 10 for its files; every internal/tui race run carries `-parallel 1`. The `-run` also names the tests that hold or call a migrated site but match no file keyword (the model approval/ask folds, the prompt-scroll reseat, the clipboard and drag-copy tests, the beat tick, startup restore, profile load and the migration tests).
**Goal:** these files have no guard findings: actuation, adoption, approver, asker, autotitle, heartbeat, model, mouse (`internal/tui/*_test.go`).
**Approach (assumed at the header base):** site by site, per item 9. The 250 ms negative window in `heartbeat_test.go` → `Fixed`.
**Files:** internal/tui/actuation_test.go, internal/tui/adoption_test.go, internal/tui/approver_test.go, internal/tui/asker_test.go, internal/tui/autotitle_test.go, internal/tui/heartbeat_test.go, internal/tui/model_test.go, internal/tui/mouse_test.go
**Read first:** internal/tui/actuation_test.go — pumpTimeout, msgWithin, pumpItem, firstRestore; internal/tui/model_test.go — reseatDeadline, TestPromptScrollReseatCannotSpin; internal/tui/heartbeat_test.go — TestBeatTickRunsOnTheStatedInterval; internal/tui/autotitle_test.go — namingCallDeadline
**Depends on:** item 9
**Tests:** the existing tests in those files.
**Acceptance:** `go vet ./internal/tui/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '(Actuation|Adoption|Approver|Asker|AutoTitle|Heartbeat|Mouse|ModelApprovalFold|ModelAskFold|PromptScrollReseat|Clipboard|DragCopy|BeatTick|InitFiresImmediateBeat|StartupRestore|ProfileLoad|KeyMigration|SubAgentsMigration)' ./internal/tui/`; `GOMEMLIMIT=2GiB go test -count=1 -v -run 'TestNoFixedTimingInTests$' ./internal/testtime/ | grep -E 'internal/tui/(actuation|adoption|approver|asker|autotitle|heartbeat|model|mouse)_test.go'` prints nothing
**Commit:** `test(tui): route timing through testtime, part 1`

## 14. Migrate internal/tui, part 2, and release the package

**What:** Item 9's rule, and internal/tui leaves `pendingPackages`.
**Regression guard.** every internal/tui race run carries `-parallel 1`. **Files:** gains conpty_windows_test.go (`time.NewTimer`, `time.Sleep(conptySettle)`), live_test.go and smoke_live_test.go (`context.WithTimeout(…, 2*time.Minute)`), which the guard names. The `-run` also names ParkCall, Build, CancelledStep and the Start*Cancel tests, which no listed keyword matches. `runUntil` (tui_test.go) registers `t.Cleanup(func(){ program.Kill(); <-done })` before its Eventually, so a timeout never leaves `program.Run` running.
**Goal:** `internal/tui` has no guard findings and is not in `pendingPackages`.
**Approach (assumed at the header base):** the remaining files are parkedcall, recall, runview, sink, tui_test, update_check, worker and the Windows-only conpty test, plus any file the guard still names. The live-gated files follow the same rule.
**Files:** internal/tui/parkedcall_test.go, internal/tui/recall_test.go, internal/tui/runview_test.go, internal/tui/sink_test.go, internal/tui/tui_test.go, internal/tui/update_check_test.go, internal/tui/worker_test.go, internal/tui/conpty_windows_test.go, internal/tui/live_test.go, internal/tui/smoke_live_test.go, internal/testtime/guard_test.go
**Read first:** internal/tui/tui_test.go — runUntil; internal/tui/conpty_windows_test.go — conptySettle, waitConPTYChild; internal/tui/smoke_live_test.go, internal/tui/live_test.go — the context.WithTimeout sites; internal/tui/sink_test.go — waitForEvents; internal/tui/parkedcall_test.go — waitForReplies; internal/tui/worker_test.go — TestStartResumeCancelYieldsCancelledMsg; internal/testtime/guard_test.go — pendingPackages
**Depends on:** item 13
**Tests:** the existing tests in those files.
**Acceptance:** `go vet ./internal/tui/`; `GOOS=windows go vet ./internal/tui/`; `GOMEMLIMIT=2GiB go test -count=1 -run 'TestNoFixedTimingInTests' ./internal/testtime/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '(Parked|Recall|RunView|Sink|UpdateCheck|Worker|ParkCall|Build|CancelledStep|StartResume|StartExchange|StartCompact)' ./internal/tui/`
**Commit:** `test(tui): finish routing internal/tui timing through testtime`

## 15. Migrate internal/mcp and internal/mcpauth

**What:** Item 9's rule. Includes `gatedServerWait` and `mcpCallTimeout`; the wedged-server `Sleep(time.Minute)` → `Fixed`.
**Regression guard.** `runGatedFixtureServer` (mcp_test.go) polls `time.Now().After(deadline)` inside the re-exec'd fixture subprocess, which has no `testing.TB`; its loop is bounded by `testtime.Expiry(gatedServerWait)` (item 9's rule) or a `time.NewTimer(testtime.Budget(gatedServerWait))` select beside the 10 ms stat poll, keeping `os.Exit(2)` on expiry, never Eventually.
**Goal:** `internal/mcp` and `internal/mcpauth` have no guard findings and are not in `pendingPackages`.
**Approach (assumed at the header base):** site by site, per item 9.
**Files:** internal/mcp/mcp_test.go, internal/mcp/transport_test.go, internal/mcp/tool_test.go, internal/mcpauth/handler_test.go, internal/testtime/guard_test.go
**Read first:** internal/mcp/mcp_test.go — runGatedFixtureServer, gatedServerWait, runFixtureServer, assertNoGoroutineIn, pidAlive; internal/mcp/tool_test.go — mcpCallTimeout override; internal/mcp/transport_test.go — context.WithTimeout(20s) sites; internal/mcpauth/handler_test.go — storedRecord fixture ages (not banned forms, so mcpauth has no findings)
**Depends on:** item 9
**Tests:** the existing package tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcp/ ./internal/mcpauth/`
**Commit:** `test(mcp): route timing through testtime`

## 16. Migrate internal/tools

**What:** Item 9's rule. Includes `consoleCancelBudget`; the network_funnel watchdog multiples keep their ratios.
**Regression guard.** `pidAlive(pid, within) bool` (terminal_test.go) and `waitFor(budget, done) bool` (console_confine_linux_test.go) keep their signatures, their loops re-implemented on a testtime bound (`testtime.Expiry` or a timer on `testtime.Budget`): pidAlive is also called from exec_common_test.go and the Windows-only terminal_windows_test.go, which are not in **Files:**, and reshaping it into Eventually would turn their t.Errorf into Fatalf.
**Goal:** `internal/tools` has no guard findings and is not in `pendingPackages`.
**Approach (assumed at the header base):** the files are askqueue, console_read, console_send, console_confine_linux, terminal, read_file_unix, network_funnel, console_open_windows and console_windows `_test.go`.
**Files:** internal/tools/askqueue_test.go, internal/tools/console_read_test.go, internal/tools/console_send_test.go, internal/tools/console_confine_linux_test.go, internal/tools/terminal_test.go, internal/tools/read_file_unix_test.go, internal/tools/network_funnel_test.go, internal/tools/console_open_windows_test.go, internal/tools/console_windows_test.go, internal/testtime/guard_test.go
**Read first:** internal/tools/terminal_test.go — pidAlive, waitForPIDFile; internal/tools/exec_common_test.go — pidAlive caller; internal/tools/terminal_windows_test.go — pidAlive callers; internal/tools/console_send_test.go — consoleCancelBudget; internal/tools/network_funnel_test.go — watchdog AfterFunc(40*budget / 20*budget); internal/tools/console_confine_linux_test.go — waitFor; internal/tools/askqueue_test.go — waitForQueuedAskers
**Depends on:** item 9
**Tests:** the existing package tests.
**Acceptance:** `GOOS=windows go vet ./internal/tools/`; `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tools/`
**Commit:** `test(tools): route timing through testtime`

## 17. Migrate internal/agent and internal/domain

**What:** Item 9's rule. The slow fake collaborators' `hold` sleeps → `Fixed`; the queue ceilings → `Budget`.
**Regression guard.** `holdBeta` (fanout_test.go) runs as gatedTaskResponder's gate on the agent's goroutine and tolerates expiry; its loop is bounded by `testtime.Expiry(5*time.Second)` or a `time.NewTimer(testtime.Budget(5*time.Second))` select, keeping the fall-through to `cancel()`, never Eventually (t.Fatalf off the test goroutine).
**Goal:** `internal/agent` and `internal/domain` have no guard findings and are not in `pendingPackages`.
**Approach (assumed at the header base):** the agent files are approvalcache, approvalqueue, askqueue, background, delegationphase, fanout, naming, promptslot, setreactions, subagent, live_delegate_cap and turn `_test.go`. The domain file is promptslot `_test.go`.
**Files:** internal/agent/*_test.go (the twelve named), internal/domain/promptslot_test.go, internal/testtime/guard_test.go
**Read first:** internal/agent/fanout_test.go — holdBeta, gatedTaskResponder; internal/agent/promptslot_test.go — queuedCallerCeiling, waitForQueuedCallers; internal/domain/promptslot_test.go — promptSlotWaiterCeiling, waitForPromptSlotWaiters; internal/agent/background_test.go — approval-queue poll loop; internal/agent/delegationphase_test.go — waitFor
**Depends on:** item 9
**Tests:** the existing package tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/agent/ ./internal/domain/`
**Commit:** `test(agent): route timing through testtime`

## 18. Migrate internal/console, internal/platform and internal/filewatch

**What:** Item 9's rule. `registryTestTimeout` rises to `Budget(30*time.Second)` to match its siblings; `finalFrameFlushDelay` and the filewatch debounce sleeps → `Fixed`.
**Regression guard.** `pidAlive(pid, within)` (teardown_unix_test.go) asserts the escapee stays alive throughout a window — a negative window; it keeps its bool signature and bounds its loop by `time.NewTimer(testtime.Fixed(within))` in a select. The same timer form covers the readUntil/waitFor loops only where Eventually does not fit.
**Goal:** `internal/console`, `internal/platform` (with its subpackages) and `internal/filewatch` have no guard findings and are not in `pendingPackages`.
**Approach (assumed at the header base):** the files are console process, process_windows, registry and ring; platform lock, teardown_unix, conpty_windows and any winlabel test the guard names; filewatch filewatch and filewatch_unix (all `_test.go`).
**Files:** internal/console/*_test.go, internal/platform/lock_test.go, internal/platform/teardown_unix_test.go, internal/platform/conpty_windows_test.go, internal/filewatch/*_test.go, internal/testtime/guard_test.go
**Read first:** internal/platform/teardown_unix_test.go — pidAlive, assertSetsidEscapeeSurvived, escapeeRunTimeout; internal/console/process_test.go — testTimeout, readUntil, waitFor; internal/console/registry_test.go — registryTestTimeout; internal/console/process_windows_test.go — finalFrameFlushDelay; internal/filewatch/filewatch_test.go — testSettle, testDeadline
**Depends on:** items 1 and 9
**Tests:** the existing package tests.
**Acceptance:** `GOOS=windows go vet ./internal/console/... ./internal/platform/...`; `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/console/ ./internal/filewatch/`; `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/platform/`
**Commit:** `test(platform): route console, platform and filewatch timing through testtime`

## 19. Migrate the fake-server and harness packages

**What:** Item 9's rule. The fake-server pacing (`dripServer`, `chunkGap`, `hang`) → `Fixed`.
**Regression guard.** a time-boxed racing loop (`internal/reactions/runner_test.go` over `racingWindow`, provider's `dripServer` over `span`) becomes `stop := time.After(testtime.Fixed(racingWindow))` (resp. `span`) checked by a non-blocking select in the loop condition. `awaitLimit` is a production var in `internal/stubllm/server.go`, outside the guard's reach: only the test override (server_test.go, 20 ms meant to expire) migrates, to `Fixed`; server.go is left unchanged.
**Goal:** `internal/reactions`, `internal/provider`, `internal/stubllm`, `cmd/stubllm`, `internal/schedule`, `internal/run` and `cmd/demorig` have no guard findings and are not in `pendingPackages`.
**Approach (assumed at the header base):** site by site, per item 9. Named constants: `awaitDeadline`, `racingWindow`, `testWait`, `quietWait`, `engineTestTimeout`, `termTimeout`, `awaitLimit`.
**Files:** internal/reactions/*_test.go, internal/provider/*_test.go, internal/stubllm/*_test.go, cmd/stubllm/*_test.go, internal/schedule/*_test.go, internal/run/*_test.go, cmd/demorig/*_test.go, internal/testtime/guard_test.go
**Read first:** internal/reactions/runner_test.go — racingWindow, awaitDeadline, TestEmitRacingCloseNeverPanics; internal/provider/stream_test.go — dripServer; internal/stubllm/server.go — awaitLimit; internal/stubllm/server_test.go — awaitLimit override; internal/schedule/harness_test.go — eventually; cmd/demorig/engine_test.go — awaitInputs
**Depends on:** item 9
**Tests:** the existing package tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/reactions/ ./internal/provider/ ./internal/stubllm/ ./cmd/stubllm/ ./internal/schedule/ ./internal/run/ ./cmd/demorig/` (one package at a time if memory is tight)
**Commit:** `test: route fake-server and harness timing through testtime`

## 20. Migrate the remaining packages and retire the pending set

**What:** Item 9's rule for every package still pending: userexec, keystore, subprocess, present, gitexec, workflow, undo, security, mdns, judge, config, update, and any other package the guard names.
**Regression guard.** `cmd/release`'s `testTagTimeout` and `testPollPause` feed a counting fake sleep (virtual time); as Budget, `APOGEE_TEST_SLOW=1` would turn TestPublishTagStepFailures' pinned "after 30s" into "after 1m0s". They stay untouched: no banned call takes them, so cmd/release never enters pendingPackages and leaves this item's list. The pending-set check is `! grep -q`, since `grep -c` exits 1 on the correct outcome.
**Goal:** the guard reports no findings anywhere, and `pendingPackages`, with its "is still pending" check, is deleted from `guard_test.go`.
**Approach (assumed at the header base):** site by site, per item 9. Named constants: `testTimeout` (gitexec), `lockWaitGrace` and `parkedWalkQuiet` (undo).
**Files:** the `_test.go` files of the packages named above, internal/testtime/guard_test.go
**Read first:** cmd/release/publish_test.go — testTagTimeout, testPollPause, runTestPublishWith, TestPublishTagStepFailures; cmd/release/publish.go — publishOptions.tagTimeout; internal/undo/snapshot_test.go — parkedWalkQuiet; internal/undo/journal_test.go — lockWaitGrace; internal/present/server_test.go — fetchEventually
**Depends on:** items 12, 14, 15, 16, 17, 18 and 19
**Closes:** test-fixed-timing-class
**Tests:** the existing package tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'TestNoFixedTimingInTests' ./internal/testtime/`; `! grep -q pendingPackages internal/testtime/guard_test.go`; `GOMEMLIMIT=2GiB go test -race -count=1` over each migrated package, one at a time
**Commit:** `test: finish routing test timing through testtime`

## 21. Split the slow internal/tui sweeps into top-level tests

**What:** Three tests set the Pi's critical path: `TestOverlayHeightQueryMatchesItsRender` (141 s), `TestInputBoxWindowFollowsTheCaret` (136 s) and `TestFrameNeverExceedsTheTerminalHeight` (76 s). The shard packer cannot split a test below the top level. Coverage is kept, never cut.
**Regression guard.** every internal/tui race run carries `-parallel 1`. Every comment naming one of the three old tests is repointed (panes.go, popup.go and model.go cite TestOverlayHeightQueryMatchesItsRender as the pin; sessions_test.go and model_test.go comments too). Each split test's name starts with its old name's stem (`TestOverlayHeightQueryMatchesItsRenderPrompts`, `TestInputBoxWindowFollowsTheCaretAt8Rows`). The per-height tests pass the named constants (`frameFloorRows`, 10, `smallestOverlayWindow`, 16, 24) to `assertInputBoxWindowFollowsTheCaret`.
**Goal:** none of the three remains a top-level test. Each becomes several top-level tests over a shared helper:
- the overlay height query, one per pane group (prompts, browser+picker, workflows, settings, reports, dropdown), with a guard that every `paneFixtures()` entry lands in exactly one group;
- the caret window, one per height (8, 10, 12, 16, 24);
- the frame height, one per overlay, with the overlays table lifted into a function and a guard that each overlay is claimed once.

The union of cases equals the old sweeps.
**Approach (assumed at the header base):** extract `sweepOverlayHeightQuery(t, fx)`, `assertInputBoxWindowFollowsTheCaret(t, height)` and `runFrameHeightProperty(t, overlay)`. `paneFixtures()` stays intact for `TestOverlayPanesRenderOncePerUpdateAndView`.
**Files:** internal/tui/panes_test.go, internal/tui/model_test.go, internal/tui/sessions_test.go, internal/tui/panes.go, internal/tui/popup.go, internal/tui/model.go
**Read first:** internal/tui/panes_test.go — paneFixtures, TestOverlayHeightQueryMatchesItsRender; internal/tui/sessions_test.go — TestFrameNeverExceedsTheTerminalHeight, smallestOverlayWindow; internal/tui/model_test.go — TestInputBoxWindowFollowsTheCaret; internal/tui/paintcache_test.go — TestOverlayPanesRenderOncePerUpdateAndView; internal/tui/model.go — frameFloorRows; scripts/test-shards.sh — the costing awk (an unknown test costs 0.1 s)
**Closes:** tui-sweep-critical-path
**Depends on:** item 14
**Tests:** the split tests and the two coverage guards.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '(OverlayHeightQuery|InputBoxWindowFollows|FrameNeverExceeds)' ./internal/tui/`; `go test -list '.*' ./internal/tui/ | grep -cE '^(TestOverlayHeightQueryMatchesItsRender|TestInputBoxWindowFollowsTheCaret|TestFrameNeverExceedsTheTerminalHeight)$'` is 0; `go test -list '(OverlayHeightQuery|InputBoxWindowFollows|FrameNeverExceeds)' ./internal/tui/ | grep -c '^Test'` ≥ 16; `grep -rnE 'OverlayHeightQueryMatchesItsRender|InputBoxWindowFollowsTheCaret|FrameNeverExceedsTheTerminalHeight' internal/` cites only the new names
**Commit:** `test(tui): split the three slowest sweeps into top-level tests`

## 22. tuitest caches the snapshot until the screen changes

**What:** Closes `snapshot-frame-cache`. Every `WaitFor` poll rebuilds a full Frame and a styled render under the emulator lock.
**Regression guard.** the cmd/apogee race run carries `-parallel 1`. `Resize` changes the frame but not `written`, so the cache is keyed on a generation that both `Write` and `Resize` bump, never on `written`. The styled render is built on demand only — never on an idle poll — under s.mu, from the screen state at the cached generation (the one both `Write` and `Resize` bump), and memoised on that cache entry; this reconciles the lazy approach with the folded guard, so the failure dump's two halves are one instant, as `Frame.Styled`'s doc promises.
**Goal:** `Screen.Snapshot` returns a cached Frame while no `Write` has changed the screen since the last build, and builds the styled render only when a failure message needs it.
**Approach (assumed at the header base):** in `internal/tuitest/screen.go`, key the cache on a generation that both `Write` and `Resize` bump, and make the styled render lazy where `WaitFor` prints its failure frame. `Screen.holding` uses the same cache.
**Files:** internal/tuitest/screen.go, internal/tuitest/frame.go, internal/tuitest/screen_test.go, internal/tuitest/wait.go
**Read first:** internal/tuitest/screen.go — Screen.Snapshot, Screen.holding, Screen.Write, Screen.Resize; internal/tuitest/wait.go — waiter.failure; internal/tuitest/frame.go — newFrame, Frame.Styled; internal/tuitest/wait_test.go — TestWaitFailureCarriesTheFrameAndItsStyles
**Closes:** snapshot-frame-cache
**Depends on:** item 9
**Tests:** a write changes the snapshot; a resize changes the snapshot; repeated snapshots with no write return the cached frame and build no styled render (count both builds through unexported counters); a failing `WaitFor` still prints the styled frame.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tuitest/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestE2ESmokeInProcess' ./cmd/apogee/`
**Commit:** `perf(tuitest): cache the snapshot frame until the screen changes`

## 23. The e2e apogee binary is built once per suite run

**What:** Every cmd/apogee chunk's `TestMain` runs its own `go build` of apogee, 13 links per run.
**Regression guard.** the cmd/apogee race run carries `-parallel 1`. A set-but-missing `APOGEE_TEST_E2E_BINARY` makes TestMain exit non-zero with a message naming the variable and the path, and never sets `e2eBuildErr` (which would skip every PTY test green). Supersedes `docs/design/test-drivers.md` ("The binary is built once per `go test` run … TestMain … runs go build"): every prose line saying TestMain builds the binary, found by `grep -rn 'TestMain' docs/design/test-drivers.md scripts/test-shards.sh cmd/apogee/main_test.go`, is rewritten.
**Goal:** `scripts/test-shards.sh` builds `./cmd/apogee` once, non-race and without `build_args`, as `TestMain` does, beside its `go test -c` builds, and exports `APOGEE_TEST_E2E_BINARY` to the chunks. `TestMain` uses that file when the variable is set, fails loudly when it names a missing file, and builds as before when it is unset. `docs/manual/building.md` documents the variable.
**Approach (assumed at the header base):** in `cmd/apogee/main_test.go`, add the check before `buildE2EBinary`. Keep the variable out of `ambientApogeeEnv`. The script adds the build to its build-failure check, and a Windows run gets an `.exe` suffix.
**Files:** scripts/test-shards.sh, cmd/apogee/main_test.go, docs/manual/building.md, docs/design/test-drivers.md
**Read first:** cmd/apogee/main_test.go — TestMain, buildE2EBinary, ambientApogeeEnv; scripts/test-shards.sh — heavy build loop (build_pids, build_failed); cmd/apogee/e2e_support_test.go — launchPTYWithEnv; docs/design/test-drivers.md — "The binary is built once per go test run"; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride (scans non-test files only, so the new variable is safe)
**Closes:** e2e-binary-per-chunk
**Tests:** a test of the selection helper (set/existing, set/missing, unset).
**Acceptance:** `bash -n scripts/test-shards.sh`; `grep -n 'APOGEE_TEST_E2E_BINARY' scripts/test-shards.sh docs/manual/building.md` matches in both files; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestE2EBinary|TestListingOnly' ./cmd/apogee/`
**Commit:** `perf(test-shards): build the e2e apogee binary once per run`

## 24. WaitQuiet slice 1: after an armed pane, and before Quit

**What:** Recast at the regression check (2026-10-12). Class B. `WaitQuiet(settled)` stands in for "ready" where a real signal exists.
**Regression guard.** binding — a painted idle prompt (`waitIdle`) does not mean the session record is written (the idle save is a Cmd queued after idle, `saveAtIdle` in internal/tui/model.go), and Quit while busy is a cancel (`Model.quit`); so before Quit, a WaitQuiet that guards a save becomes testtime.Eventually on the saved effect, and one that only ensures idle becomes waitIdle. Acceptance gains a grep/awk check that no WaitQuiet remains between awaitArmed/WaitCursorAnswers and the answering key, nor directly before Quit() without a comment naming the write; cmd/apogee race runs carry `-parallel 1`. In addition to the record-read binding already in the item, keep the first-pass frame-read carve-out: WaitQuiet sites feeding awaitPaneMarked and the popups golden/Find waits stay, per the ratified "waits before goldens and whole-frame asserts stay" call (the item yields to it and to awaitApprovalPane's doc, cmd/apogee/e2e_approval_test.go:452, "returns the settled frame it painted"); the Acceptance check exempts those sites. The `-run` covers every caller of each shared helper the item changes (raisePane, awaitApprovalPane, awaitForcedPane, alwaysAllow, allowIfAsked, allowAndAwait), found by `grep -n 'raisePane(\|awaitApprovalPane(\|awaitForcedPane(\|alwaysAllow(\|allowIfAsked(\|allowAndAwait(' cmd/apogee/*_test.go`, not only the tests it edits.
**Goal:** in `cmd/apogee` tests:
- No `WaitQuiet` sits between an `awaitArmed` (or `WaitCursorAnswers`) and the key that answers the pane (about 8 sites, e.g. in the console, egress, reactions and smoke tests and `raisePane`).
- No `WaitQuiet` sits immediately before `Quit()` unless a comment names the write it waits for. Where it guards a save, it becomes a `WaitFor` on that save's effect.
**Approach (assumed at the header base):** find the sites with `grep -n -B3 -A2 'WaitQuiet(' cmd/apogee/*_test.go`. The two popups sites next to a golden keep their wait.
**Files:** cmd/apogee/e2e_console_test.go, cmd/apogee/e2e_egress_test.go, cmd/apogee/e2e_reactions_test.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_approval_test.go, cmd/apogee/e2e_popups_test.go, plus the files holding the pre-`Quit` sites
**Read first:** cmd/apogee/e2e_approval_test.go — awaitPaneMarked, raisePane, awaitArmed, waitIdle; cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesPrompts, TestE2EPopupClickAsk; internal/tui/model.go — Model.quit; cmd/apogee/e2e_console_test.go — alwaysAllow
**Depends on:** item 12
**Tests:** the changed tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '<every test the item changed>' ./cmd/apogee/`; a grep/awk check over `cmd/apogee/*_test.go` that no `WaitQuiet` remains between `awaitArmed`/`WaitCursorAnswers` and the answering key, nor directly before `Quit()` without a comment naming the write, exempting `awaitPaneMarked` and the popups golden/Find waits
**Commit:** `test(e2e): wait on arming and exit signals instead of a quiet screen`

## 25. WaitQuiet slice 2a: backend-state reads in e2e_[a-l]

**What:** Recast at the regression check (2026-10-12). Class B. A `WaitQuiet` followed by a read of backend state (stub requests, session record, workspace files, proxy log) stands in for "the turn has folded".
**Regression guard.** binding — a WaitQuiet guarding a read of the session record or any file written at idle becomes testtime.Eventually on that record or file (never waitIdle alone, since the idle save is queued after idle — `saveAtIdle`); waitIdle replaces a WaitQuiet only where the read needs the turn folded and nothing written at idle; Acceptance gains a grep/awk check that no WaitQuiet in its files is directly followed by a backend-state read; cmd/apogee race runs carry `-parallel 1`. In addition to the record-read binding, an absence read (asserting something did NOT happen) uses waitIdle followed by the read, per the first-pass report; the awk/grep check is defined: a backend-state read is a line (after skipping blank and comment lines, looking up to the next 6 code lines) calling any of stub Requests, childRequests, namingRequests, sessionRecord*, readWorkspaceFile, os.ReadFile, os.Stat, Saw, argvs, readHookPayloads, descendants, or the fanOut/taskList helpers.
**Goal:** in `cmd/apogee/e2e_[a-l]*_test.go`, every `WaitQuiet` whose next statement reads backend state is replaced by `waitIdle` or by a `WaitFor`/`testtime.Eventually` on the specific state read.
**Approach (assumed at the header base):** decide site by site. When the read needs the turn folded, use `waitIdle`; when it needs one record, wait on that record.
**Files:** cmd/apogee/e2e_[a-l]*_test.go
**Read first:** cmd/apogee/e2e_present_test.go — sessionRecordText; internal/session/store.go — atomicWrite; cmd/apogee/e2e_console_test.go — descendants; cmd/apogee/e2e_delegation_test.go — childRequests; cmd/apogee/e2e_fanout_test.go — fanOutParentRequests; cmd/apogee/e2e_egress_test.go — proxy.Saw; cmd/apogee/e2e_livestate_test.go — first.Requests baseline; cmd/apogee/e2e_approval_test.go — waitIdle
**Depends on:** item 24
**Tests:** the changed tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '<every test the item changed>' ./cmd/apogee/`; a grep/awk check that no `WaitQuiet` in `cmd/apogee/e2e_[a-l]*_test.go` is directly followed by a backend-state read, as the guard defines one
**Commit:** `test(e2e): wait on backend state instead of a quiet screen (a–l)`

## 26. WaitQuiet slice 2b: backend-state reads in the remaining files

**What:** Recast at the regression check (2026-10-12). Item 25's rule for `cmd/apogee/e2e_[m-z]*_test.go` and the non-e2e `cmd/apogee` tests.
**Regression guard.** binding — a WaitQuiet guarding a read of the session record or any file written at idle becomes testtime.Eventually on that record or file (never waitIdle alone, since the idle save is queued after idle — `saveAtIdle`); waitIdle replaces a WaitQuiet only where the read needs the turn folded and nothing written at idle; Acceptance gains a grep/awk check that no WaitQuiet in its files (`cmd/apogee/*_test.go`) is directly followed by a backend-state read; cmd/apogee race runs carry `-parallel 1`. In addition to the record-read binding, an absence read (asserting something did NOT happen) uses waitIdle followed by the read, per the first-pass report; the awk/grep check is defined: a backend-state read is a line (after skipping blank and comment lines, looking up to the next 6 code lines) calling any of stub Requests, childRequests, namingRequests, sessionRecord*, readWorkspaceFile, os.ReadFile, os.Stat, Saw, argvs, readHookPayloads, descendants, or the fanOut/taskList helpers.
**Goal:** no `WaitQuiet` in `cmd/apogee` tests is directly followed by a backend-state read.
**Approach (assumed at the header base):** as item 25.
**Files:** cmd/apogee/e2e_[m-z]*_test.go, cmd/apogee/*_test.go (non-e2e)
**Read first:** cmd/apogee/e2e_naming_test.go — TestE2ENamingIsSilentWithAutoTitleOff, namingRequests; cmd/apogee/e2e_tasklist_test.go — taskListRequestAfterAResult, taskListRequestCarrying; cmd/apogee/e2e_present_test.go — openerLog.argvs; cmd/apogee/e2e_seat_test.go — run.session.Requests; cmd/apogee/e2e_subagent_preempt_test.go — carriesTask loop; cmd/apogee/e2e_approval_test.go — waitIdle
**Closes:** waitquiet-readiness-proxy
**Depends on:** item 25
**Tests:** the changed tests.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '<every test the item changed>' ./cmd/apogee/`; a grep/awk check that no `WaitQuiet` in `cmd/apogee/*_test.go` is directly followed by a backend-state read, as item 25's guard defines one
**Commit:** `test(e2e): wait on backend state instead of a quiet screen (m–z)`

## 27. Measure what typing prompts costs

**What:** The typed-prompts policy gate (owner call: switch only at ≥10%).
**Regression guard.** the measured interval (Type → prompt tail visible) is the program's per-key fold cost and an upper bound on what pasting saves; the 10% threshold applies to it, and the issue records that it is an upper bound. The measured tests run under `t.Parallel`, so the accumulator is atomic (a `sync/atomic` Int64 of nanoseconds), and the denominator is the summed per-test elapsed (`go test -json`/`-v`), not the run's wall time.
**Goal:** `.capcom/issues/e2e-typed-prompts.md` records a measurement and its verdict ("paste" or "keep typing"):
- the share of e2e test time spent between `submit`/`submitNow` typing and the prompt tail appearing,
- measured over the 20 cmd/apogee e2e tests with the most `submit` calls,
- in one race run on this box.
**Approach (assumed at the header base):** add temporary timing around the `Type` → `promptTail` wait in `submit` and `submitNow` (accumulated, printed by `TestMain`), run once, record the numbers, and revert the instrumentation. Only the issue file is committed.
**Files:** .capcom/issues/e2e-typed-prompts.md
**Read first:** cmd/apogee/e2e_smoke_test.go — submit, promptTail, typingAllowance; cmd/apogee/e2e_subagent_preempt_test.go — submitNow; cmd/apogee/main_test.go — TestMain; internal/tuitest/driver.go — Driver.Type, Driver.send; .capcom/issues/e2e-typed-prompts.md
**Depends on:** item 26
**Tests:** none (a measurement).
**Acceptance:** `capcom show e2e-typed-prompts` shows the measured share and the verdict; `git diff --stat` touches only that issue file
**Commit:** `docs(issues): record the measured cost of typed e2e prompts`

## 28. Paste prompts in the e2e submit helpers

**What:** Runs only if item 27's verdict is "paste". When the verdict is "keep typing", this item is marked done-skipped with a NOTES line and the issue stays open.
**Regression guard.** cmd/apogee race runs carry `-parallel 1`. TestE2EApprovalKeysAreArmedAfterPaint calls `submit` and becomes a `typeSubmit` user, so e2e_approval_test.go joins **Files:**; e2e_copy_test.go leaves it (TestE2ECopyWritesOSC52 types through `drv.Type`, never `submit`, and needs no edit). The `-run` also covers the `submitNow` consumers (TestE2EQueuedMessage*) and the armed-after-paint test. `docs/design/test-drivers.md` names `Paste` (bracketed paste, one PasteMsg, bypasses key claims) beside `Type` in both driver input-surface lists.
**Goal:**
- `tuitest.Driver` and `PTYDriver` gain `Paste(text)`, which sends the text in bracketed paste (`\x1b[200~ … \x1b[201~`); the Windows PTY stub skips.
- `Paste` joins the `driven` interface.
- `submit` and `submitNow` paste the prompt, then wait for the prompt tail as before, and `typingAllowance` is removed.
- A `typeSubmit` helper keeps typing for the tests whose claim is about keys: the armed-after-paint tests, the popups dropdown and ask-answer tests, `TestE2ECopyWritesOSC52`, and `TestE2ESmokeInProcess`/`PTY`.
**Approach (assumed at the header base):** use the x/ansi bracketed-paste constants. Check that a pasted `/verb` followed by Enter sends the command rather than accepting the autocomplete, and that a pasted image path is still attached.
**Files:** internal/tuitest/driver.go, internal/tuitest/pty.go, internal/tuitest/pty_windows.go, cmd/apogee/e2e_smoke_test.go, cmd/apogee/e2e_subagent_preempt_test.go, cmd/apogee/e2e_support_test.go, cmd/apogee/e2e_popups_test.go, cmd/apogee/e2e_approval_test.go, docs/design/test-drivers.md
**Read first:** cmd/apogee/e2e_smoke_test.go — submit, typingAllowance, promptTail; cmd/apogee/e2e_support_test.go — driven; internal/tuitest/driver.go — Driver.Type, Driver.send; internal/tuitest/pty.go — PTYDriver.Type; internal/tui/prompteditor.go — Model.foldPaste, attachPastedImage; internal/tui/autocomplete.go — Model.autocompleteKey
**Closes:** e2e-typed-prompts
**Depends on:** item 27
**Tests:** a tuitest Paste test; a pasted `/verb` + Enter e2e test.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/tuitest/`; `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestE2E(Smoke|Popup|Copy|Usage|QueuedMessage|ApprovalKeysAreArmed)' ./cmd/apogee/`
**Commit:** `perf(e2e): paste prompts instead of typing them key by key`

## 29. scripts/ci-flakes.sh tallies CI failures

**What:** Makes "is it a flake?" a lookup instead of a guess (review §8, lever 10).
**Goal:** `scripts/ci-flakes.sh [--since YYYY-MM-DD] [--workflow ci.yml]` lists failed runs with `gh`, fetches each failed job's log with `gh api repos/{owner}/{repo}/actions/jobs/<id>/logs`, and prints two tables: per run (date, sha, job, failing tests) and aggregated (test, package, platform, count, dates). Non-test job failures are named by the failing step. `docs/manual/building.md` names the script.
**Approach (assumed at the header base):** bash, safe under bash 3.2. Logs are cached under `${TMPDIR:-/tmp}`. Parse `--- FAIL`, the `FAIL\tpkg` lines and `DATA RACE`. Read the repo name from `gh repo view --json nameWithOwner`.
**Files:** scripts/ci-flakes.sh, docs/manual/building.md
**Read first:** scripts/check-pins.sh — header style, set -euo pipefail; docs/manual/building.md — test-shards and check-pins paragraphs; .gitattributes — scripts/*.sh eol=lf; .github/workflows/ci.yml; internal/platform/conpty_windows_test.go — TestStartPseudoConsoleRunsTheCommand
**Closes:** ci-flake-tally
**Tests:** `bash -n`, plus a run against the live repo.
**Acceptance:** `bash -n scripts/ci-flakes.sh`; `scripts/ci-flakes.sh --since 2026-10-10 | grep -c 'TestStartPseudoConsoleRunsTheCommand'` ≥ 1
**Commit:** `build: add a CI failure tally script`

## 30. Test docs follow the new discipline; file the never-filed findings

**What:** The prose and the register catch up with items 6–28. Every doc line that describes a test wait rule, `WaitQuiet` as readiness, `APOGEE_TEST_SLOW` semantics or "a bounded half-second per walk" is updated; find them with `grep -rn 'WaitQuiet\|half-second\|APOGEE_TEST_SLOW\|time.Sleep' docs/design docs/manual AGENTS.md README.md --exclude-dir=archived`.
**Regression guard.** the sweep covers living docs only — `grep -rn 'WaitQuiet\|half-second\|APOGEE_TEST_SLOW\|time.Sleep' docs/design docs/manual AGENTS.md README.md --exclude-dir=archived` — never docs/plans, docs/reviews or docs/handoffs, which are closed records. The stubllm run is `-run 'DesignDoc'` (TestDesignDocExampleScriptParses parses test-drivers.md's `## stubllm` section; no stubllm test matches Manual|Readme).
**Goal:**
- `docs/design/test-drivers.md` §Waiting states the testtime rule (Budget/Fixed/Eventually, the guard), and says `WaitQuiet` is only for "the frame has settled" before a capture, never for readiness.
- No doc claims a half-second walk.
- capcom holds these new issues:
  - `esc-disarm-fold-time` (the esc×2 window is measured at fold time, not at the keypress);
  - `demorig-eperm-root-cause` (closed with a retry, cause unknown);
  - `default-wire-windows-arm64-flake` (`TestE2EDefaultWireStaysChatCompletions`, about 1 in 3 on windows/arm64);
  - `conpty-reap-join-bound` (production `reap` joins the reader for only 5 s before `CloseOutput`);
  - `filewatch-stall-test-skipped` (`7929556d` skips instead of fixing).
**Approach (assumed at the header base):** `capcom create --id <slug> --title <title>`, each body citing the review's section.
**Files:** docs/design/test-drivers.md, docs/manual/building.md, AGENTS.md, .capcom/issues/
**Read first:** docs/design/test-drivers.md — §Waiting, the "half-second per walk" line, ## stubllm; internal/stubllm/script_test.go — TestDesignDocExampleScriptParses, designDocExample; docs/manual/building.md — APOGEE_TEST_SLOW knobs; AGENTS.md — make test knobs paragraph; internal/tools/manual_drift_test.go — TestReadmeStatesTheToolCounts
**Depends on:** items 20, 26 and 28
**Tests:** `TestReadmeStatesTheToolCounts` and the other doc-drift tests stay green.
**Acceptance:** `grep -rn 'half-second' docs/design/test-drivers.md` is empty; `capcom show esc-disarm-fold-time`; `GOMEMLIMIT=2GiB go test -count=1 -run 'Manual|Readme' ./internal/tools/ ./internal/config/ ./internal/domain/`; `GOMEMLIMIT=2GiB go test -count=1 -run 'DesignDoc' ./internal/stubllm/`
**Commit:** `docs: state the test timing discipline and file open timing findings`

## 31. Refresh the shard timings seed

**What:** Items 21 and 28 change test names and costs; CI packs by the committed seed.
**Regression guard.** the seed check greps `OverlayHeightQuery` without the `Test` prefix, so a correct seed passes whatever prefix item 21's split names carry.
**Goal:** `scripts/test-timings.seed` is regenerated from a full slow-mode run of the finished tree, and lists the split tui tests.
**Approach (assumed at the header base):** `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`, then `make test-timings-seed`.
**Files:** scripts/test-timings.seed
**Read first:** Makefile — test-timings-seed; scripts/test-shards.sh — TIMINGS, TIMINGS_SEED, is_slow_box; scripts/test-timings.seed; internal/tui/panes_test.go — TestOverlayHeightQueryMatchesItsRender, paneFixtures
**Depends on:** items 21 through 30
**Tests:** the full suite run itself.
**Acceptance:** `grep -c 'OverlayHeightQuery' scripts/test-timings.seed` ≥ 2; `git diff --stat scripts/test-timings.seed` is non-empty
**Commit:** `build(test-shards): refresh the shard-timings seed`
