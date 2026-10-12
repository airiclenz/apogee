---
title: ADR 0042 decision
---

**1. One self-contained binary.** Apogee ships as a single static, CGO-free executable with nothing
to install beside it — no runtime assets, no shared libraries, no data directory that must exist
before first run (`config.yaml` is seeded from an embedded template into `~/.apogee`). Release
builds are `CGO_ENABLED=0` across every target (`make cross`, `make dist`), which is what keeps the
six-platform cross-build a loop over `GOOS`/`GOARCH` rather than six build environments.

**2. Every external program is a runtime-detected optional enhancement.** An executable apogee does
not ship is resolved on `PATH` — at construction where the answer is stable, at call time where it
is not — and its absence produces a **named, graceful result**, never a start-up failure and never a
prerequisite the user must satisfy first. The realisations are the pattern: the `git` tools
resolve `git` through their `execHost`'s look (*amended 2026-09-22: formerly the package-level `lookGit`; the
lookup is a field of the tool's `execHost`, the one value the execution tools take the operating system as*)
and answer "git not available"; `python_exec` probes `python3` then
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
(ADR 0004,
ADR 0012), and on
macOS the box is a generated profile handed to the system `/usr/bin/sandbox-exec`. This is the one
place a program apogee does not ship decides whether a feature exists — and it is bounded twice
over. It buys a **mode, not the agent**: a host without it still runs Plan, Ask-Before and
Allow-Edits under the confine-if-you-can/gate-if-you-can't net (`ErrConfinementUnavailable`), so the
missing program costs autonomy, not function. And it is macOS-only: Linux re-execs **apogee itself**
in its `__confined-exec` helper mode (landlock), and Windows drops a low-integrity token
(ADR 0020) —
neither reaches outside the binary.

*Amended (2026-09-17, ADR 0081):*
Linux now has the same bounded exception. On a kernel where landlock cannot fence (Raspberry Pi OS
leaves the LSM off its boot line; a container's seccomp profile answers `ENOSYS`) the selector falls
back to a **namespace backend** that launches the child under bubblewrap (`bwrap`) — resolved on
`PATH` at construction, launched once for real to prove the host grants unprivileged user
namespaces, gracefully absent. It is bounded exactly as the macOS one is: it buys **a mode, not the
agent** (`{false, false}` with the reason in `Capabilities().Unavailable`, and the
confine-if-you-can/gate-if-you-can't net keeps every other mode working), and it is reached only on
the second rung — a landlock-capable kernel never looks for it, so "Linux re-execs apogee itself"
stays true of the first rung. Two of the three OSes now carry the exception; the rule stands
because both are bounded the same way, and a native (bwrap-free) namespace launcher remains the
recorded follow-up that would take Linux's back.

*Amended (2026-10-02, ADR 0081's
note of the same date):* the 2026-09-17 note's "resolved on `PATH` at construction" is superseded for
`bwrap`. It is the one external program this decision's `PATH` resolution does not cover: it is taken
only from `/usr/bin`, `/bin`, `/usr/local/bin` or `/run/current-system/sw/bin`, because it builds the
fence and a writable `PATH` entry must not choose it. A bwrap found only on `PATH` is treated as
absent — the backend says where it found it and why it declined, and never runs it — so the
exception stays gracefully absent in exactly the same shape.

**5. The module graph stays lean and stdlib-first.** The direct requires are the set the policy
named — Cobra, Bubble Tea/Lipgloss/Bubbles, the MCP go-sdk, `yaml.v3`, and small utilities — and a
new direct dependency is a decision to be argued, not a convenience to be taken. The standing
example is ADR 0041 rejecting `fsnotify` for a one-second
`os.Stat` poll: a third-party dependency and four sets of per-OS event semantics, for a file that
changes a handful of times a session.
