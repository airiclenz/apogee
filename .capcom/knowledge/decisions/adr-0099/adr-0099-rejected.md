---
title: ADR 0099 considered options
---

- **Keep the compare and widen the window.** Rejected. Any fixed width loses to a fold queue
  backed up longer than it, and a wider window makes a stray second press more likely to act.
- **Stamp the press with its read time instead of its fold time.** Rejected. Bubble Tea v2 key
  messages carry no read timestamp, and the ordered delivery the tick already relies on answers
  the same question without one.
- **A bool for the armed state.** Rejected. Keeping the `time.Time` stamp costs nothing and lets
  tests backdate an arm to stand in for a late fold.
