---
title: The published binaries are not code-signed
status: parked
type: task
priority: 3
labels: [release]
aliases: [apogee-3p1]
created: 2026-09-03T18:40:52Z
---

**Status:** parked 2026-08-05, the day the first prebuilt binaries shipped (v0.11.0 — all six
targets as release assets, plus the `airiclenz/tap` Homebrew formula that installs them). The
README states the gap where the download is rather than burying it, so this entry exists to keep
that statement honest and to hold the design once it is picked up.

**What the gap actually is, per platform** — it is two different problems wearing one name:

- **macOS.** The Go linker already ad-hoc signs `darwin/arm64` (verified on the v0.11.0 asset:
  `LC_CODE_SIGNATURE`, 162 KB), which is the *load* requirement on Apple Silicon — without it the
  kernel SIGKILLs the process, so the binaries do run. What is missing is **Developer ID signing +
  notarisation**, which is the *Gatekeeper* requirement: a browser download carries
  `com.apple.quarantine` and is refused until `xattr -d com.apple.quarantine` clears it. A `curl`
  download never sets the attribute, and Homebrew's own download does not either — so the brew path
  and the documented `curl` path are both unaffected today. Only the browser path is.
  `darwin/amd64` carries no signature and needs none to load.
- **Windows.** No signature at all, so SmartScreen warns about an unrecognised publisher. An
  Authenticode certificate is the only fix, and reputation accrues per-certificate over time.

**Why it is deferred, not dropped.** Both halves cost money and identity, not engineering: an Apple
Developer Program membership (notarisation also needs a per-release upload to Apple's service, so
it puts a network round-trip and a credential into `make dist`'s path) and an Authenticode
certificate from a CA. Neither belongs in a pre-production 0.x release cut from a laptop, and
`SHA256SUMS` — published as a release asset, and what the Homebrew formula pins per platform — is
the integrity check that is actually available today.

**The design question to settle when this is picked up:** whether signing stays a *release-time*
step layered over `make dist` (the target keeps producing unsigned archives, and a separate
signing/notarisation pass rewrites them before upload) or moves *into* it. The layered answer is
strongly preferred: `make dist` is cross-platform by construction — one CGO-free `go build` per
target from whichever machine cuts the release — whereas `codesign`/`notarytool` run only on macOS
and `signtool` only on Windows. Folding either in would make the whole matrix un-buildable from a
single host, which is the property the target exists to have. That points at CI (a signing job per
platform, secrets held there) rather than at the Makefile, and CI-cut releases are not a thing this
repo does yet — settle that first.
