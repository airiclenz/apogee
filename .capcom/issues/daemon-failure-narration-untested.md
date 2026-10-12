---
title: The daemon's failed-Firing narration path is untested
status: done
type: task
priority: 3
aliases: [apogee-y9g]
created: 2026-09-04T11:05:24Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

Deferred from plan 'docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md' (items 11/14).

cmd/apogee/daemonfire.go:393 reads res.ContextFiles (and :424 res.Wrote) after runOnce and before the error branch at :428, so the comment's claim that the anomalies are 'reported whether the run answered or failed' holds only for a failure that still produced a Result. A run that failed early enough to return a zero run.Result logs nothing, and no test covers the failed-run path at all: daemonfire_test.go exercises the answered path only. Add a case that fails runOnce after a Result carrying an unreadable-context-file note and a written path, and one that fails with a zero Result, so the intended behaviour on each is pinned.
