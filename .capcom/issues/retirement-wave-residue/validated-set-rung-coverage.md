---
title: The Validated-set applying rung has no end-to-end coverage while the shipped catalogue is empty
status: done
type: task
priority: 3
labels: [floor, residue, tests]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-i5h.4]
created: 2026-09-03T18:41:55Z
closed: 2026-09-04T21:28:14Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 3-6 (Mechanism-retirement residue)
---

- [ ] **The Validated-set applying rung has no end-to-end coverage while the shipped catalogue is
  empty.** `setApplied`, `appliedNotice` and the canonical member sort
  (`cmd/apogee/validatedsets.go:169,207-213`) can no longer be reached by any shipped
  configuration — every shipped entry retired — so the rung and `probe model`'s "now
  AUTO-APPLIES" report survive only as unit pins over hand-built decisions
  (`internal/probe/model_test.go:105,123`). Nothing walks the path a user takes from a saved
  record to an applied set.
