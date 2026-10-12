---
title: "validated-sets: is undocumented"
status: done
type: task
priority: 3
labels: [docs, residue]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-t57.1]
created: 2026-09-03T18:41:49Z
closed: 2026-09-04T21:28:21Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 9-17 (manual gaps, plus the registry-drift gate)
---

- [ ] **`validated-sets:` is undocumented.** The key parses `enable:` and `alias:`
  (`internal/config/config.go`) and carries two `/settings` rows, and
  `docs/manual/configuration.md:172-173` leans on "the Validated set" as a known term, but no page
  says what one is or how a set is matched to a model.
