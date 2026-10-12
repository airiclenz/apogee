---
title: gcSessions' nil-store guard is unreachable in production
status: done
type: task
priority: 4
aliases: [apogee-v00.6]
created: 2026-09-04T11:15:24Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

Item 16 made headless build the sweep store unconditionally, so every caller now hands gcSessions a non-nil store. Its nil guard survives only because wire_session_test.go:681 calls it directly. Either drop the guard and the direct call, or keep both and say in the doc that the guard is a test-only contract.
