---
title: The 'server offline' refusal lives in three hand-kept copies
status: done
type: task
priority: 3
aliases: [apogee-v00.3]
created: 2026-09-04T11:15:12Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

The sentence a Driver prints when the startup beat never answered is spelled out three times — internal/tui/heartbeat.go:690, cmd/apogee/headless.go:494 and cmd/apogee/daemonfire.go — and only comments bind them. Items 8 and 9 of the driver-parity plan added the second and third copies deliberately (the sibling was in flight in the same tree), leaving the consolidation unowned.

internal/notice is now the natural home: item 2 created it as the leaf that composes Driver-facing sentences, and both the TUI and cmd/apogee already import it. Any move must keep the sentence byte-identical — three tests pin the exact announced string.

## Acceptance

One composer in internal/notice; the three call sites read from it; the announced sentence is unchanged byte-for-byte.
