---
title: ADR 0097 consequences
---

- The README's "works offline" sentence now says the TUI makes one release check to github.com and
  links the `update-check` key that turns it off. apogee still works offline: the check fails
  silently and nothing waits on it.
- Every interactive boot makes one HTTPS request to github.com unless disabled. Without a cache
  this is one request per boot, which is cheap and keeps no state on disk.

  > **Amended 2026-10-12.** Only a release build makes it: a `dev` build and a `.dirty` build never
  > send the request (see the amendment to point 1).
- Windows users get two package channels, and the release runbook gains two publish steps.

  > **Amended 2026-10-12 (ADR 0098).** The two
  > publish steps no longer exist as steps of their own: `cmd/release publish` publishes every
  > channel, the Homebrew tap, the Scoop bucket and winget included, and `make release-scoop` and
  > `make release-winget` are retired. Of the two Windows channels, only Scoop is live yet; winget
  > awaits its first acceptance in `microsoft/winget-pkgs`.
- A `go install` build reads as a source build and is pointed at `git pull && make install`, which
  is the nearest correct advice for a binary that carries no release stamp.

  > **Amended 2026-10-12.** It does not: a `go install` build carries no VCS revision, reads as its
  > own `GoInstall` channel, and is pointed at `go install
  > github.com/airiclenz/apogee/cmd/apogee@latest` (see the amendments to points 6 to 8).
- `apogee update` verifies the download against `SHA256SUMS` from the same release. That catches a
  corrupt or truncated download, not a compromised release: the binaries are still unsigned (bead
  `apogee-3p1`), and code signing stays out of scope here.
- Out of scope, by decision: caching the check result, an in-chat `/update` command, a custom MSI
  or EXE installer, and releases cut by CI.
