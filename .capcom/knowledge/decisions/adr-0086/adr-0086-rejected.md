---
title: ADR 0086 considered options
---

- **Asynchronous delegation (handoff option A).** Rejected for now, not denied. It is the literal
  ask, but it replaces ADR 0013 §5 and ADR 0039 D4, and it ships dark under the Floor invariant.
  D1 and D4 are both parts it would need, so none of this is thrown away if it is grilled later.
- **Retention for the Exchange only.** Rejected by the owner. It covers the parent correcting a child
  before it answers, but not "ask the refactor helper to also update the docs" a message later.
- **A summary call at every completed delegation, or a saved child transcript.** Rejected: the first
  costs a model call on every named completion, including those never continued, and the second is
  option A's resume and overflows a small model's window. The child's own final report is free.
- **A continue line on every result, so every delegation is continuable.** Rejected: one more line
  of model-facing text per delegation for every model, which the Floor invariant would ship off.
- **Caps on what is saved** (the 16 newest, a 4096-token report). Rejected by the owner: disk is
  cheap, and the only cost that matters is the model's context, so the caps apply at use (D2).
- **`esc×2` inside a run view as the single stop.** Rejected: the first `esc` walks up a level, so the
  double tap would land at the top level and cancel everything.
- **Folding `apogee-2un` in** (a Turn cancel keeps the siblings that already finished). Kept
  separate: it changes what `esc×2` means and needs its own design for the wire shape of a rolled-back
  Turn that keeps some tool results. The single stop covers most of its need, and the two share code.
