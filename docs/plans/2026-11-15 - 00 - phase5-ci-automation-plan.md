# Phase-5 CI automation — the lock, job-breakaway and seatbelt `/dev/null` checks

**Goal:** Turn three owner-run Phase-5 residuals into tests CI runs: the single-instance lock is
freed when its holder dies (every OS), a descendant cannot leave the Windows teardown job, and the
seatbelt `/dev/null` exemption holds live on macOS. A new non-gating macOS workflow runs the
darwin tests that no CI has run before.

**Date:** 2026-11-15
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 0773d284

**Sources:**
- `.capcom/issues/phase5-owner-verification.md` and its children `single-instance-lock-check`,
  `windows-job-breakaway`, `macos-confined-live-run`
- `internal/platform/lock.go`, `teardown_windows.go`, `seatbelt.go`; `.github/workflows/latest-go.yml`
- ADR 0098 (the release gate reads `ci.yml` alone), ADR 0020 (Windows confinement)

**Ratified design calls:**
- **macOS job home:** its own workflow `.github/workflows/macos.yml`, never a `ci.yml` job, so it
  never gates a release (owner, 2026-11-15).
- **Issue closing:** this plan closes no issue. Each issue names the CI job that proves it and the
  owner closes it after the first green run following the push (owner, 2026-11-15).

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi 5 host: one package and one test per run (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/platform/`), never two test runs at once, never a package pattern.
- Every test duration goes through `internal/testtime` (`Budget`, `Fixed`, `Eventually`); its AST guard enforces it.
- Windows- and darwin-tagged tests cannot run here: their Acceptance is a cross-compile of the test binary (`GOOS=<os> go vet` plus `GOOS=<os> go test -c -o /dev/null`). CI runs them after the owner pushes.

**Out of scope:**
- The owner-run remainder: `macos-confined-live-run`'s live session, `windows-confined-live-run`, `windows-conpty-console-check`, `windows-below-floor-notice`.
- Moving the macOS job into `ci.yml`; running the whole suite on macOS.
- Changing the job-object limits or the lock implementation (tests only, plus CI and docs).

**Regression check (2026-11-15, 0773d284):**
- 1: context line added (`single-instance-lock-check`; closes no issue).
- 2: guard folded (assert the child joined the job before `go`); context line added (`windows-job-breakaway`).
- 3: guard folded (loud-skip twin in `seatbelt_notdarwin_test.go`); context line added (`macos-confined-live-run`).
- 4: guard folded (runner and step Acceptance greps, Goal reworded to the real triggers, step comment corrected); context line added (`single-instance-lock-check`, `macos-confined-live-run`).

## 1. The lock is freed when its holding process dies

**What:**
**Context:** capcom issue `single-instance-lock-check` (the holder-dies half; its Windows contention
cases already run in `ci.yml`'s `test-windows` job).
**Goal:** an untagged test in `internal/platform` proves, on every OS it runs on, that a lock held
by a separate process is refused to this one while that process lives (`*LockHeldError`, and
`ProbeLock` reports it held with that process's PID) and is acquirable once that process is killed.

**Approach (assumed at the header base):** a new file `internal/platform/lock_holder_test.go`.
The holder is the test binary re-executed with `-test.run=^TestLockHolderHelper$` and an env var
(`APOGEE_LOCKTEST_HOLDER=<lock path>`), the `confinetest.RunHelper` shape; the stub test returns
at once when the env var is unset. No `TestMain` change (Windows and Linux each own one). The
helper calls `AcquireLock`, writes one `held` line to stdout, then blocks reading stdin — never a
sleep. The parent waits for `held` within a `testtime.Budget`, asserts the refusal and the probe,
calls `Process.Kill` then `Wait`, and polls `AcquireLock` with `testtime.Eventually` (a killed
process's handle closes asynchronously on Windows). Release every acquired lock with `t.Cleanup`.
`lock_test.go`'s header says it needs no subprocess scaffolding; amend that comment to point at
this file as the one deliberate exception.

**Files:** internal/platform/lock_holder_test.go, internal/platform/lock_test.go
**Read first:** internal/platform/lock.go — AcquireLock, ProbeLock, LockHeldError, writeLockPID; internal/platform/lock_test.go — lockPath, acquire, TestAcquireLockRefusesASecondHolder; internal/platform/confinetest/confinetest.go — RunHelper, HelperTestName;
internal/platform/landlock_linux_test.go — TestMain; internal/platform/platform_windows_test.go — TestMain; internal/platform/lock_windows.go — unlockFile; internal/testtime/testtime.go — Budget, Eventually

**Tests:** `TestAcquireLockFreedWhenHolderDies` (new).

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAcquireLockFreedWhenHolderDies|TestLockHolderHelper' ./internal/platform/` → ok
- `GOOS=windows go vet ./internal/platform/ && GOOS=darwin go vet ./internal/platform/` → clean
- `GOOS=windows go test -c -o /dev/null ./internal/platform/ && GOOS=darwin go test -c -o /dev/null ./internal/platform/` → ok

**Commit:** `test(platform): assert the single-instance lock is freed when its holder dies`

## 2. A descendant cannot break away from the Windows teardown job

**What:**
**Context:** capcom issue `windows-job-breakaway`.
**Goal:** a Windows-tagged test in `internal/platform` proves that a process contained by the
production `os/exec` teardown path gets `ERROR_ACCESS_DENIED` when it calls `CreateProcess` with
`CREATE_BREAKAWAY_FROM_JOB` — the Windows twin of `TestTeardownDoesNotReachASetsidEscapee`.

**Approach (assumed at the header base):** a new file `internal/platform/teardown_windows_test.go`
(`//go:build windows`). The child is the test binary re-executed with
`-test.run=^TestBreakawayHelper$` and `APOGEE_BREAKAWAY_HELPER=1` (no `TestMain` change). The
parent builds the `exec.Cmd` with stdin and stdout pipes and contains it through
`NewProcessTeardown` and the same start-then-`Contain` calls `RunWithTeardown` makes. Only after
containment does it write one `go` line to the child's stdin; the child waits for it, because
os/exec has a documented window before `Contain`. The child then calls `windows.CreateProcess` on
`cmd.exe /c exit 0` with `CREATE_BREAKAWAY_FROM_JOB|CREATE_SUSPENDED` and prints the error code.
If the call succeeds, it terminates that process and reports success. The parent asserts the
reported code is exactly `ERROR_ACCESS_DENIED`; any other result fails, including success. Tear
the child down through the teardown's own path, with every wait bounded by `testtime`. Reuse
helpers from `conpty_windows_test.go` (same package) where they fit; add no production code.

**Regression guard.** `Contain` swallows every error (`teardown_windows.go` `Contain`/`containHandle`),
so after `Contain` and before writing `go`, assert `td.(*jobTeardown).job != windows.InvalidHandle`
and `.assigned`, as `pollJob` does (`conpty_windows_test.go`); otherwise a runner whose test process
already sits in a no-breakaway job would pass without proving the teardown job.

**Files:** internal/platform/teardown_windows_test.go
**Read first:** internal/platform/teardown_windows.go — NewProcessTeardown, jobTeardown.Contain, containHandle, Reap, Release; internal/platform/teardown.go — RunWithTeardown, ProcessTeardown;
internal/platform/conpty_windows_test.go — pollJob, conptyTestTimeout, TestPseudoConsoleKillReapsTheTree; internal/platform/teardown_unix_test.go — TestTeardownDoesNotReachASetsidEscapee; internal/platform/platform_windows_test.go — TestMain

**Tests:** `TestTeardownJobRefusesBreakaway` (new), failing when the child never joined the job.

**Acceptance:**
- `GOOS=windows go vet ./internal/platform/` → clean
- `GOOS=windows go test -c -o /dev/null ./internal/platform/` → ok
- `grep -n 'CREATE_BREAKAWAY_FROM_JOB' internal/platform/teardown_windows_test.go` → present

**Commit:** `test(platform): assert a contained process cannot break away from the Windows job`

## 3. The seatbelt `/dev/null` exemption, live on macOS

**What:**
**Context:** capcom issue `macos-confined-live-run` (its `/dev/null` half only; the live session
stays owner-run).
**Goal:** a darwin-tagged test proves under live `sandbox-exec` that a confined shell can write
`/dev/null` while a write outside the box is still denied and leaves no file — the seatbelt twin
of `TestLandlockAllowsDevNullThroughTheFence`.

**Approach (assumed at the header base):** add `TestSeatbeltAllowsDevNullThroughTheFence` to
`internal/platform/seatbelt_darwin_test.go`, with the landlock test's two subtests
(`dev_null_write_succeeds` running `: 2>/dev/null && echo x > /dev/null`, and
`out_of_box_write_still_denied`). Build the confiner as `TestSeatbeltProbe` does
(`newSeatbeltConfiner(sandboxExecPresent())`) and self-skip on the same condition it does
(`Capabilities().FSWrite` false). Port `runConfinedShellLine` as a darwin-local helper typed to
`*seatbeltConfiner`; do not widen the Linux helper.

**Regression guard.** Add a loud-skipping `TestSeatbeltAllowsDevNullThroughTheFence` twin to
`internal/platform/seatbelt_notdarwin_test.go`, in the shape of its `TestSeatbeltProbe` skip, so the
macOS deferral of the new test stays visible in Linux `go test` output.

**Files:** internal/platform/seatbelt_darwin_test.go, internal/platform/seatbelt_notdarwin_test.go
**Read first:** internal/platform/landlock_linux_test.go — TestLandlockAllowsDevNullThroughTheFence, runConfinedShellLine; internal/platform/seatbelt_darwin_test.go — TestSeatbeltProbe; internal/platform/seatbelt.go — newSeatbeltConfiner, Capabilities, Confine, seatbeltProfile;
internal/platform/seatbelt_darwin.go — sandboxExecPresent; internal/platform/seatbelt_notdarwin_test.go — TestSeatbeltProbe; internal/platform/seatbelt_test.go — TestSeatbeltProfileAllowsDevNull

**Tests:** `TestSeatbeltAllowsDevNullThroughTheFence` (new, darwin), plus its loud-skip twin of the same name in `seatbelt_notdarwin_test.go` (new).

**Acceptance:**
- `GOOS=darwin go vet ./internal/platform/` → clean
- `GOOS=darwin go test -c -o /dev/null ./internal/platform/` → ok
- `GOMEMLIMIT=2GiB go test -count=1 -run TestSeatbeltProfileAllowsDevNull ./internal/platform/` → ok (the hermetic twin is untouched)
- `GOMEMLIMIT=2GiB go test -count=1 -v -run '^TestSeatbeltAllowsDevNullThroughTheFence$' ./internal/platform/` → SKIP with its reason (the Linux twin)

**Commit:** `test(platform): probe the seatbelt /dev/null exemption under live sandbox-exec`

## 4. A non-gating macOS workflow

**What:**
**Context:** capcom issues `single-instance-lock-check` and `macos-confined-live-run` (their macOS
halves run in this workflow).
**Goal:** `.github/workflows/macos.yml` runs the `internal/platform` and `internal/gitexec` tests
with `-race` on a macOS runner on pushes to `main` (`latest-go.yml`'s `paths-ignore`) and on PRs, and `docs/manual/building.md` describes
it instead of saying there is no macOS runner.

**Approach (assumed at the header base):** model the file on `latest-go.yml`. Use its header
comment's argument (a workflow of its own because `cmd/release/gate.go` reads `ci.yml` runs
alone; ADR 0098), its triggers and `paths-ignore`, `permissions: contents: read`, and the SHA-pinned
`actions/checkout` and `actions/setup-go` lines with `persist-credentials: false` copied from
`ci.yml`. Add `go-version-file: go.mod`. One job, `test-macos`, on `runs-on: macos-latest`, one
step: `go test -race -count=1 -p 1 -timeout 30m ./internal/platform/... ./internal/gitexec/...`.
The step's comment says these trees hold the darwin-only tests (the live seatbelt battery and
item 3's twin, item 1's holder test, the ctime fileprint test) and that `-p 1` keeps the
sandbox-exec batteries serial. In `docs/manual/building.md`, replace the sentence "There is no
macOS runner (darwin is only cross-compiled)." with one naming `macos.yml`, what it runs and that
it does not gate a release. Leave `ci.yml` and `ciPushPaths` untouched.

**Regression guard.** The Acceptance greps below pin `runs-on: macos-latest` and the race step, since
actionlint, the pin check and the drift test all pass on a macos.yml that runs nothing darwin. The step
comment names "the darwin-tagged tests (seatbelt live battery plus item 3's twin) and the darwin paths of
tests every OS runs (item 1's holder test, gitexec's ctime fileprint via ctime_darwin.go)", not "darwin-only".

**Files:** .github/workflows/macos.yml, docs/manual/building.md
**Read first:** .github/workflows/latest-go.yml — header comment, on.push.paths-ignore, setup-go cache-dependency-path; .github/workflows/ci.yml — test-windows "go test (race)" step (-p 1 -timeout 30m), checkout/setup-go SHA pins with persist-credentials: false and go-version-file; docs/manual/building.md — "A second workflow, latest-go.yml" paragraph, "CI's windows-latest job" paragraph;
cmd/release/gate.go — ciWorkflow, hasGreenRun; cmd/release/cifilter_test.go — TestCIPushPathsMatchCIYAML; scripts/check-pins.sh — pinned;
internal/platform/seatbelt_darwin_test.go — TestSeatbeltProbe, TestSeatbeltProbeNetwork

**Tests:** none (workflow config); `make actionlint` and the pin check stand in.

**Acceptance:**
- `make actionlint` → clean
- `sh scripts/check-pins.sh` → ok
- `grep -c 'There is no macOS runner' docs/manual/building.md` → 0; `grep -n 'macos.yml' docs/manual/building.md` → present
- `GOMEMLIMIT=2GiB go test -count=1 -run TestCIPushPathsMatchCIYAML ./cmd/release/` → ok (the `ci.yml` filter drift test still holds)
- `grep -n 'runs-on: macos-latest' .github/workflows/macos.yml` → present
- `grep -n 'go test -race .*-p 1 .*\./internal/platform/\.\.\. \./internal/gitexec/\.\.\.' .github/workflows/macos.yml` → present

**Commit:** `ci: run the darwin platform tests on a non-gating macOS workflow`
