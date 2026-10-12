---
title: TestMechanismIDsConstructsUnderBypass is vacuous
status: done
type: task
priority: 2
labels: [floor, residue, tests]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-i5h.2]
created: 2026-09-03T18:41:54Z
closed: 2026-09-04T21:28:14Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 3-6 (Mechanism-retirement residue)
---

- [ ] **`TestMechanismIDsConstructsUnderBypass` is vacuous.** Its comment
  (`cmd/apogee/wire_tools_test.go:277-280`) still calls `validate` "a real catalogued Mechanism",
  but `validate` retired in this wave, so `ResolveEnabled` drops it
  (`cmd/apogee/wire_tools_test.go:283`) and the test builds an Agent with an EMPTY
  `EnableMechanisms` — it no longer proves the config → `EnableMechanisms` → engine-build path is
  coherent end-to-end. Pinning it again needs a test-registered row, since the shipped catalogue
  is empty by design.
