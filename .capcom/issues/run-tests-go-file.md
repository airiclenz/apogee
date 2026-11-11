---
title: run_tests hands go test a file path as a <file>/... package pattern and reports the setup failure as FAIL
status: done
type: bug
priority: 3
created: 2026-11-10T19:36:39Z
closed: 2026-11-11T06:36:34Z
reason: resolved by audit-defects plan (2026-11-10 - 02 - audit-defects-plan), archived at e5e5bb54
---

Source: docs/reviews/code-audit-2026-11-10.md ("`run_tests` turns a file path into a package pattern that matches nothing"). Verified PARTLY 2026-11-10 at a5c4f2db.

The report's "may report PASS" half is false: both a pattern error and "no packages to test" exit non-zero, so the model gets a misleading FAIL, not a false PASS.

**Where:** `internal/tools/run_tests.go`: `subtree()` (~:325), `goTestRunner.args`, `goPackagePattern` (~:383).

**What:** `subtree()` accepts a file path on purpose (its parent starts runner detection) but returns the file itself as the target. pytest and npm/jest take that natively. For Go, `internal/foo/foo_test.go` becomes `go test ./internal/foo/foo_test.go/...`; go Lstats `…/foo_test.go/`, gets "not a directory", reports `[setup failed]` and exits 1. The tool returns `FAIL (go test) — exit code 1` with an lstat error at the head of the log.

**Why it matters:** the schema invites a path and the other runners accept a file. A model that names a Go test file is told its suite FAILS with an error to decode, though no test ran; it may hunt a non-existent breakage or abandon run_tests.

**Suggested fix:** for the Go runner, map a file target to its directory (`./internal/foo`) before building the pattern, or refuse a file path for Go with a sentence naming the directory form and `filter`. Add a test with a Go file path.

## Acceptance

- run_tests path=<pkg>/<x>_test.go in a Go module runs that file's package (or returns a clear refusal naming the directory form), never a <file>/... pattern.
- A run_tests test pins the argv for a Go file path.
