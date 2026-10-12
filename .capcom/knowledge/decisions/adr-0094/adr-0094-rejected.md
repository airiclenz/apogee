---
title: ADR 0094 considered options
---

- **Option B: an early placeholder result.** The blocking round would return a stub at once and the
  report would replace it later. It rewrites a result the model has already read, and it reaches the
  tool round's commit order. Rejected by the owner.
- **A `fan_out` with one item.** It works today, but the agent gets a Receipt where it wanted a
  report, and it has to write a brief template for one task. The verb a small model gets right is
  still "one helper": `sub_agent`.
- **A spawn/message/wait tool family (ADR 0086's option A).** It is still not built. The `workflow`
  tool already covers status, stop and message, so this ADR adds one switch and no tool.
