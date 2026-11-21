---
title: "A path's real location has one resolver (architecture review 2026-11-21 #4)"
status: parked
type: task
priority: 3
labels: [security, windows]
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f. The Windows junction behaviour of `EvalRealPath` is inferred from `RealPath`'s own doc, not run on Windows; confirm it first.

Problem: five resolvers answer "where does this path really live": `security.EvalRealPath` (internal/security/pathsafety.go:76, 12 non-test files), `platform.RealPath` (internal/platform/realpath.go:32), `canonicalWritableRoot` (confine_posix.go:100), `resolveBoxRoots` (confiner_windows.go), and internal/projectroot/projectroot.go:88. They disagree on a missing path (`EvalRealPath` climbs to the nearest existing ancestor; `RealPath` errors), and only `RealPath` crosses a Windows junction. Six junction fixes in ten days each patched one caller: f53e6dfc, 7ff18b95, 86a84d68, edf140b2, 74c5027e, c05956b8. c05956b8 made callers hand `security.ProjectRootPaths` (rules.go:329) four spellings, worked out in internal/agent/construct.go:304. Smaller: "workspace root plus writable paths" is rebuilt at six backend sites (landlock_linux.go:426, :561, namespace_linux.go:386, seatbelt.go:182, winguard.go:144, confiner_windows.go:347).

Solution: one module owns a path's real location under links and junctions (existing or not yet created) and the comparison of a path against a root in every spelling. The per-package `*_windows_test.go` junction files in config, security and agent collapse into one resolver suite.

Must keep: ADR 0020 (a Windows box root that is a reparse point is refused, never resolved through — the resolver must expose that, not hide it), ADR 0012 (path safety is a blast-radius bound), ADR 0096 (Project root and Adoption).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
