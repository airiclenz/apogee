---
title: diagnostics reports a clean file under a nested or non-root go.mod as go vet findings
status: open
type: bug
priority: 2
created: 2026-11-10T19:36:32Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("`diagnostics` reports a clean file in a nested Go module as a vet failure"). Verified VALID (widened) 2026-11-10 at a5c4f2db.

**Where:** `internal/tools/diagnostics.go`: `goVetSpec` (~:338) and `runGoVet` (~:310).

**What:** `go vet` runs on the absolute package directory with `Dir` at the workspace root and `GOWORK=off`. When the file's module is not the root module, go refuses to load the package:
- root has its own go.mod, file under a nested go.mod → "main module (…) does not contain package …";
- root has no go.mod (Seqcom's `checkouts/<task>/` clones, a monorepo of modules) → "go.mod file not found".

`runGoVet` reads any non-zero exit as findings, so `diagnostics` returns an error quoting a module-resolution error for a clean file. The test helper's comment ("a normal workspace is always a module") states the broken assumption.

**Why it matters:** the model is told a clean file has vet errors and may churn on a phantom problem. Same nested-project shape `run_tests` was fixed for in run-tests-subdir-marker.

**Suggested fix:** run vet from the package directory (`Dir: filepath.Dir(abs)`, inside the fence) on `.`, or walk up to the nearest go.mod inside the workspace as run_tests does. Keep the GOWORK/GOTOOLCHAIN/GOENV pins. Consider reporting a module-resolution failure as "go vet skipped: …" rather than findings. Add tests for both shapes.

## Acceptance

- diagnostics on a clean .go file under sub/proj/go.mod, with a different module at the root, reports clean.
- diagnostics on a clean .go file under checkouts/x/go.mod, with no go.mod at the root, reports clean.
- A real vet finding in a nested module is still reported as findings.
