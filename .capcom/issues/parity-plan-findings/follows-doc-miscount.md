---
title: internal/tui follows() doc miscounts the resolvers above it
status: done
type: task
priority: 4
aliases: [apogee-v00.7]
created: 2026-09-04T11:15:25Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

reportpane.go's follows() doc says 'the three resolvers above' though only pane() precedes it in the file — reportState and reportContent sit below. Pre-existing prose, outside the range item 18 reworded.
