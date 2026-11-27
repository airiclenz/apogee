---
title: The Exchange-close undo capture has a fixed 2-minute deadline
status: open
type: task
priority: 4
labels: [slow-box]
created: 2026-11-27T21:27:15Z
---

`internal/agent/agent.go:1664`: the undo capture at Exchange close has a fixed 2-minute cap; on
expiry it degrades to funnel-only coverage. A verifier noted that this fence overrides the store's
30-minute backstop. On a throttled box with a large repo a healthy run silently loses undo coverage.

Fix: wait on the capture with a backstop sized for the slowest hardware (consistent with the store's
own backstop), and surface the degradation when it happens. See `toolchain-probe-fixed-deadline`
for the shared decision.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A slow but progressing capture completes; degradation, when it happens, is visible to the user.
