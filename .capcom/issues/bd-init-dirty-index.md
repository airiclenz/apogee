---
title: No guard stops an agent running `bd init` against a dirty git index
status: done
type: task
priority: 3
aliases: [apogee-f82]
created: 2026-09-04T21:52:24Z
closed: 2026-09-05T07:07:39Z
reason: "Guard shipped: .beads/hooks/prepare-commit-msg (outside the BEADS INTEGRATION markers) refuses bd init's auto-commit when the index carries paths bd does not own"
---

Deferred from plan `docs/plans/2026-09-03 - 02 - deferred-items-sweep-plan.md` (item 1). Item 1's reproduction cleared beads' `pre-commit` hook — it sweeps nothing — and pinned the real mechanism instead: `bd init` runs its own auto-commit, so an agent that runs it while the index is dirty commits whatever is staged. That is what took the Mechanism-retirement wave's 22 staged deletions. The hazard is now recorded in AGENTS.md and nowhere else: no hook, wrapper or check refuses `bd init` on a dirty index, so the next agent that runs it repeats the incident. Wanted: a guard (a pre-`bd init` cleanliness check, or a documented wrapper) rather than a prose warning. See AGENTS.md's git-hook section for what the reproduction established.
