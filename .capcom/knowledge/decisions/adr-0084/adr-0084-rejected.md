---
title: ADR 0084 rejected
---

- **Leaving `FileContext` unread and shrinking its fraction.** It moves the number without fixing
  the cause: any fixed fraction is wrong for a session whose standing content it cannot see.
- **Configurable shares or a configurable band.** The window is already configurable twice
  (`context-window:`, `working-window:`); a user has no way to know what fraction their standing
  content deserves, and every key here is a key the bench has to sweep.
- **Capping History inside `deriveGrowthBounds`.** It leaves two Historys in the tree — the one the
  fold derives and the one the fill notice reads — which is exactly the disagreement ADR 0077 §3
  forbids.
- **Dropping the History floor and letting the standing content take what it measures.** A
  workspace text larger than the working room would then silently starve the conversation; the
  oversize notice exists to say so out loud instead.
- **Memoising the measurement.** The standing render is not stable within a session, and a stale
  reservation is worse than a render per `budget()` call.
