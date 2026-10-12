---
title: The dangerous-action guard is documented nowhere user-facing
status: done
type: task
priority: 2
labels: [docs, residue, security]
plan: 2026-09-03 - 02 - deferred-items-sweep-plan
aliases: [apogee-t57.6]
created: 2026-09-03T18:41:51Z
closed: 2026-09-04T21:28:22Z
reason: Delivered by docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md items 9-17 (manual gaps, plus the registry-drift gate)
---

- [ ] **The dangerous-action guard is documented nowhere user-facing.** The two-tier guard (hard
  refusal, then forced look) runs in every mode; `docs/manual/` has no occurrence of it, and it
  lives only in `docs/adr/` and `docs/design/`.
