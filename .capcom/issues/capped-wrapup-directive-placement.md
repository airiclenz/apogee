---
title: "Capped delegate: wrap-up directive fenced on the closing tool result, not the system prompt (P1)"
status: done
type: bug
priority: 1
plan: 2026-09-18 - 00 - capped-delegate-report-loss-plan
aliases: [apogee-1zz]
created: 2026-09-18T21:03:18Z
closed: 2026-09-19T09:57:14Z
reason: Shipped by plan 2026-09-18 - 00 (commits 3163948f..3e5a017a); CHANGELOG [Unreleased] carries the entries
---

The directive is fenced onto the last tool result (engine fence [engine — wrap-up]); the system copy is only the fallback. Both wires degrade a tool-less request's tail to user role so this is the tail-message signal without a profile switch. Plan item 4. Source: docs/handoffs/2026-09-18 - 00 - capped-delegate-report-loss.md F1, P1; ADR 0076 D6 addendum.
