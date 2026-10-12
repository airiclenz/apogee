---
title: "Retry a capped no-text reply once at a raised cap (reverses ADR 0046 'rejected: retrying a reply that hit the cap')"
status: done
type: task
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-tfp]
created: 2026-09-14T20:28:24Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Session-mining headline 15 / item 25b: 30a3b2df [320] faulted with 'a retry meets the same ceiling' and the identical retry [322] succeeded; 538f0700 [241] 26k reasoning tokens. A reasoning-forced model makes the cap non-deterministic, which contradicts ADR 0046 D4's premise. Fix shape: on FinishLength with no text and reasoning present, one re-stream at 2× cap through the restreamSpent latch (internal/agent/loop.go cappedReplyErrFmt). The message wording fix alone lands in plan 2026-09-14 - 03. Deferred by owner call.
