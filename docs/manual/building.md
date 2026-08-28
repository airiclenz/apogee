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
| `make test` | Run the test suite with the race detector |
| `make cross` | Cross-build all six release targets (Linux/macOS/Windows × amd64/arm64) |
| `make dist` | Build the publishable release archives into `dist/`, plus `SHA256SUMS` |
| `make check` | The full acceptance gate — gofmt, vet, build, race tests, the workflow pin check and `actionlint`, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
| `make release-smoke VERSION=v0.18.0` | Verify a **published** release from the outside (see [Releasing](#releasing)) |
| `make help` | List every target |

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
installs straight from the tip of `main` into your Go bin dir (pin a commit with
`@<sha>` instead). Only `@latest` is off-limits — proxy.golang.org immutably retains
the retired `v1.x` module versions, so `@latest` resolves to stale `v1.7.0`.

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

Prefer the raw toolchain? `go build -o apogee ./cmd/apogee` does the same thing — the
Makefile just gives the common commands one-word names. Releases are cross-compiled to
all **six** targets — Linux, macOS and Windows × `amd64` and `arm64` — from any one of
them: the tree is CGO-free, so `make dist` builds and packs the entire published
matrix on whichever machine cuts the release (`make cross` is the same six builds
thrown away, as a compile check), and every OS-specific backend is behind a build tag
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

