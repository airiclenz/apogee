---
title: internal/floor's write-tool superset is unpinned
status: done
type: task
priority: 2
labels: [floor, residue, tests]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-i5h.3]
created: 2026-09-03T18:41:54Z
closed: 2026-09-04T21:28:14Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 3-6 (Mechanism-retirement residue)
---

- [ ] **`internal/floor`'s write-tool superset is unpinned.** `wave4WriteTools`
  (`internal/floor/toolnames.go:14-52`) decides whether a call invalidates the read cache and
  whether an empty reply was preceded by a write, but the test that cross-checked the map against
  every workspace-writing builtin (`TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin`) died
  with `internal/mechanisms/writedetection_test.go`. `internal/floor/toolnames_test.go` pins only
  a hand-written case table, so a writing tool added to `internal/tools` later is simply absent
  from the map and the guards go stale silently. Re-homing the coverage test needs a call on
  floor's "never imports `internal/tools`" rule (`internal/floor/doc.go:13-15`).
