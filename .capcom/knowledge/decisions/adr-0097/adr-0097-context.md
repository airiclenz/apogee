---
title: ADR 0097 context
---

apogee ships through one package manager today: the Homebrew tap (`airiclenz/tap`) on macOS and
Linux. Everyone else unpacks a release archive by hand or builds from source. That leaves two gaps.

- **Windows has no package channel.** A Windows user downloads a zip, puts `apogee.exe` on the
  `PATH`, and has no command that moves them to the next release. Windows users expect `scoop` and
  `winget`.
- **Nobody is told a release exists.** apogee releases often (a `VERSION` micro-bump per shipped
  feature). Nothing in the binary says when a newer one is out, so users stay on old builds and
  report defects that are already fixed.

> **Amended 2026-10-12.** "Today" in this section is 2026-10-10, the day the owner ratified the
> shape. Since then the Scoop bucket (`airiclenz/scoop-bucket`) is a live channel, and the winget
> package's first submission still awaits review in `microsoft/winget-pkgs`, so winget is not yet
> live (`docs/manual/updating.md`).

Closing the second gap means the binary must ask somewhere. That cuts against two things apogee
has said about itself. The README promises "no API key, no cloud, works offline". And
ADR 0031
binds every layer with door-keeping invariants, two of which bear on a network request:
invariant 1 (the engine is wire-silent) and invariant 3 (no first-party connectors). So the
question is not only how to check, but where the check may live and how the user turns it off.

The owner ratified the shape on 2026-10-10 (plan
`docs/plans/2026-10-10 - 00 - windows-channels-and-update-notice-plan.md`). This ADR records it.
