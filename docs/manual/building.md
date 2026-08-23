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
| `make check` | The full acceptance gate — gofmt, vet, build, race tests, the ADR-0010 import invariant, cross-build, and an `apogee --help` smoke run |
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

