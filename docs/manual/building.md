# Building from source

Not the only way in any more — [Install](../../README.md#install) has Homebrew, Scoop, winget
(not live yet: its first submission still awaits review in `microsoft/winget-pkgs`) and prebuilt
archives — but it stays the shortest path to the tip of `main`.

**Prerequisites:** Go 1.26.10 or later (the toolchain version pinned in `go.mod`).

```bash
git clone https://github.com/airiclenz/apogee.git
cd apogee
make build      # compiles ./apogee
./apogee --help
```

A `Makefile` wraps the common Go invocations:

| Command | Does |
|---|---|
| `make build` | Compile the binary to `./apogee` |
| `make install` | Build, then copy the binary to a directory on your `PATH` |
| `make run ARGS="--help"` | Build-and-run, passing flags via `ARGS` |
| `make stubllm` | Compile the scripted test upstream to `./stubllm` — a dev tool, never a release asset |
| `make sessionexpect` | Compile the session-entry expect judge to `./sessionexpect` — a dev tool, never a release asset; it judges the newest saved session for photon's `run:` expects (`./sessionexpect --help`) |
| `make test` | Run the test suite with the race detector, as a queue of short-lived processes over a few lanes (see [Testing](#testing)); `ARGS="..."` passes extra `go test` flags to every process |
| `make test-timings-seed` | Copy the last run's `.test-timings` into the committed `scripts/test-timings.seed` the runner packs by on a fresh checkout (see [Testing](#testing)) |
| `make live-eval` | Run the opt-in live-model eval and the judge tests against a real server, always `-count=1`; `LIVE_ENDPOINT=` (default `http://127.0.0.1:1111`) becomes `APOGEE_LIVE_ENDPOINT` and `JUDGE_ENDPOINT=` (default the same) `APOGEE_JUDGE_ENDPOINT`, with `APOGEE_LIVE_MODEL` / `APOGEE_JUDGE_MODEL` set in the environment to pin the models; fails if the real `~/.apogee` grew during the run |
| `make home-census` | Print the entry counts of the real `~/.apogee` sessions and scratch dirs (what `live-eval` compares) |
| `make fmt` | `gofmt -w` over the tree |
| `make vet` | `go vet ./...` |
| `make lint` | Run `golangci-lint` (the standard linter set, configured by `.golangci.yml`) over the module |
| `make vulncheck` | Run `govulncheck` over the dependency graph — needs the network |
| `make actionlint` | Lint the GitHub workflow files with the pinned `actionlint` |
| `make cross` | Cross-compile every package for all six release targets (Linux/macOS/Windows × amd64/arm64), as a check |
| `make dist` | Build the publishable release archives into `dist/`, plus `SHA256SUMS` — a wrapper over `go run ./cmd/release dist` |
| `make check` | The full acceptance gate — gofmt, `GOOS=windows go vet ./...` over the whole module, `golangci-lint`, build, `govulncheck`, race tests, the workflow pin check and `actionlint`, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
| `make release-status [VERSION=vX.Y.Z]` | Report the release gate, the baseline and each publish step — a wrapper over `go run ./cmd/release status` (see [Releasing](#releasing)) |
| `make release-smoke [VERSION=vX.Y.Z]` | Verify a **published** release from the outside — a wrapper over `go run ./cmd/release smoke`, which `publish` also runs last (see [Releasing](#releasing)) |
| `make clean` | Remove the built binary |
| `make help` | List every target |

The built binary also carries Cobra's `apogee completion <shell>`, which prints a
shell-completion script for `bash`, `zsh`, `fish` or `powershell`.

To run `apogee` from anywhere, `make install` copies the built binary to the first
directory that is both on your `PATH` and writable without `sudo`, trying
`/usr/local/bin`, your Go bin dir (`go env GOBIN`, else `$(go env GOPATH)/bin`),
`~/.local/bin`, `/opt/homebrew/bin` and `~/bin` in that order. It never installs
somewhere your shell cannot find it: if nothing qualifies — the usual case on macOS,
where `/usr/local/bin` belongs to root — it stops and prints the two ways to finish,
either `sudo install -m 0755 ./apogee /usr/local/bin/apogee` or an explicit
`make install PREFIX=~/.local/bin` plus the line that puts that directory on your
`PATH`. `PREFIX` overrides the search entirely.

No clone at all? `go install github.com/airiclenz/apogee/cmd/apogee@main` builds and
installs straight from the tip of `main` into your Go bin dir; `@latest` installs the
current release, and `@<sha>` pins a commit. (The retired `v1.x` module versions that
proxy.golang.org retains immutably are retracted in `go.mod`, so `@latest` no longer
resolves to them.) The `retract [v1.0.0, v1.8.0]` block was published in the
self-retracting `v1.8.0` tag, whose only job is that retraction. Go reads a retraction from
the `go.mod` of the *highest* version, so it could not ride a 0.x tag. Keep the block in
`go.mod`, and never publish a version above v1.8.0 without it.

**Versions and tags.** The top-level `VERSION` file is the single source of truth for the
release version — one line, carrying the leading `v` (`vX.Y.Z`); the `dist` step strips
that `v` for the archive names and nothing else re-states the number. Pushing a commit that
changes `VERSION` to `main` is what creates the tag: a CI workflow
(`.github/workflows/tag-on-version-bump.yml`) puts an **annotated** tag on that exact
commit, named verbatim from the file, one per bump the push carries. So a version bump is
always a commit of its own and, at a release cut, the *last* one — the `CHANGELOG.md`
rollup lands first, so the tree the tag pins already contains it. CI creates the tag and
nothing more: the GitHub Release on top of it, and every package channel after that, are
published by `go run ./cmd/release publish` (see [Releasing](#releasing)).

## Setting up a clone

Two pieces of git setup are needed only by a clone you commit from, each done once.

**The capcom merge driver.** Issues live as markdown files under `.capcom/issues/`, and
`.gitattributes` hands `.capcom/issues/**/*.md` to capcom's `capcom-log` merge driver. Git
runs a merge driver only once config defines it, so define it once per machine:

```bash
git config --global merge.capcom-log.name "capcom Log merge"
git config --global merge.capcom-log.driver "capcom merge-driver %O %A %B %P %S %X %Y"
```

Always `--global`, never repo-local. `capcom init --merge-driver` prints these lines without
the flag, but apogee's git tools refuse to run in a repository whose own config defines a
`merge.*.driver` (`internal/gitexec`, `CommandConfigRefusal`).

**The commit-msg hook.** Hooks live in the tracked `.githooks/` directory, not `.git/hooks/`.
It holds one hook, `commit-msg`, which refuses any commit message carrying an AI attribution
line (a `Co-Authored-By` naming an AI, a `Claude-Session:` line, a "Generated with Claude"
footer). It is the backstop for the no-attribution rule, not the rule itself. Activate it with
`git config core.hooksPath .githooks`; a fresh clone has not run that, so check
`git config --get core.hooksPath` before relying on the hook. A clone still pointing at the old
beads hook directory is repointed by the same command. Activating it is a trust decision: from
then on every `git commit`, `checkout` and `push` runs this checkout's shell with your full user
privileges, and apogee fences none of it (`SECURITY.md`, "What counts"). Activate it only in a
checkout you trust, and in a clone of a fork or of a repository you do not control, read
`.githooks/` first. A checkout may keep a byte-identical copy of `.githooks/commit-msg` at
`.git/hooks/commit-msg` as a dormant fallback for when `core.hooksPath` is unset.

## Testing

`make test` is the supported way to run the suite, and the shape to reach for when verifying
work: it splits the suite into short-lived processes and bounds how many tests run at once. A
raw multi-package `go test` — `./...`, or several packages named on one line — does neither.
`go test` runs packages concurrently and gives each of them the whole box for its own
`t.Parallel` fan-out, so the driven tests in `cmd/apogee` and `internal/tui` end up competing
with their neighbours and report that load as a failure. To debug one test, narrow to its
package instead: `go test -race -count=1 -run TestE2ESmokeInProcess ./cmd/apogee/`.

`make test` does not run `go test ./...` in one process. Almost all of the suite's wall time
is in two packages — `cmd/apogee` and `internal/tui` — and both are `t.Parallel` inside.
In `cmd/apogee` the e2e tests are parallel by default: `tuitest.CheckLeaks` attributes
goroutines to the test that started them, and the launch helpers neither `t.Setenv` (the
testing package forbids that alongside `t.Parallel`) nor swap a package-level seam.
`internal/tui`'s driver tests are parallel too: each builds its own `Model` and stub upstream,
and the paint cache, transcript arrays and textarea value the driver helpers touch belong to
that model. In both packages only a test that reaches `t.Setenv` or a package-level seam
itself — directly or through a helper — stays serial, and the testing package runs those
before it releases the parallel ones; a guard in each package (`seams_guard_test.go`) fails
any parallel test that swaps a seam, so the exception cannot creep back in unnoticed.

`scripts/test-shards.sh` runs the suite as a queue of short-lived test processes spread over a
fixed number of **lanes**. It builds each of the two heavy packages' test binaries once
(`go test -c`), reads each binary's roster from `-test.list`, and packs the tests into
**chunks** of about two minutes of measured test time and at most 400 tests, longest first.
Every other package is packed whole into **batches** by the same measure. Chunks and batches
form one queue, longest first, and each lane takes the next item, runs it as a fresh process,
and takes another when that process exits. A chunk is the prebuilt binary run in its package
directory, as `go test` runs it, with `-test.run '^(TestA|TestB|…)$'`. A batch is
`go test -p 1` over its packages. The run is bounded by the busiest lane rather than the
slowest package.

Short-lived processes are what bound the suite's memory. A test process grows with the number
of tests it holds, not just with the test running now. The testing package parks every parallel
test at its `t.Parallel` call until the serial ones are done, and each parked goroutine keeps the
stack its test function's frame needs. In `internal/tui` those frames run to hundreds of KB,
because a `Model` is about 120 KB by value. The race detector then shadows all of that again.
One race-enabled process per package grew to several GB this way; a chunk holds a fraction of
the package and gives it all back when it exits. When you add a table of parallel subtests
whose cases build a `Model`, start each case through `runParallelCase`
(`internal/tui/sessions_test.go`). It parks the case on a small stack and takes the body's
frame only when the case runs.

`GOMEMLIMIT` does not bound a race-enabled test process. It counts only the memory the Go
runtime manages: the heap and goroutine stacks. The race detector's shadow memory is allocated
outside it and is typically several times the program's own. Bound memory by running fewer,
shorter processes, which the lanes and chunks above do; don't rely on a lower `GOMEMLIMIT`.

Every test runs as `go test` would run it: nothing is skipped or weakened, and every process
gets the same flags except for three the script sets itself. `-run` only picks a chunk's tests.
One difference from `go test`: a prebuilt binary does not run `go test`'s vet subset first.
golangci-lint's govet covers that in `make check` and in CI.

The first flag is `-parallel`. `go test` runs a package's `t.Parallel` tests GOMAXPROCS at a
time by default, as if it had the box to itself. A process here does not have the box to itself.
If every lane also fanned its tests out that wide, lanes × GOMAXPROCS driven e2e tests would
run on the box at once. That is load, not logic, and it is what makes the kit's waits time out
and leak checks catch goroutines still unwinding. So the script divides its process budget
(one per core, minus one, between 2 and 7) among the lanes, and passes each process its share
as `-parallel`. The share is 1 whenever the lanes fill the budget, which is the default sizing
and CI's. A batch of light packages takes at least 2, since those tests mostly sleep. The bound
is the script's, not the tests'. The isolated `go test -race -count=1 ./cmd/apogee/` keeps
`go test`'s default and the whole box, which is where the `cmd/apogee` sweep's 202s → 66s
(measured 2026-09-14) shows.

The second flag is `-timeout 30m`. `go test`'s default alarm is 10 minutes per test binary,
sized for a binary that fans its tests out. A chunk is sized by timings from whichever box
measured them, and a slower box can take several times as long. 30 minutes covers that and
still ends a real hang with a goroutine dump. A `-timeout` of your own wins
(`make test ARGS=-timeout=1h`), since the extra flags come after the script's. A chunk's binary
takes the test flags among `ARGS` in its own spelling (`-test.timeout=1h`). Every other flag,
such as `-tags`, goes to the `go test -c` that builds it.

Chunks and batches are balanced from the previous run's per-test durations, cached in
`.test-timings` (gitignored, rewritten every run). A test costs the larger of its own duration
and the sum of its direct subtests' durations. A parent whose subtests call `t.Parallel`
returns before they run, so its own line reads 0.00 s however long its subtests take. A checkout
that has never run the suite (a CI runner, every time) has no cache. The script then falls back
to the committed `scripts/test-timings.seed` (same format) and packs by those. The run says
which it read (`test-shards: timings: …` on stderr). A missing or stale cache costs only a less
even split, never a skipped test. The roster comes from each binary's `-test.list`, and the
script refuses to run a plan that does not cover every listed test. The seed goes stale as the
suite's shape moves. Refresh it with `make test-timings-seed` after a `make test` on a dev box
(it copies `.test-timings` into the seed), and commit the result.

A failing chunk reports the failing tests' full output, just as `go test` does. A process that
dies of a signal prints no verdict of its own. The script reports the signal and the tests the
process was running: on Linux, a SIGKILL there is usually the kernel's OOM killer, so check
`dmesg`. A passing run prints, per heavy package and for the batches, how many processes ran and
the slowest of them, plus the run's wall time.

The knobs:

| Variable | Effect |
| --- | --- |
| `APOGEE_TEST_LANES=n` | How many test processes run at once. Default: the process budget above. |
| `APOGEE_TEST_SHARDS=n` | The older knob, from when each heavy package ran as n long-lived shards. It is now read as `APOGEE_TEST_LANES` = n per heavy package + 1, the process count those shards came to. `APOGEE_TEST_LANES` wins. |
| `APOGEE_TEST_SLOW=1` | The slow-box plan: four lanes on a box with 7 GiB of RAM or more, three below it (an explicit lane count still wins), every process `-parallel 1`. It scales the test budgets as well as the concurrency: unless `APOGEE_TEST_TIME_SCALE` is set, it sets it to 2. |
| `APOGEE_TEST_RACE=0` | Drops the race detector (see below). |
| `APOGEE_TEST_TIME_SCALE=x` | Multiplies every test budget — each deadline or wait that ends on a condition — by x, a number above 0 (default 1). Durations whose length is what a test claims are never scaled. The `==>` summary line names any scale other than 1. |
| `APOGEE_TEST_E2E_BINARY=path` | The apogee binary `cmd/apogee`'s PTY tests drive. The script sets it, not you: it builds `./cmd/apogee` once per run, without `-race` and as it ships, and hands that file to every chunk, where each chunk would otherwise build its own. A plain `go test ./cmd/apogee/` with it unset builds the binary itself, as before. Set to a path that names no file, it fails the test run at startup with a message naming the variable and the path. |

`go test -race -count=1 ./...` remains the equivalent single-process run (every test, nothing
skipped), but it is not the supported shape. It hands every package the box at once, which is
the load the lanes exist to avoid. When bisecting or debugging, name the package and the test
(`go test -race -count=1 -run TestX ./cmd/apogee/`) rather than the whole tree. CI runs
`make test` with no lane knob set, so the lanes come from `nproc` as on every other box: a hosted
runner is a 4 vCPU box, which gives three lanes, each a race-enabled process with its own memory
cost.

`APOGEE_TEST_SLOW=1` makes the same script runnable on a box the default plan overloads, such as
a Raspberry Pi 4 or 5. It is explicit rather than sized off the core count, because a Pi and
CI's 4 vCPU runner report the same `nproc` and differ 3–5× per core. Slow mode doubles the test
budgets too (`APOGEE_TEST_TIME_SCALE=2`, unless you set a scale yourself), because a slow box's
waits run long as well as its processes. `APOGEE_TEST_RACE=0` drops
the race detector. The stock Raspberry Pi OS arm64 kernels give 39-bit (Pi 4) or 47-bit (Pi 5)
virtual addresses, and TSan requires 48 (`FATAL: Found 39 - Supported 48`), so no `-race` binary
runs on either. An unraced run announces itself on stderr and in its `==>` summary line, and it
is not the `make check` gate: `make check` refuses to run with `APOGEE_TEST_RACE=0` set. Run
race-enabled verification on another box.

`APOGEE_REQUIRE_LANDLOCK_NET=1` goes the other way: it makes a skip a failure. The landlock
network test, `TestLandlockProbeNetwork` in `internal/platform`, normally skips on a host that
cannot run it: no landlock, a landlock ABI below 4 (no network rights), or no `bash` for the UDP
row's `/dev/udp` send. With the variable set, each of those is a test failure with the same
reason, so a run can prove the UDP arm (confinement-execution-contract §6.2, row #13) actually
ran. The test logs the probed landlock ABI whether the variable is set or not. CI's `check` job
sets it in a step of its own, after `make test`:
`go test -count=1 -v -run '^TestLandlockProbeNetwork$' ./internal/platform/`, which must also
print `--- PASS: TestLandlockProbeNetwork/udp_egress_under_network_deny`. Leave it unset on a
dev box whose kernel lacks landlock ABI 4, where the skip is the honest result.

To tell a flake from a real failure, `scripts/ci-flakes.sh [--since YYYY-MM-DD]
[--workflow ci.yml] [--limit N]` tallies what failed across CI's failed runs. It needs `gh`,
authenticated for the repository. It lists the failed runs, fetches each failed job's log
(cached under `${TMPDIR:-/tmp}/apogee-ci-flakes/`, so a second pass is quick), and prints two
tables: one row per failed job with its date, commit and failing tests, then one row per
failure with its package, platform, count and dates. A test that fails on several commits that
did not touch it is the flake. A data race and a test timeout are tagged `[race]` and
`[timeout]`. A job that failed outside any test, such as lint, vet or a cross-compile, is named
by its failing step. A run that was re-run to green no longer counts as failed, so its first
attempt is not in the tally.

## Releasing

The release process lives in this repo and needs nothing outside it
([ADR 0098](../../.capcom/knowledge/decisions/adr-0098.md)): a clone, the Go toolchain,
`git`, `gh` logged in with push access to this repository, the Homebrew tap and the Scoop
bucket, and — for winget — `komac`. It runs the same from Linux, macOS or Windows. The work
is split in two. A Go maintainer program, `cmd/release` (run as `go run ./cmd/release <verb>`,
never a release asset), does everything deterministic; two committed skills do the judgment —
which changes shipped, how the notes read, which version to cut.

**The usual way: run the `release-apogee` skill.** It is canonical under
`.apogee/skills/release-apogee/`, so apogee loads it as a project skill, and Claude Code
reaches it through the pointer `.claude/skills/release-apogee/SKILL.md`. It checks the gate
with `status`, drafts the notes with `release-notes-apogee`, proposes a version for you to
confirm, makes the two local commits with `prepare`, shows a `publish --dry-run`, and runs
`publish` only after one explicit confirmation. `release-notes-apogee` on its own is
read-only: it answers "what shipped since the last release?" and drafts the notes without
cutting anything.

**By hand,** the same cut is four steps:

1. **Check the gate: `go run ./cmd/release status [vX.Y.Z]`** (or
   `make release-status [VERSION=vX.Y.Z]`). Read-only apart from fetching `origin`, it
   reports the release gate, the baseline — the latest published release and the commits
   since — and the highest release tag, then probes each publish step (tag, release,
   homebrew, scoop, winget, smoke) for the version as `done`, `pending`, `setup-pending` or
   `unknown`. The version defaults to `VERSION`; a bare `X.Y.Z` reads as `vX.Y.Z`. It exits
   0 when the gate clears and 1 when it is blocked or a check could not run. The gate is
   green CI, not a local `make check`. Its gate commit is `HEAD` with the trailing commits
   that touch only `CHANGELOG.md` and/or `VERSION` peeled off — a release's own two commits
   and any per-feature micro-bumps. It clears when the tree is clean, the gate commit is on
   `origin/main`, `origin/main` is contained in `HEAD`, and `ci.yml` has a successful run for
   the gate commit or for a peeled commit `origin/main` already contains (a push runs CI on
   its head alone, so a feature pushed with its micro-bump leaves the green run on the
   bump). There is no bypass: red CI, flaky or not, blocks the release until it is fixed.
2. **Make the two release commits: `go run ./cmd/release prepare vX.Y.Z`.** It makes exactly
   two **local** commits and pushes nothing: `chore(release): cut vX.Y.Z`, which gives every
   bullet under `## [Unreleased]` its `## [X.Y.Z] — YYYY-MM-DD` heading, then
   `chore(version): bump version to vX.Y.Z` — the bump alone, last, so the tree the tag pins
   already carries the rollup. It refuses, writing nothing, when the version is not strictly
   above the highest `v0.*` release tag, `VERSION` already reads it, the tree is dirty,
   `[Unreleased]` is missing or empty, or `CHANGELOG.md` already has a heading for the
   version. An aborted cut is undone with
   `git reset --keep HEAD~2`.
3. **Publish: `go run ./cmd/release publish vX.Y.Z --notes-file <path>`.** Write the release
   notes to a file outside the repo, run it once with `--dry-run` — the gate and the probes,
   then what each pending step would do, changing nothing — and then for real. It is the
   only verb that changes anything outside the machine, it never prompts or reads stdin, and
   it runs these steps in order:
   - **tag** — while the tag is not on `origin`, it refuses unless the gate clears and
     `VERSION` at `HEAD` reads the version; it then pushes `HEAD` to `main`, where
     `tag-on-version-bump.yml` puts the **annotated** `vX.Y.Z` tag on the bump commit, and
     waits for it (`--tag-timeout`, default 10m). It never creates a tag itself. Never move
     or delete a tag afterwards.
   - **release** — builds the six archives plus `SHA256SUMS` in a fresh `git clone` checked
     out at the tag, in a temporary directory and never in your checkout, and creates the
     GitHub Release with `gh release create --verify-tag`, titled with the tag and carrying
     the notes file and all seven assets.
   - **homebrew** and **scoop** — render the formula and the manifest whole from the
     templates in `cmd/release/templates/`, filling in the version and the hashes from the
     release's `SHA256SUMS`, and commit `apogee X.Y.Z` straight to `airiclenz/homebrew-tap`
     and `airiclenz/scoop-bucket` (`bucket/apogee.json`), pushing only when the file
     changes. Both files are generated output: a hand edit in the tap or the bucket is
     overwritten at the next release, so change the template here instead.
   - **winget** — runs `komac update AiricLenz.Apogee` with both Windows archives' release
     URLs and `--submit`, opening the manifest pull request against `microsoft/winget-pkgs`,
     which merges later, after winget's own validation and review. komac takes its token
     from `GITHUB_TOKEN`, else `gh auth token`.
   - **smoke** — last, the smoke test below, over everything the other steps published.

   It prints the step table whatever happens and exits 1 when a step fails.
4. **Re-run to converge.** Before each step `publish` asks the remote whether that step is
   already done and skips it if so, so a half-finished release — a failed step, an
   interrupted run, a tag that took longer than the timeout — is resumed by running the
   same command again. There is no local state file: the remote is the truth, and a second
   run cannot duplicate a commit or a pull request. A channel whose one-time setup (below)
   is pending is skipped and named, not failed; once that setup lands, running `publish`
   again converges the missing channel.

**The smoke test.** `go run ./cmd/release smoke [vX.Y.Z]` (or
`make release-smoke [VERSION=vX.Y.Z]`) runs on its own too, against any published release,
and it is the check that catches a release nobody can install. It checks that the tag is
on `origin` and annotated rather than lightweight; with `--dist-dir` (which `publish` passes
its own build to), that the local build's `SHA256SUMS` equals the published one; that all
six published assets download and match the release's own `SHA256SUMS`; that each of those
binaries carries the tagged commit in its embedded build stamp — the checksums only prove
the assets match the list the release itself published, the stamp is what ties them to the
tree — and that the archive for *this* machine holds a binary reporting the released
version. Where apogee is installed through Homebrew, it also runs
`brew update && brew upgrade apogee` and expects the upgraded binary to report the same
version — the one claim only a real tap and a real release can make. Last, it reads the
Scoop bucket's `bucket/apogee.json` and checks that it names the release and lists both
Windows archives' hashes exactly as the release's `SHA256SUMS` does. Only the latest release
belongs in the bucket, so smoking an older tag skips that check, naming the bucket's
version, rather than failing it; the winget half has no such check, since its pull request
merges on winget's schedule, not the release's. Every check whose tool or input is missing —
`gh`, Homebrew, an archive for this host, a bucket it cannot read — says `SKIP` and names
it, so a partial run is never mistaken for a pass. A binary built from a modified tree only
warns — untracked files flip that flag.

**One-time setup for the Windows channels.** `publish`'s scoop and winget steps publish into
repositories that have to exist first, and the first winget submission is a different
command from every later one. Until each is done, `status` reports that channel
`setup-pending` and `publish` skips it. Done once, by the owner. Scoop's half is done and the bucket
is live; winget's first submission (step 4) still awaits
review in `microsoft/winget-pkgs`, so the winget step stays `setup-pending` until it merges:

1. **Create the Scoop bucket.** A public GitHub repository `airiclenz/scoop-bucket`; the
   scoop step writes `bucket/apogee.json` into it, creating the directory on its first run.
   It clones with `gh repo clone` and pushes with `git`, so that `gh` login needs push
   access to the bucket.
2. **Fork `microsoft/winget-pkgs`** to the account that submits. komac pushes each manifest
   branch to that fork and opens the pull request from it.
3. **Install komac** from [its repository](https://github.com/russellbanks/Komac), which
   lists the package-manager and prebuilt installs.
4. **Submit the first manifest with `komac new AiricLenz.Apogee`.** `komac update` — what
   the winget step runs — can only update a package winget already knows, so the
   first release goes in through `komac new`'s interactive questions: the version, both
   Windows archives' release URLs, `apogee_<version>_windows_<arch>/apogee.exe` as the
   nested portable executable with the command alias `apogee`, and the package's
   publisher, name, licence and description. Let it submit, and wait for that pull request
   to merge; until it has, the winget step stays `setup-pending`.
5. **The token.** komac opens pull requests with `GITHUB_TOKEN`, or `gh auth token` when
   that is unset: a classic personal access token with the **`public_repo`** scope, which
   is enough to push to the fork and open a pull request on `microsoft/winget-pkgs`.

`make check` covers what can be proven *before* a release: alongside the Go gates it runs
`scripts/check-pins.sh` — every GitHub Action must be pinned to a 40-character commit SHA
with its `# vX.Y.Z` tag in the comment beside it — and `actionlint` over the workflow files.
Both also run in CI, so a workflow cannot regress between one push and the next.
It also lints the module itself: `make lint` runs `golangci-lint`'s standard set under
`.golangci.yml`, pinned by module version and fetched with `go run` exactly the way
`actionlint` is, so it needs no separately installed tool. And `make vulncheck` runs
`govulncheck` against the Go vulnerability database, which reports only the known
vulnerabilities your code actually reaches; it is the one gate that needs the network, and
it fails with the tool's own error when the database is unreachable rather than passing
quietly. Both run in CI too.
Windows-tagged tests run on a `windows-latest` job; `make check` on a Linux or macOS box
vets the whole module for Windows (`GOOS=windows go vet ./...`), so every Windows-tagged file
must compile and vet clean, but it cannot run them.

Prefer the raw toolchain? `go build -o apogee ./cmd/apogee` builds the same binary, minus
one field of provenance: `make build` injects the build number — the commit count, via
`-ldflags -X github.com/airiclenz/apogee.buildCount=…` — so `apogee --version` reports
`vX.Y.Z+N.g<rev>[.dirty]` from a Make build and `vX.Y.Z+g<rev>[.dirty]` from a bare
`go build`; the version number itself comes from `VERSION` either way. Otherwise the
Makefile just gives the common commands one-word names. Releases are cross-compiled to
all **six** targets — Linux, macOS and Windows × `amd64` and `arm64` — from any one of
them: the tree is CGO-free, so `make dist` builds and packs the entire published
matrix on whichever machine cuts the release (`make cross` is the compile check beside
it — the same six targets, but compiling every package, `./...`, to `/dev/null`, where
`dist` builds only `./cmd/apogee`, with `-trimpath`), and every OS-specific backend is behind a build tag
rather than a separate artifact. `make dist` needs nothing beyond the Go toolchain: the
`dist` verb of `cmd/release` packs the archives and writes `SHA256SUMS` with the standard
library, so it runs the same on Linux, macOS and Windows.

**Reading the code?** [`AGENTS.md`](../../AGENTS.md) is the single map: it says where each
kind of knowledge lives — the glossary (`capcom know show glossary`) for the domain language, the decision records
(`capcom know show decisions`) for the settled decisions, `docs/design/` for the contracts, `layout.md` for the TUI spec — and
states the conventions you cannot derive from the source. Per-package `doc.go` files
carry the file-by-file tours from there.

> **Note:** launch the TUI with a bare `apogee` — it starts on a `servers:` entry of
> `~/.apogee/config.yaml` (an entry with `wire: anthropic` speaks the Anthropic Messages API) —
> or point one session straight at an OpenAI-compatible server with
> `apogee --endpoint <openai-compatible-url> --model <name>`. All four autonomy modes, the
> full tool suite, MCP, sub-agents, sessions, and skills are live; `apogee probe`
> reports which confinement case this machine is in (see
> [Auto mode's blast radius](configuration.md#auto-modes-blast-radius)).

