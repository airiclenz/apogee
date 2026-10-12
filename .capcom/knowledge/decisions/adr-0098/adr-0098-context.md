---
title: ADR 0098 context
---

Cutting an apogee release was split between the repo and the owner's personal skill library.
The repo held a runbook (`docs/manual/building.md`, *Releasing*), a CI workflow that tags a
`VERSION` bump, the `make dist` recipe and three bash scripts (`release-scoop`, `release-winget`,
`release-smoke`). Everything else lived in the personal `/cut-release` and `/release-notes`
skills, outside the repo: writing the release notes, the changelog rollup, creating the GitHub
release and editing the Homebrew tap formula. Nothing in the repo published the GitHub release or
the tap, and a clone without the owner's library could not cut a release at all.

The repo half was also tied to POSIX. `make dist` needs `zip`, `tar`, `sed` and
`sha256sum`/`shasum`, and the scripts need bash. None of these come with Windows, and apogee now
ships Windows channels (ADR 0097).

The owner judges a deterministic, code-generated changelog-to-notes renderer worse than notes an
agent writes from evidence. So the release cannot be all code. It also cannot be all agent,
because publishing to five channels is exact, repeatable work that prose instructions reproduce
badly. The owner settled the split on 2026-10-11, in a grill over the handoff
`2026-10-11 - 00 - release-automation-in-repo`.
