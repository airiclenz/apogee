---
title: "TUI: one boxedPane adapter set keyed by framePane (review 2026-09-14 candidate 10)"
status: done
type: task
priority: 3
aliases: [apogee-agk]
created: 2026-09-15T06:08:10Z
closed: 2026-09-16T22:47:48Z
reason: pointer table (3d5c0183) + pickerOfferings (62cac0b1) landed
---

Deferred from docs/reviews/architecture-review-2026-09-14.html candidate 10 on 2026-09-15: the five per-pane click handlers and eight wheel arms in internal/tui/mouse.go are 8-day-old output of plan 2026-09-06 - 00 (bead apogee-p94); let the design settle, then collapse the POINTER arms only into one table keyed by framePane walked in slot order. Constraint: ADR 0053 D3 — key verdicts (close/accept) stay per pane, never in the adapter; table must be package-level, not funcs on Model (ADR 0011). Smaller fix that captures most: a shared listPaneClick(pane, render, count, accept, policy) helper. Includes the speculative pickerOffering{title,hint,rows,accept} per pickerKind (picker.go four switches) — only worth it folded with this.
