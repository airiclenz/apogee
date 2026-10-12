---
title: ADR 0097 decision
---

1. **The interactive TUI checks for a newer release on every boot.** The check runs
   asynchronously and never delays boot. It keeps no cache file, so each boot asks once. No other
   **Driver** checks: `apogee headless`, `apogee daemon`, the bench and `apogee probe` never make
   the request. A run whose output must be reproducible never depends on the network state of the
   day.

   > **Amended 2026-10-12.** Not every boot: only a release build checks. A binary whose base
   > version is `dev` (built with an empty `VERSION`) or whose full version ends `.dirty` (built
   > from a modified working tree) gets no update host and never sends the request
   > (`updateCheckEnabled` in `cmd/apogee/wire_update.go`: `devBaseVersion`,
   > `dirtyVersionSuffix`). A developer's own build is never told to upgrade to a published
   > release.

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

   > **Amended 2026-10-11 (ADR 0098).** The caller
   > is no longer the personal `/cut-release`. The repo's own `cmd/release publish` publishes
   > every channel, Homebrew tap included. `make release-scoop` and `make release-winget` are
   > retired in its favour. The one-time setup is still manual.

   > **Amended 2026-10-12.** There are six channels, not five. A binary built by `go install …@latest`
   > is its own channel (`update.GoInstall` in `internal/update/detect.go`), and its upgrade command
   > is `go install github.com/airiclenz/apogee/cmd/apogee@latest`, not `git pull && make install`.
   > The source-build row stands for a binary built from a checkout. `docs/manual/updating.md`
   > carries the six-row table.

7. **The install channel is detected from a build stamp and the executable's location.** `make
   dist` stamps the release binaries it builds. A binary without that stamp is a source build — a
   local `make build`, `make install` or `go install` — and is told `git pull && make install`. A
   stamped binary is classified by where its own executable lives, with symlinks resolved: inside
   a Homebrew prefix it is the Homebrew channel, inside a Scoop apps directory it is Scoop, inside
   winget's package directory (user or machine scope) it is winget. A stamped binary anywhere else
   came from a release archive.

   > **Amended 2026-10-12.** The code checks the location first and the stamps second, and reads
   > two stamps, not one (`update.Detect` in `internal/update/detect.go`). Any binary whose resolved
   > executable lies under a Homebrew Cellar (`/Cellar/apogee/`), a Scoop `apps/apogee` directory or
   > winget's package directory belongs to that channel, stamped or not. Only a binary outside
   > every package-manager tree falls through to the stamps: one whose build info carries no
   > `vcs.revision` is a `go install` build, one with a revision but without the `make dist` stamp
   > is a source build, and a stamped release binary is a release archive. An unstamped binary is
   > therefore not always a source build.

8. **`apogee update` replaces only a release-archive install.**
   - On a Homebrew, Scoop or winget install it refuses: it prints that channel's upgrade command
     and exits non-zero. It never runs a package manager on the user's behalf, because the package
     manager owns that binary and its records.
   - On a source build it refuses the same way and prints `git pull && make install`.

     > **Amended 2026-10-12.** A `go install` build is refused the same way with its own command:
     > `apogee was installed via go install — run: go install
     > github.com/airiclenz/apogee/cmd/apogee@latest` (`managedInstallError` in
     > `cmd/apogee/update.go`).
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
