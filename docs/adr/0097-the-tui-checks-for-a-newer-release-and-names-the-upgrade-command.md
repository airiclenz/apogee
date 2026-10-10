---
Status: accepted
---

# The TUI checks for a newer release and names the upgrade command

## Context

apogee ships through one package manager today: the Homebrew tap (`airiclenz/tap`) on macOS and
Linux. Everyone else unpacks a release archive by hand or builds from source. That leaves two gaps.

- **Windows has no package channel.** A Windows user downloads a zip, puts `apogee.exe` on the
  `PATH`, and has no command that moves them to the next release. Windows users expect `scoop` and
  `winget`.
- **Nobody is told a release exists.** apogee releases often (a `VERSION` micro-bump per shipped
  feature). Nothing in the binary says when a newer one is out, so users stay on old builds and
  report defects that are already fixed.

Closing the second gap means the binary must ask somewhere. That cuts against two things apogee
has said about itself. The README promises "no API key, no cloud, works offline". And
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
binds every layer with door-keeping invariants, two of which bear on a network request:
invariant 1 (the engine is wire-silent) and invariant 3 (no first-party connectors). So the
question is not only how to check, but where the check may live and how the user turns it off.

The owner ratified the shape on 2026-10-10 (plan
`docs/plans/2026-10-10 - 00 - windows-channels-and-update-notice-plan.md`). This ADR records it.

## Decision

1. **The interactive TUI checks for a newer release on every boot.** The check runs
   asynchronously and never delays boot. It keeps no cache file, so each boot asks once. No other
   **Driver** checks: `apogee headless`, `apogee daemon`, the bench and `apogee probe` never make
   the request. A run whose output must be reproducible never depends on the network state of the
   day.

2. **The check is one request to github.com.** It sends `HEAD
   https://github.com/airiclenz/apogee/releases/latest` and reads the latest tag from the redirect's
   `Location` header. The request carries nothing about the user, the session, the code or the
   model. It sees published releases only (never drafts or pre-release tags), and it does not use
   the GitHub API, so there is no rate limit to hit. Any failure is silent: offline, a timeout, a
   non-redirect answer or an unparseable tag all mean "no notice".

3. **The check is on by default, and two switches turn it off.** The config key
   `update-check: false` disables it. So does the environment variable `APOGEE_NO_UPDATE_CHECK` set
   to any non-empty value. The environment override shows in `/settings` as `false` with the
   environment marker, like the other environment overrides.

4. **The notice sits inline on the startup box's version row.** When the latest release is newer
   than the running binary, the row reads, for example,
   `v0.24.11 → v0.25.0 · brew upgrade apogee`. The command after the `·` is the upgrade command for
   the way this binary was installed (point 6). When the check fails, finds no newer release or is
   disabled, the row is unchanged.

5. **There is no silent auto-update.** apogee never replaces its own binary in the background. The
   notice tells the user what to run; replacing the binary is always an explicit command.

6. **Five install channels, each with its own upgrade command.**

   | Channel | Platforms | Upgrade command |
   | --- | --- | --- |
   | Homebrew (`airiclenz/tap`) | macOS, Linux | `brew upgrade apogee` |
   | Scoop (bucket `airiclenz/scoop-bucket`) | Windows | `scoop update apogee` |
   | winget (package id `AiricLenz.Apogee`) | Windows | `winget upgrade AiricLenz.Apogee` |
   | Release archive, unpacked by hand | all six targets | `apogee update` |
   | Built from source | any | `git pull && make install` |

   The Scoop manifest and the winget manifest are published by repo scripts behind make targets
   (`make release-scoop`, `make release-winget`). The owner's personal `/cut-release` skill calls
   them after the Homebrew tap step. winget pull requests to `microsoft/winget-pkgs` are submitted
   with `komac`. The one-time setup (creating the bucket repository, forking `winget-pkgs`, the
   first `komac new` submission) is manual and documented; no agent runs it.

7. **The install channel is detected from a build stamp and the executable's location.** `make
   dist` stamps the release binaries it builds. A binary without that stamp is a source build — a
   local `make build`, `make install` or `go install` — and is told `git pull && make install`. A
   stamped binary is classified by where its own executable lives, with symlinks resolved: inside
   a Homebrew prefix it is the Homebrew channel, inside a Scoop apps directory it is Scoop, inside
   winget's package directory (user or machine scope) it is winget. A stamped binary anywhere else
   came from a release archive.

8. **`apogee update` replaces only a release-archive install.**
   - On a Homebrew, Scoop or winget install it refuses: it prints that channel's upgrade command
     and exits non-zero. It never runs a package manager on the user's behalf, because the package
     manager owns that binary and its records.
   - On a source build it refuses the same way and prints `git pull && make install`.
   - On a release-archive install it asks `[y/N]` before doing anything; `--yes` skips the
     prompt. It downloads the archive for the running platform, verifies it against the release's
     `SHA256SUMS`, and runs the new binary's `--version` before the swap. The old binary is kept
     until the new one is in place, so a failure at any step leaves the working install untouched.

9. **The network code lives outside the engine.** All of it — the release lookup, version
   ordering, download and verification — sits in `internal/update`, which only `cmd/apogee`
   imports. `internal/tui` never imports `internal/update` (or the root package). It sees the check
   through a host interface on `tui.Options`, which `cmd/apogee` fills for the interactive TUI and
   leaves empty for every other path.

**Why ADR 0031 permits this.** The check is a Driver concern, composed in the Driver's own `main`,
and the engine never learns it happened. Invariant 1 holds: the embeddable engine makes no request
and exposes no wire surface. Invariant 3 holds too: it forbids service-specific *data connectors*,
tools that reach an outside service on the model's behalf. The release check is no tool, puts
nothing in front of the model, and fetches no user data — it reads one tag from apogee's own
release page. Invariant 4 is untouched, because nothing model-visible depends on the result.

## Consequences

- The README's "works offline" sentence now says the TUI makes one release check to github.com and
  links the `update-check` key that turns it off. apogee still works offline: the check fails
  silently and nothing waits on it.
- Every interactive boot makes one HTTPS request to github.com unless disabled. Without a cache
  this is one request per boot, which is cheap and keeps no state on disk.
- Windows users get two package channels, and the release runbook gains two publish steps.
- A `go install` build reads as a source build and is pointed at `git pull && make install`, which
  is the nearest correct advice for a binary that carries no release stamp.
- `apogee update` verifies the download against `SHA256SUMS` from the same release. That catches a
  corrupt or truncated download, not a compromised release: the binaries are still unsigned (bead
  `apogee-3p1`), and code signing stays out of scope here.
- Out of scope, by decision: caching the check result, an in-chat `/update` command, a custom MSI
  or EXE installer, and releases cut by CI.
