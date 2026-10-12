---
title: ADR 0053 consequences
---

- The marginal cost of a new list pane is **rows + accept**. Adding one no longer means re-deriving
  a clamp, a wrap rule, a filter and a budget call.
- "↓ at the bottom of a filtered list" is a direct unit test on `Model.listKey`
  (`listsurface_test.go`) instead of a scripted overlay session, and it is proved for both wrap
  answers at once.
- The three answers a list used to give per pane — the clamp, the wrap, the accept target — became
  one, and every pane that adopts the module deletes its copy.
- Behaviour is unchanged everywhere by construction: the wrap flag, the filter semantics and the
  budget claim are the same values the panes were passing, now passed from one place.
- `popupSpec.bodyPadAbove` / `bodyPadBelow` remain the painter's contract, because the pads are spent
  against the WRAPPED body's height inside `renderPopupPlaced` and dropped whole when they do not
  fit — a caller cannot compute that. What changed is that `listsurface.go` is now their only setter.
- The `Model` did not grow: adopting the module in the picker and the browser moved four fields into
  two embedded values and cost **nothing** (104,600 → 104,592 bytes). Decision 9 is what keeps that
  true as the remaining panes adopt it.
