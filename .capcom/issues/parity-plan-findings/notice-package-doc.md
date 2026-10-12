---
title: internal/notice keeps its package doc in contextfiles.go rather than a doc.go
status: done
type: task
priority: 4
aliases: [apogee-v00.4]
created: 2026-09-04T11:15:13Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

The package was created with two files and its package doc at the head of contextfiles.go. The house convention for a non-trivial package is a doc.go. Harmless at one file; worth doing when the package grows a second concern (the 'server offline' composer would be one).
