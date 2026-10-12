---
title: "Webhook handlers for the sync Reaction classes (advise:/gate: as a mapping)"
status: done
type: feature
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-1d8]
created: 2026-09-09T07:19:56Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Stage 3 admits argv only for advise: and gate: (owner call 2026-09-09); a mapping is refused with 'advise: is an argv list' / 'gate: is an argv list'. ADR 0076 D3 names webhook as a day-one handler kind in general. If a case arrives: POST the seam payload, read the response body as the advice text (advise) or the decision word + reason (gate), same deadline/cap/escalation as argv.
