---
title: A 5 s toolchain probe timeout drops the GOROOT and GOMODCACHE read roots for the whole run
status: open
type: task
priority: 4
labels: [slow-box]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`cmd/apogee/toolchain_roots.go:33`: the `go env GOROOT GOMODCACHE` probe has a fixed 5 s cap and its
answer is cached for the process, so one timeout on a slow box silently drops both read roots for
the entire run, with no retry. Sibling budgets were sized for slow boxes (`vetTimeout` 300 s,
`treeProbeTimeout` 30 s). AGENTS.md: a fixed deadline that fails a healthy run on slow hardware is a
bug.

Fix: size the budget like `vetTimeout`, or wait on the condition with a generous backstop; re-probe
on a later Turn after a timeout and say once when giving up.

The four fixed-deadline issues from this audit (`toolchain-probe-fixed-deadline`,
`undo-capture-fixed-deadline`, `tree-snapshot-fixed-deadline`, `git-tool-fixed-deadline`) need one
shared decision on the pattern; decide it once.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A slow probe no longer drops the roots for the run; a timeout is retried and announced once if it finally fails.
- No happy-path speed-up stands in for the fix.
