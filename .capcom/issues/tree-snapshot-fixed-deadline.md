---
title: A 2 s tree-snapshot timeout silently loses the workspace-change warning
status: open
type: task
priority: 4
labels: [slow-box]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/agent/treesnapshot.go:56`: the tree snapshot has a fixed 2 s cap. A missed snapshot
silently loses the "this command changed workspace files" warning. A verifier also noted that each
Agent has its own snapshotter with no coordination.

Fix: size the budget for slow boxes (like `treeProbeTimeout` 30 s) or wait on a condition; surface a
skipped snapshot once per session. See `toolchain-probe-fixed-deadline` for the shared decision.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A slow snapshot no longer drops the warning silently; a skip is announced once per session.
