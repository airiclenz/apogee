---
title: ADR 0007 consequences
---

- The loop must be written so that *every* `Step()` return is at a serializable boundary —
  no partial state may leak across it. This is a structural constraint on the loop's
  implementation, locked in Phase 0.
- The recover boundary is **per-tool and per-Mechanism**, not Step-wide: a panic in one tool
  fails that tool, not the whole Turn, so the loop can still make progress.
- Cancellation semantics are part of the public API contract from v0.1 (additively refinable,
  but present): embedders rely on "cancel takes effect at the next boundary."
- The bench's "a panic aborts a long counterfactual sweep" fragility dissolves as a free
  consequence of the recover-at-boundary contract — it was a symptom of a missing *product*
  property, not a bench-only concern.
