---
title: "run_tests ignores markers under its path: detects the runner only at the workspace root"
status: open
type: bug
priority: 2
created: 2026-11-05T08:51:43Z
---

`run_tests` detects its runner only at the workspace root. `internal/tools/run_tests.go:260` calls `detectTestRunner(t.root)` before the `path` argument is resolved, so a project in a subdirectory gets "no test runner detected", even when that directory holds `go.mod`. The refusal also doesn't say where it looked, so it reads as if nothing was found anywhere. Still true on main (2f57f04b, after v0.32.0).

Reported by Seqcom (request `run-tests-clone-marker`, run 20261105T0756Z, task `code-toolchain-check`): `run_tests path=checkouts/code-toolchain-check-capcom` was refused although `go.mod` sits at that path, while `go build ./...` and `go test ./internal/frontmatter/...` from the same directory worked in the terminal. Same refusal for a Python clone whose marker is nested deeper (`code/master/pytest.ini`).

The runner also runs from the workspace root with `path` as its target. A nested Go module can't be tested that way (`go test ./checkouts/x/...` from the outer module), so the fix must detect the runner at, or above, `path` and run it from the directory that holds the marker.

## Why

Seqcom runs each code task in a `checkouts/<task-id>` clone inside its workspace, so every Go and Python task hits this refusal and has to run its tests in the terminal, which is the step `run_tests` exists for. A monorepo with a project in a subdirectory has the same problem.

## Acceptance

- `run_tests path=<dir with go.mod>` detects Go and runs `go test` from that directory (likewise pytest and npm markers)
- With no marker at `path` or the root, the refusal names the directories it searched and points to terminal
- The tool description and docs say how `path` picks the runner
