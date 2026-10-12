---
title: ADR 0090 consequences
---

- The engine-run workflows plan's item 43 rule is reversed: the workflow block still has no fold of
  its own, but it paints its stage rows rather than its item runs, and every item run has a view.
- ADR 0063 D4 is amended: a level of the view stack is a run or a workflow stage. A run still has
  exactly two shapes (ADR 0063 D5) — its row and its view — and an item run is a run.
- `layout.md` ("The workflow block", "Run view"), `CONTEXT.md` (**Stage view**, **Workflow**) and
  `docs/manual/workflows.md` describe the new shape.
- Nothing here is a Mechanism. The rows and views change only what the human sees and can reach,
  so there is nothing for Bypass to switch off, and the Bypass floor is untouched.
