---
title: ADR 0014 context
---

Small models do not spontaneously decompose an oversized task — handed "review this codebase"
on an 8k window, a 4B model reads files until the window bursts. Apogee owns the loop and
already has the delegation primitive (the `sub_agent` recursion point,
ADR 0013),
but nothing makes the model reach for it when a prompt or skill does not say so. The question
this ADR settles: how Apogee helps a model decompose a too-large task **without** the user
asking — and what shape that help takes without violating settled architecture.

The grill (2026-07-05) surfaced four constraints that forced the shape:

- **No Mechanism calls the Upstream.** Compaction's summarisation call is internal-to-a-Turn
  as a *structural* privilege (ADR 0007);
  granting Mechanisms their own model calls opens unanswerable questions (whose tokens, which
  Turn's effectiveness judgment, what the bench sees).
- **The recursion point is the single spawn path.** ADR 0013 §1 hangs Resolution, guard
  tiers, and event nesting off dispatch recognising `SubAgentToolName`; loop code invoking
  `newChildAgent` directly would be a second, uncovered spawn path — and would fabricate
  history the model never produced.
- **Auto-compaction is Exchange-boundary-only.** A multi-Turn fan-out lives inside *one*
  Exchange, so no generative reducer can fire for its entire life; only `tool_result_cap`
  (default-off) acts mid-Exchange. A fan-out design that ignores this re-creates the very
  window explosion it exists to prevent.
- **Cancel rolls the whole Turn back, and a sub-agent is atomic within its parent Turn**
  (ADR 0007; ADR 0013 §5). Anything that piles N children into one Turn makes a user's cancel
  during child 9 of 10 destroy nine completed child Exchanges.
