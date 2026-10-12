---
title: Store.Prune's partial-failure contract is untested
status: done
type: task
priority: 2
labels: [residue, tests]
plan: 2026-09-03 - 01 - driver-parity-and-residues-plan
aliases: [apogee-1ov.1]
created: 2026-09-03T18:41:28Z
closed: 2026-09-04T10:53:06Z
reason: delivered by plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md, item 15 (Store.Prune's partial-failure contract gets a test)
---

- [ ] **`Store.Prune`'s partial-failure contract is untested.** The doc comment
  (`internal/session/store.go:341-342`) promises that one failed delete does not abort the sweep —
  the first error comes back alongside the count of what did go — and the loop implements it
  (`internal/session/store.go:386-393`). No test drives a delete that fails, so the contract that
  keeps a single unremovable record from stranding the rest of the sweep is unpinned.
