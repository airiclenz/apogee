# Building from source

Not the only way in any more — [Install](../../README.md#install) has Homebrew and prebuilt
archives — but it stays the shortest path to the tip of `main`.

**Prerequisites:** Go 1.26+ (the toolchain version pinned in `go.mod`).

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
| `make demorig` | Compile the demo storyboard rig to `./demorig` — a dev tool, never a release asset (see `graphics/demo/README.md`, "Storyboards") |
| `make test` | Run the test suite with the race detector, sharded across processes (see [Testing](#testing)); `ARGS="..."` passes extra `go test` flags to every shard |
| `make test-timings-seed` | Copy the last run's `.test-timings` into the committed `scripts/test-timings.seed` the shards pack by on a fresh checkout (see [Testing](#testing)) |
| `make live-eval` | Run the opt-in live-model eval and the judge tests against a real server, always `-count=1`; `LIVE_ENDPOINT=` (default `http://127.0.0.1:1111`) becomes `APOGEE_LIVE_ENDPOINT` and `JUDGE_ENDPOINT=` (default the same) `APOGEE_JUDGE_ENDPOINT`, with `APOGEE_LIVE_MODEL` / `APOGEE_JUDGE_MODEL` set in the environment to pin the models; fails if the real `~/.apogee` grew during the run |
| `make home-census` | Print the entry counts of the real `~/.apogee` sessions and scratch dirs (what `live-eval` compares) |
| `make fmt` | `gofmt -w` over the tree |
| `make vet` | `go vet ./...` |
| `make lint` | Run `golangci-lint` (the standard linter set, configured by `.golangci.yml`) over the module |
| `make vulncheck` | Run `govulncheck` over the dependency graph — needs the network |
| `make actionlint` | Lint the GitHub workflow files with the pinned `actionlint` |
| `make cross` | Cross-compile every package for all six release targets (Linux/macOS/Windows × amd64/arm64), as a check |
| `make dist` | Build the publishable release archives into `dist/`, plus `SHA256SUMS` — a wrapper over `go run ./cmd/release dist` |
| `make check` | The full acceptance gate — gofmt, `GOOS=windows go vet` over `internal/platform`, `internal/probe`, `internal/console`, `internal/auditsplit` and `cmd/release`, `golangci-lint`, build, `govulncheck`, race tests, the workflow pin check and `actionlint`, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
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
resolves to them.)

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

## Testing

`make test` is the supported way to run the suite, and the shape to reach for when verifying
work: it shards the two heavy packages and bounds how many tests each process runs at once. A
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

`scripts/test-shards.sh` splits those two packages across several concurrent `go test`
processes as well. A shard is a process of its own, so every test runs exactly as it does
today — same flags but two, same isolation, nothing skipped or reordered within its shard —
and the run is bounded by the slowest shard rather than the slowest package. Measured on a
9-core box on 2026-09-06, before the `cmd/apogee` sweep: 212s in one process, 82s sharded cold (no timing
cache) and around 55s warm.

The first flag is `-parallel`. `go test` runs a package's `t.Parallel` tests GOMAXPROCS at a
time by default, as if it had the box to itself; a shard does not, and a shard that also fanned
its tests out that wide would put shards × GOMAXPROCS driven e2e tests on the box at once —
which is load, not logic, and is what makes the kit's waits time out and leak checks catch goroutines
still unwinding. So the script divides its process budget among the processes it launches and
passes each heavy shard that share as `-parallel`: 1 whenever the plan already fills the budget
with processes (the default sizing, and CI's cap below), more only when `APOGEE_TEST_SHARDS`
leaves slots over. The bound is the script's, not the tests': the isolated
`go test -race -count=1 ./cmd/apogee/` keeps `go test`'s default and the whole box, which is
where the `cmd/apogee` sweep's 202s → 66s (measured 2026-09-14) shows.

The second flag is `-timeout 30m`, and it follows from the first. `go test`'s default alarm is
10 minutes per test binary, sized for a binary that fans its tests out; a shard running them
one at a time takes close to the sum of its tests, which on a slow box passes 10 minutes with
nothing hung — on a 4-core arm64 box with the race detector, `cmd/apogee` alone costs about
20 minutes serial. 30 minutes still ends a real hang with a goroutine dump. A `-timeout` of your
own wins (`make test ARGS=-timeout=1h`), since the extra flags come after the script's.

Shards are balanced from the previous run's per-test durations, cached in `.test-timings`
(gitignored, rewritten every run). A checkout that has never run the suite — a CI runner,
every time — has no cache, so the script falls back to the committed
`scripts/test-timings.seed` (same format) and packs by those; the run says which it read
(`test-shards: timings: …` on stderr). A missing or stale cache costs only a less even split,
never a skipped test: the roster comes from `go test -list`, and the script refuses to run a
plan that does not cover every listed test. `APOGEE_TEST_SHARDS=n` overrides the per-package
shard count; the script prints each shard's wall time so the balance can be read off a run.
The seed goes stale as the suite's shape moves; refresh it with `make test-timings-seed`
after a `make test` on a dev box (it copies `.test-timings` into the seed) and commit the
result.

`go test -race -count=1 ./...` remains the equivalent single-process run — every test, nothing
skipped — but it is not the supported shape, for the reason above: it hands every package the
box at once, which is the load the shard plan exists to avoid. When bisecting or debugging,
name the package and the test (`go test -race -count=1 -run TestX ./cmd/apogee/`) rather than
the whole tree. CI runs `make test` under
`APOGEE_TEST_SHARDS=2` — the same sharded form, capped because a hosted runner is a 4 vCPU
box where each race-enabled shard carries its own memory cost.

Two opt-in knobs make the same script runnable on a box the default plan overloads, such as
a Raspberry Pi 4 or 5. `APOGEE_TEST_SLOW=1` is the slow-box plan: one shard per heavy package
(an explicit `APOGEE_TEST_SHARDS` still wins), every process `-parallel 1`, the rest `-p 2` —
at most four driven tests on the box at once. It is explicit rather than sized off the core
count because a Pi and CI's 4 vCPU runner report the same `nproc` and differ 3–5× per core.
`APOGEE_TEST_RACE=0` drops the race detector: the stock Raspberry Pi OS arm64 kernels give
39-bit (Pi 4) or 47-bit (Pi 5) virtual addresses and TSan requires 48
(`FATAL: Found 39 - Supported 48`), so no `-race` binary runs on either at all. An unraced run announces itself on stderr and in its `==>` summary
line, and it is not the `make check` gate — `make check` refuses to run with
`APOGEE_TEST_RACE=0` set; race-enabled verification needs another box.

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

## Releasing

The release process lives in this repo and needs nothing outside it
([ADR 0098](../adr/0098-the-repo-owns-its-release-pipeline.md)): a clone, the Go toolchain,
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
`setup-pending` and `publish` skips it. Done once, by the owner, before the first release
that ships on Scoop and winget:

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
vets the five trees that job tests (`GOOS=windows go vet ./internal/platform/...
./internal/probe/... ./internal/console/... ./internal/auditsplit/... ./cmd/release/...`) but
cannot run them.

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
kind of knowledge lives — `CONTEXT.md` for the domain language, `docs/adr/` for the
settled decisions, `docs/design/` for the contracts, `layout.md` for the TUI spec — and
states the conventions you cannot derive from the source. Per-package `doc.go` files
carry the file-by-file tours from there.

> **Note (as of 2026-10-02):** launch the TUI with a bare `apogee` — it starts on a `servers:` entry of
> `~/.apogee/config.yaml` (an entry with `wire: anthropic` speaks the Anthropic Messages API) —
> or point one session straight at an OpenAI-compatible server with
> `apogee --endpoint <openai-compatible-url> --model <name>`. All four autonomy modes, the
> full tool suite, MCP, sub-agents, sessions, and skills are live; `apogee probe`
> reports which confinement case this machine is in (see
> [Auto mode's blast radius](configuration.md#auto-modes-blast-radius)).

