---
title: smoke's goproxy check does not pin GO111MODULE
status: open
type: bug
priority: 3
created: 2026-11-14T10:48:03Z
---

Deferred from the v1-placeholder-retraction run (plan `docs/plans/archived/2026-11-05 - 00 - v1-placeholder-retraction-plan.md`, item 5).

`goProxyEnv` (cmd/release/smoke.go:36-44) pins `GOPROXY`, `GOFLAGS`, `GOWORK`, `GOPRIVATE`, `GONOPROXY`, `GONOSUMDB` and `GOTOOLCHAIN` for the goproxy check's `go list -m -json -retracted` calls, but not `GO111MODULE`. With `GO111MODULE=off` in the environment, `go list -m` fails, so the goproxy check reports FAIL instead of performing the lookup. The offline rehearsal test (`internal/update/retraction_scheme_test.go`) already pins `GO111MODULE=on` for the same reason.

The smoke command's Long help (cmd/release/smoke.go:105-114) was edited for the goproxy check without reflowing: line 107 runs to 133 columns while its neighbours sit near 90.

## Why

smoke is the release's last check; an inherited environment variable must not turn a healthy release into a goproxy FAIL.

## Acceptance

- `goProxyEnv` pins `GO111MODULE=on`, and `TestSmokeGoProxyPinsTheProxyOutsideTheCheckout` pins it with the rest
- The smoke Long help is reflowed to its neighbours' width
