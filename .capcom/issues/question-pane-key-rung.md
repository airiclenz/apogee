---
title: "A question pane is one module (architecture review 2026-10-14 #4)"
status: open
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#4), re-verified 2026-11-05 at f7ec72b6. The panePrompt row in internal/tui/panes.go is still the only pane with no key rung ("No key rung, for the reason above"), so the approval and ask prompts' keys live in branches of handleKey's global switch (model.go), the armed check repeats across model.go, approval.go, mouse.go and picker.go, and closes hand-clear fields beside reset(). The review proposes a soft-modal key claim on the prompt row owned by approval.go and ask.go, one armed-question gate, and one close that every path goes through, so a new prompt field is covered by reset and the claim is testable without handleKey. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched it; the review cites three latch fixes in one week as its symptom.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): VALID — panePrompt row still has no key rung (internal/tui/panes.go:137); plan with paste-outside-pane-table
