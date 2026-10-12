---
title: Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke
status: parked
type: task
priority: 3
labels: [macos, owner-run]
aliases: [apogee-2uh.2]
created: 2026-09-03T18:40:50Z
---

- **Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke** — same shape; the
  darwin binary cross-builds clean (amd64+arm64, re-verified 2026-07-23), so what remains on a
  Mac is `--help`, a trivial session, and the confined Auto run. That run should also probe the
  `/dev/null` seatbelt exemption live — `internal/platform/seatbelt_darwin_test.go` only
  delegates to the shared `confinetest.Probe` battery, so the exemption is pinned by the hermetic
  profile-string tests alone (folded here from the /dev/null confinement run's residuals,
  2026-08-13).
