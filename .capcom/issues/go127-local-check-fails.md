---
title: "make check fails under Go 1.27.4: lint typecheck and a config test"
status: done
type: bug
priority: 2
plan: 2026-11-12 - 00 - go127-gate-and-session-log-plan.md
created: 2026-11-12T08:30:35Z
closed: 2026-11-12T16:45:38Z
reason: "Resolved by plan '2026-11-12 - 00 - go127-gate-and-session-log-plan' (archived a1d86ac0): golangci-lint pinned to v2.15.0 (item 1); the nested non-string request-extra key is refused by a key walk in RequestExtra.UnmarshalYAML, not by encoding/json, whose Go 1.27 json/v2 backing marshals map[any]any int keys (item 2); a latest-go workflow lints and tests on stable Go (item 3). Closeout ran make check green under go1.26.11 and under go1.27.4 with the 1.27.4 toolchain first on PATH. A plain GOTOOLCHAIN=go1.27.4 run failed only TestE2EAnnouncedScratchDirHoldsTheToolchainCaches, a test-environment artifact: the system go1.26.11 on PATH inside the test's confinement box attempts a toolchain switch and cannot create a module cache under the test HOME."
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
