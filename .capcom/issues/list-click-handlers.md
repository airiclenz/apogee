---
title: "A list row click is one walk (architecture review 2026-10-14 #13)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#13), re-verified 2026-11-05 at f7ec72b6. Six click handlers still repeat one multi-step body: handleBrowserClick, handlePickerClick, handleAskClick, handleApprovalClick and handleDropdownClick in internal/tui/mouse.go, and handleWorkflowsClick in internal/tui/workflows.go. The review proposes one list-click walk that owns the gate / hit / outside / chrome / armed / latch / seat sequence, with each pane supplying count, seat, accept, armed, outside and "a sub-step owns the pane", so the outside-click rule is stated once and a sub-step guard cannot be forgotten. Its symptom (a click on /workflows discarding a half-typed recipe name) was fixed by item 15 of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; ADR 0053 D3 holds (accept stays per pane), and the earlier tui-boxedpane-adapters issue closed without this shared-click step.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
