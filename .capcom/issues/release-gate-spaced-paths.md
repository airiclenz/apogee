---
title: Release gate misreads any commit that touches a path containing a space
status: open
type: bug
priority: 2
labels: [tooling]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 12 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `touchesOnlyPeelablePaths` (cmd/release/gate.go:129) splits `git diff-tree --name-only` with `strings.Fields`, so house plan and handoff names (`YYYY-MM-DD - NN - name.md`) tear into fragments that match no CI-skip pattern; a C-quoted path keeps its quote. (The report's second site `:545` does not exist.)

## Why

The peel stops at a prose-only commit CI never ran on, and `checkGate`, `status` and `publish` falsely report "ci.yml has no successful run", blocking a clear release.

## Acceptance

- Paths are read with `git diff-tree -r -z --name-only` and split on NUL.
- A gate test with a spaced path peels it.
