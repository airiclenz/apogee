---
Status: accepted
---

# External programs are optional enhancements, never prerequisites

## Context

Apogee is one binary. A user builds it from source or unpacks a release archive, runs it in a
repository, and expects an agent — not an install list. The machines it has to work on are not
uniform: a developer's laptop with a full toolchain, a bare container with neither `git` nor a
Python, a locked-down work machine where installing anything is a ticket. A coding agent that
refuses to start, or that silently loses half its tool surface with no explanation, is worse on
those hosts than one that says what it cannot do and keeps going.

The policy that answers this was decided at plan time and recorded in `technical-design.md` §3a — a
section of a component-narration document that is now archived
(`docs/design/archived/technical-design.md`). The code has been citing it by section number ever since: `internal/tools/git.go:21,76`,
`internal/tools/python_exec.go:34,52,89`, `internal/tools/run_tests.go:234,365`,
`internal/tools/grep.go:85`, and `internal/mechanisms/autofix.go`'s standing requirement #2, whose
`autofix_test.go` pins the "gracefully absent" path. Those citations name a live constraint on
every tool and Mechanism written since, so it is recorded here as a decision of its own rather than
archived along with the document that happened to hold it.

## Decision

**1. One self-contained binary.** Apogee ships as a single static, CGO-free executable with nothing
to install beside it — no runtime assets, no shared libraries, no data directory that must exist
before first run (`config.yaml` is seeded from an embedded template into `~/.apogee`). Release
builds are `CGO_ENABLED=0` across every target (`make cross`, `make dist`), which is what keeps the
six-platform cross-build a loop over `GOOS`/`GOARCH` rather than six build environments.

**2. Every external program is a runtime-detected optional enhancement.** An executable apogee does
not ship is resolved on `PATH` — at construction where the answer is stable, at call time where it
is not — and its absence produces a **named, graceful result**, never a start-up failure and never a
prerequisite the user must satisfy first. The realisations are the pattern: the five `git` tools
resolve `git` through `lookGit` and answer "git not available"; `python_exec` probes `python3` then
`python` and answers "python not available"; `diagnostics`' vet half degrades to a skipped note
without a Go toolchain; `run_tests` reports "not available" for a runner its project markers named
but the host lacks; `autofix` resolves `goimports`/`black`/`rustfmt` once at construction and leaves
a language whose formatters are absent untouched. Absence is a **result the model can read and route
around**, not an error the loop retries.

**3. Where a capability can be had in-process, keep an in-process rung.** Graceful degradation is a
floor, not an excuse: autofix's standing requirement #2 — Go always keeps the in-process
`go/format.Source` tail behind `goimports` — means Go repair works on a host with nothing on `PATH`
at all. The search tools took that further and need no rung above it: `grep` and `find_files` are an
`io/fs` walk plus `regexp` (`grep.go:85` — *"no external grep"*), so ripgrep, which the original
policy named as its canonical optional dependency, is not detected at all because apogee never
needed it.

**4. One bounded exception: OS confinement.** Auto mode requires OS-level confinement
([ADR 0004](0004-auto-mode-requires-os-level-confinement.md),
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)), and on
macOS the box is a generated profile handed to the system `/usr/bin/sandbox-exec`. This is the one
place a program apogee does not ship decides whether a feature exists — and it is bounded twice
over. It buys a **mode, not the agent**: a host without it still runs Plan, Ask-Before and
Allow-Edits under the confine-if-you-can/gate-if-you-can't net (`ErrConfinementUnavailable`), so the
missing program costs autonomy, not function. And it is macOS-only: Linux re-execs **apogee itself**
in its `__confined-exec` helper mode (landlock), and Windows drops a low-integrity token
([ADR 0020](0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md)) —
neither reaches outside the binary.

**5. The module graph stays lean and stdlib-first.** The direct requires are the set the policy
named — Cobra, Bubble Tea/Lipgloss/Bubbles, the MCP go-sdk, `yaml.v3`, and small utilities — and a
new direct dependency is a decision to be argued, not a convenience to be taken. The standing
example is [ADR 0041](0041-the-config-file-is-watched.md) rejecting `fsnotify` for a one-second
`os.Stat` poll: a third-party dependency and four sets of per-OS event semantics, for a file that
changes a handful of times a session.

## Considered options

- **Document hard prerequisites** ("install git, ripgrep and Python before running apogee") —
  rejected: it moves the failure to the user on exactly the hosts where they can do least about it,
  and it makes the agent's tool surface a property of the reader's diligence. A tool that reports its
  own absence is self-documenting; a README section is not enforced by anything.
- **Detect once at start-up and refuse what is missing** — rejected: the refusal would be a wall of
  warnings about capabilities most sessions never use, and PATH is not constant for a process that
  lives for hours. Detection belongs at the tool that needs the program.
- **Vendor or embed the external programs** (a bundled `git`, a bundled formatter) — rejected: it
  multiplies binary size and licensing surface per platform, and it freezes a version of somebody
  else's tool that the user's project may not want.
- **cgo-linked libraries** (libgit2, tree-sitter) for the same capabilities in-process — rejected:
  each one ends the CGO-free cross-build, which is the property that makes six release archives a
  loop rather than a build farm.
- **Refuse to start when confinement is unavailable** — rejected: it contradicts ADR 0012's
  gate-if-you-can't net and would make a kernel version a prerequisite for a Plan-mode session that
  never spawns anything.

## Consequences

- **A tool that wants an external program owes three things**: a resolution seam that is a package
  var or injected dep (so a test can fake it), a named degraded result the model can act on, and a
  test of the absent path — `autofix_test.go`'s "gracefully absent" case and
  `TestRunTestsMissingRunnerProgramDegradesGracefully` are the shape.
- **The policy costs capability, deliberately.** An `io/fs` walk is slower than ripgrep on a large
  tree, and in-process `go/format.Source` fixes less than `goimports`. Both are accepted: the fast
  path is still taken when the program is there, and the floor is a working agent everywhere.
- **`probe`'s host report is where "what is present here" is answered**
  ([ADR 0021](0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)),
  not a start-up check — the free half already names the host's capabilities on demand.
- **The code's `§3a` citations now have a standing home.** They keep their wording (they cite the
  document that decided it, which is preserved under `docs/design/archived/`), but this ADR is the
  record a reader should land on, and new code cites this ADR rather than a section number.
- **A new direct module dependency is an ADR-shaped decision.** Not every one needs its own record,
  but the burden is on the addition: what it buys that the standard library does not, and what it
  costs the cross-build.
