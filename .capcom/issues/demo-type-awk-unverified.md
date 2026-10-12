---
title: graphics/demo/type.sh is unverified under BSD awk and gawk
status: done
type: task
priority: 4
aliases: [apogee-at8]
created: 2026-09-05T10:20:55Z
closed: 2026-09-09T05:37:45Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

Item 1 of docs/plans/archived/2026-09-04 - 00 - humanized-demo-typing-plan.md claims graphics/demo/type.sh produces byte-identical output under BSD awk, gawk and mawk. Only mawk 1.3.4 is installed in this environment, so the claim was verified by construction (MINSTD spelled out, no srand()/rand(), products under 2^53) rather than by running all three. Run 'graphics/demo/type.sh --check' and diff the generated hero tape on a machine that has BSD awk and gawk before the next demo take.
