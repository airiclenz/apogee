---
title: Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke
status: done
type: task
priority: 3
labels: [macos, owner-run]
plan: 2026-11-15 - 00 - phase5-ci-automation-plan
aliases: [apogee-2uh.2]
created: 2026-09-03T18:40:50Z
closed: 2026-11-17T09:52:58Z
reason: "Owner live run on a Mac (v0.35.3): an Auto session asked to install fonts outside the project was refused by the seatbelt sandbox. /dev/null half: TestSeatbeltAllowsDevNullThroughTheFence green in macos.yml run 43874453469 (1dab6b42)."
---

- **Live Auto-confined deliverable run on macOS (seatbelt) + runtime smoke** — same shape; the
  darwin binary cross-builds clean (amd64+arm64, re-verified 2026-07-23), so what remains on a
  Mac is `--help`, a trivial session, and the confined Auto run. That run should also probe the
  `/dev/null` seatbelt exemption live — `internal/platform/seatbelt_darwin_test.go` only
  delegates to the shared `confinetest.Probe` battery, so the exemption is pinned by the hermetic
  profile-string tests alone (folded here from the /dev/null confinement run's residuals,
  2026-08-13).


## Update 2026-11-15: `/dev/null` half automated by plan [`2026-11-15 - 00 - phase5-ci-automation-plan.md`](docs/plans/2026-11-15%20-%2000%20-%20phase5-ci-automation-plan.md), items 3–4

- **What the plan adds:** `TestSeatbeltAllowsDevNullThroughTheFence` in
  `internal/platform/seatbelt_darwin_test.go` runs under live `sandbox-exec` in the new
  non-gating `.github/workflows/macos.yml`. The same workflow is the first CI run of the
  existing `TestSeatbeltProbe` battery. It is not yet certain that `sandbox-exec` works inside
  GitHub's macOS VMs; if it self-skips there, the `/dev/null` probe falls back to step 4 below.

## Owner-run steps (on a Mac, about 45–60 min)

1. Install Go at `go.mod`'s version, clone, `go build ./cmd/apogee`.
2. `./apogee --help`: prints and exits 0.
3. Start a trivial session against a reachable model endpoint (`docs/manual/configuration.md`)
   and send one prompt.
4. Only if `macos.yml` skipped the seatbelt tests: run
   `go test -count=1 -run 'TestSeatbelt' ./internal/platform/` on the Mac.
5. In a scratch project, `/mode auto` and ask for a small deliverable: create a file, run a
   build or test command. Confirm it completes confined and a write outside the project is
   refused.

**Close when:** steps 2, 3 and 5 pass, and the `/dev/null` test is green in `macos.yml` or
passed in step 4.
