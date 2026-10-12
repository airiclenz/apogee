---
title: Human stops one sub-agent without cancelling the Turn
status: done
type: feature
priority: 2
plan: 2026-09-25 - 00 - sub-agent-stop-and-session-retention-plan
aliases: [apogee-single-delegation-stop]
created: 2026-09-25T18:44:33Z
closed: 2026-09-26T04:43:53Z
reason: "Resolved by sub-agent-stop-and-session-retention plan (archived at 36bc4e6d): ^x stops one delegation, folded stopped result, parent Turn continues"
---

ADR 0086 D4-D5: a per-child stop keyed by run id (engine), bound to ctrl+x in a run view and on a Sub-Agent umbrella member row. Parent Turn continues; the stopped child is folded (second stop skips the fold); result head '[stopped by the user — engine summary follows]'; ledger outcome 'stopped'; retained like a capped run. Land beside apogee-interject-by-run-id. See docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md.
