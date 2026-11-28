---
title: Git tool calls time out at a fixed 15 s or 30 s and can leave the index half staged
status: open
type: task
priority: 4
labels: [slow-box]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/tools/git.go:50-58`: git tool calls have fixed 15 s and 30 s caps. A timed-out call is
reported to the model as a failure, and a timed-out `git add` in `move_file` or `delete_file` can
leave the index partly staged.

Fix: size the budgets for slow boxes or wait on a condition with a generous backstop; do not shorten
the underlying git work to fit. See `toolchain-probe-fixed-deadline` for the shared decision.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A slow but healthy git call on a large repo succeeds; `move_file`/`delete_file` never leave a half-staged index after a timeout.
