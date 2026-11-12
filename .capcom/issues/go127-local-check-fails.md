---
title: "make check fails under Go 1.27.4: lint typecheck and a config test"
status: open
type: bug
priority: 2
plan: 2026-11-12 - 00 - go127-gate-and-session-log-plan.md
created: 2026-11-12T08:30:35Z
---

Under a local Go 1.27.4 (go.mod says `go 1.26.11`; CI is green on 9c76ea69), `make check` fails in two places on clean `main`:

- `golangci-lint` v2.13.2 cannot read Go 1.27 export data (`export data version 5 is greater than maximum supported version 4`), so lint stops at typecheck.
- `go test -race -run TestServerEntryRequestExtraLoads ./internal/config/`: the `a_nested_non-string_key` row fails — `ApplyConfig accepted "    request-extra: {provider: {1: x}}\n"; want a refusal` (`config_test.go:7789`).

Found 2026-11-12 while running the gate for `startup-notice-wording`.

## Why

The gate cannot pass on a box with the current Go release, and CI will hit the same when its toolchain moves.

## Acceptance

- golangci-lint pinned to a release that reads Go 1.27 export data.
- The nested non-string request-extra key is refused under Go 1.27 (root cause named) and Go 1.26.
- `make check` passes under both toolchains.
