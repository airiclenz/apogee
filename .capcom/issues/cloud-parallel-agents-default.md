---
title: Keyed/cloud servers default parallel-agents above 1 (reverses ADR 0039 D2 'no signal means 1')
status: done
type: task
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-9se]
created: 2026-09-14T20:28:24Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Session-mining headline 6 / item 9b: fan-out ran serially on hosted servers with parallel-agents unset — 0d2ba815 0 overlaps across 123 children over 15h; 670403a4, 819afda7. ADR 0039 D2 says a cloud server with no /props signal means 1. A default (e.g. 4) for entries with an api-key or non-loopback base URL needs an ADR 0039 amendment plus the config template and manual. The width is at least announced to the model by plan 2026-09-14 - 04 (item 9a). Deferred by owner call.
