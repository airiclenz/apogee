---
title: apogee probe model --timeout 0 silently unbounds the battery
status: done
type: bug
priority: 3
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-probe-model-timeout-zero]
created: 2026-09-23T04:59:08Z
closed: 2026-09-23T06:55:59Z
reason: "Item 6: --timeout 0/negative keeps the default, tested and documented; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Item 7 of 'docs/plans/2026-09-22 - 01 - slow-box-deadlines-plan.md' gave 'apogee probe model' a --timeout flag (default five minutes, bounding ONE attempt). '--timeout 0' is accepted and maps to the provider's 'zero implies unbounded' case, so a user can silently disable the battery bound; no test and no line of docs/manual/probe.md mentions it. Decide whether zero means 'the default' or 'unbounded', then pin it with a test and say so in the manual.
