---
title: "Seating a session is one operation (architecture review 2026-10-14 #14)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#14), re-verified 2026-11-05 at f7ec72b6. Three sites still copy the session-owned fields into the Model by hand: replayResumed (internal/tui/model.go, boot resume), resumeLoaded (internal/tui/sessions.go, /sessions and /fork) and resetSessionView (internal/tui/commandrun.go, /clear and /new, writing the zeros), and the currency label is still derived two ways (r.Currency at boot, Meta.EffectiveCurrency on a loaded resume). The review proposes one session-seat operation that takes a seed — a record or zero — and owns every session-owned field, so a new field is one edit and resume / clear parity holds by construction. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched it.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
