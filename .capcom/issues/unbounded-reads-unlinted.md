---
title: Nothing stops a new unbounded read of a server or model body
status: open
type: task
priority: 2
created: 2026-11-27T18:35:35Z
---

Unbounded reads from MCP servers and model servers recur across audits, and each fix bounds one
call site. Bounded helpers exist (`readCappedBody` in `internal/tools/network.go`, `readCapped` in
`internal/tui/workflows.go`), but nothing makes new code use one: `io.ReadAll` appears in 18
non-test files under `internal/` and `cmd/`, and golangci-lint runs only the `standard` set.

Wanted: one size-limited read helper (or a small set, by purpose) and a `forbidigo` rule in
`.golangci.yml` that refuses `io.ReadAll` — and `json.NewDecoder` over a raw `http.Response.Body` —
outside it, with a per-line `//nolint:forbidigo // <reason>` only where the source is already
bounded (a local file read through `SafeReadFile`, an `io.LimitReader`).

## Why

A lint rule turns "remember to bound this read" into a failing `make check`, so the class
cannot come back through new code.

## Acceptance

- The 18 current `io.ReadAll` sites are each either moved to the bounded helper or carry a `nolint` with the reason the source is bounded.
- `make check` fails on a new `io.ReadAll` of a network or subprocess body outside the helper.
- Each limit is sized for the slowest hardware apogee targets and documented beside the helper.
