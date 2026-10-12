---
title: A shed Validated-set member never names the key that governs it now
status: done
type: bug
priority: 2
labels: [floor, residue]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-i5h.1]
created: 2026-09-03T18:41:53Z
closed: 2026-09-04T21:28:14Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 3-6 (Mechanism-retirement residue)
---

- [ ] **A shed Validated-set member never names the key that governs it now.**
  `retiredSetMemberNotice` (`cmd/apogee/validatedsets.go:243-251`) names the entry, the ID and
  `mechanisms.RetiredRelease(id)`, but not `mechanisms.Successor(id)` — the accessor item 2 added
  for exactly this reading. A member shed because it became a **Floor guard**
  (`internal/mechanisms/retired.go:105-110`) therefore tells the reader the row is gone without
  telling them the behaviour survives under `tool-call-repair`, `read-cache` and the rest, where
  the `mechanisms:` startup notice for the same ID does say so.
