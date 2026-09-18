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
| `make dist` | Build the publishable release archives into `dist/`, plus `SHA256SUMS` |
| `make check` | The full acceptance gate — gofmt, `GOOS=windows go vet` over `internal/platform` and `internal/probe`, `golangci-lint`, build, `govulncheck`, race tests, the workflow pin check and `actionlint`, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
| `make release-smoke VERSION=v0.18.0` | Verify a **published** release from the outside (see [Releasing](#releasing)) |
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
release version — one line, carrying the leading `v` (`v0.16.8`); `make dist` strips that
`v` for the archive names and nothing else re-states the number. Pushing a commit that
changes `VERSION` to `main` is what creates the tag: a CI workflow
(`.github/workflows/tag-on-version-bump.yml`) puts an **annotated** tag on that exact
commit, named verbatim from the file, one per bump the push carries. So a version bump is
always a commit of its own and, at a release cut, the *last* one — the `CHANGELOG.md`
rollup lands first, so the tree the tag pins already contains it. Publishing a GitHub
Release on top of that tag, with the archives `make dist` packs, stays a separate manual
act; CI creates the tag and nothing more.

## Testing

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
today — same flags but one, same isolation, nothing skipped or reordered within its shard —
and the run is bounded by the slowest shard rather than the slowest package. Measured on a
9-core box before the `cmd/apogee` sweep: 212s in one process, 82s sharded cold (no timing
cache) and around 55s warm.

The one flag is `-parallel`. `go test` runs a package's `t.Parallel` tests GOMAXPROCS at a
time by default, as if it had the box to itself; a shard does not, and a shard that also fanned
its tests out that wide would put shards × GOMAXPROCS driven e2e tests on the box at once —
which is load, not logic, and is what makes 5 s waits time out and leak checks catch goroutines
still unwinding. So the script divides its process budget among the processes it launches and
passes each heavy shard that share as `-parallel`: 1 whenever the plan already fills the budget
with processes (the default sizing, and CI's cap below), more only when `APOGEE_TEST_SHARDS`
leaves slots over. The bound is the script's, not the tests': the isolated
`go test -race -count=1 ./cmd/apogee/` keeps `go test`'s default and the whole box, which is
where the `cmd/apogee` sweep's 202s → 66s shows.

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

`go test -race -count=1 ./...` remains the equivalent single-process run, and is the one to
reach for when bisecting or debugging a single test. CI runs `make test` under
`APOGEE_TEST_SHARDS=2` — the same sharded form, capped because a hosted runner is a 4 vCPU
box where each race-enabled shard carries its own memory cost.

Two opt-in knobs make the same script runnable on a box the default plan overloads, such as
a Raspberry Pi 4. `APOGEE_TEST_SLOW=1` is the slow-box plan: one shard per heavy package
(an explicit `APOGEE_TEST_SHARDS` still wins), every process `-parallel 1`, the rest `-p 2` —
at most four driven tests on the box at once. It is explicit rather than sized off the core
count because a Pi and CI's 4 vCPU runner report the same `nproc` and differ 3–5× per core.
`APOGEE_TEST_RACE=0` drops the race detector: the Raspberry Pi OS arm64 kernel has 39-bit
virtual addresses and TSan requires 48 (`FATAL: Found 39 - Supported 48`), so no `-race`
binary runs there at all. An unraced run announces itself on stderr and in its `==>` summary
line, and it is not the `make check` gate — `make check` refuses to run with
`APOGEE_TEST_RACE=0` set; race-enabled verification needs another box.

## Releasing

Cutting a release is four acts, in this order, and only the first two are automated.

1. **Roll the changelog up.** `CHANGELOG.md`'s `[Unreleased]` section gains its release
   heading, on a commit of its own. It lands *first*, so the tree the tag pins already
   carries it.
2. **Bump `VERSION`, alone, last.** One line, leading `v`. Pushing that commit to `main`
   is what creates the annotated tag — `tag-on-version-bump.yml` puts `vX.Y.Z` on that exact
   commit and does nothing else. Never move or delete a tag afterwards.
3. **Publish.** `make dist` packs the six archives plus `SHA256SUMS` into `dist/`; attach
   all seven files to a GitHub Release on that tag, then point the Homebrew tap's formula
   (`airiclenz/tap`) at the new assets and their checksums.
4. **Smoke it from the outside.** `make release-smoke VERSION=vX.Y.Z` is the only step that
   can run *after* the release exists, and it is the one that catches a release nobody can
   install. It checks that the tag is remote and annotated rather than lightweight, that
   `make dist` still packs six verifying archives, that all six published assets download
   and match the release's own `SHA256SUMS`, that each of those binaries carries the tagged
   commit in its embedded build stamp (`go version -m`) — the checksums only prove the assets
   match the list the release itself published, the stamp is what ties them to the tree — and
   that the archive for *this* machine unpacks to a binary reporting the released version.
   Where Homebrew is installed and already has apogee, it also runs
   `brew update && brew upgrade apogee` and expects the upgraded binary
   to report the same version — the one claim only a real tap and a real release can make.
   Every check that needs a tool this machine lacks (`gh`, `brew`, `unzip` for the two
   Windows archives' stamp) says `SKIP` and names it, so a partial run is never mistaken for
   a pass. A binary built from a modified tree only warns — untracked files flip that flag.

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
vets the two trees that carry them (`GOOS=windows go vet ./internal/platform/...
./internal/probe/...`) but cannot run them.

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
rather than a separate artifact. `make dist` needs `zip` on the box for the two
Windows archives; everything else it reaches for is either the Go toolchain itself or
standard on any Unix-like box (`tar`, `sed`, and `sha256sum`/`shasum`).

**Reading the code?** [`AGENTS.md`](../../AGENTS.md) is the single map: it says where each
kind of knowledge lives — `CONTEXT.md` for the domain language, `docs/adr/` for the
settled decisions, `docs/design/` for the contracts, `layout.md` for the TUI spec — and
states the conventions you cannot derive from the source. Per-package `doc.go` files
carry the file-by-file tours from there.

> **Note:** launch the TUI with `apogee --endpoint <openai-compatible-url> --model <name>`
> to hold a real coding conversation with a local model. All four autonomy modes, the
> full tool suite, MCP, sub-agents, sessions, and skills are live; `apogee probe`
> reports which confinement case this machine is in (see
> [Auto mode's blast radius](configuration.md#auto-modes-blast-radius)).

