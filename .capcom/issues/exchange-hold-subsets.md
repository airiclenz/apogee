---
title: "Exchange-open refusal asked inside the launch (architecture review 2026-10-14 #6)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#6), re-verified 2026-11-05 at f7ec72b6. launchExchange (internal/tui/commandrun.go) still leaves every guard to its caller ("Everything upstream of it stays the caller's"), so submit (model.go, which refuses during an actuation), runCommand, runContinue, runCompact, flushInterjections (interject.go) and the wake path (canWake, engineholds.go / workflow.go) each ask their own subset of the engine holds. The review proposes that engineHolds answer one "refuse this Exchange?" question and launchExchange ask it, so the hold rule is stated once and a new opener inherits every hold. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched it. It hinges on an unanswered owner call: may a flush or a wake open an Exchange mid-actuation? actuation.go's comment still allows it while submit refuses; ADR 0029 D5 settles only the typed case.

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
