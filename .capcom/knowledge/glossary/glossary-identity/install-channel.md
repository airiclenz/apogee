---
title: Install channel
---

**Install channel**:
How the running binary was installed, which decides the upgrade command the **Boot update check**
names and whether `apogee update` may replace the binary. Six exist (`update.Method`): Homebrew,
Scoop, winget, `go install`, a source build and a release archive. `update.Detect` reads the executable's own
path first, symlinks resolved — inside a Homebrew Cellar, a Scoop `apps/apogee` directory or
winget's package directory it belongs to that package manager — and only then the build stamps: no
commit stamp is a `go install`, a commit stamp without the release stamp `make dist` sets is a
source build, and a stamped release binary anywhere else came from an archive. Only an archive install is
apogee's to replace — on every other channel `apogee update` prints that channel's command and exits
non-zero, never running a package manager for the user. See
ADR 0097.
_Avoid_: "install method" in docs (the code's `Method` names the same thing; prose says channel),
"distribution" (that is the set of channels a release publishes to, not how one binary arrived).
