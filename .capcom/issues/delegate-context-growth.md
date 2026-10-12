---
title: Delegate context growth is unbounded below the window — no nudge before the user steps in (F6)
status: done
type: bug
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-zuu]
created: 2026-09-18T21:03:37Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Session 20260918T143011Z-9788d447: phase2 docs delegate reached 348k ctx / 10.7M prompt tokens over 57 calls before the user typed 'your context is filling up — report your findings'; delegate-max-tokens (20M) did not bite and the 1.3M window meant neither compaction nor the fill ladder fired. Same shape as the cap problem: the child needed a nudge and the engine gave none. Not covered by the 2026-09-18 plan. Source: docs/handoffs/2026-09-18 - 00 - capped-delegate-report-loss.md F6.
