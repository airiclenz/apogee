---
title: Store.Prune's partial-failure contract is untested on the count-only path
status: done
type: task
priority: 3
aliases: [apogee-v00.5]
created: 2026-09-04T11:15:23Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

internal/session Store.Prune calls s.now() only when MaxAge > 0, so the injected-clock seam item 15 used to unlink a candidate mid-sweep never fires on a MaxCount-only sweep. TestPruneReportsFirstErrorAndKeepsSweeping therefore pins the contract on the age path alone; the count path has no seam to reach it without a production change.

## Acceptance

The 'first error, keep sweeping' contract is pinned on the MaxCount-only path too.
