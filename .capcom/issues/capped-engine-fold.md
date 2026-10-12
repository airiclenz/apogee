---
title: "Capped delegate: engine fold is the partial result (P3)"
status: done
type: feature
priority: 1
plan: 2026-09-18 - 00 - capped-delegate-report-loss-plan
aliases: [apogee-e2a]
created: 2026-09-18T21:03:02Z
closed: 2026-09-19T09:57:14Z
reason: Shipped by plan 2026-09-18 - 00 (commits 3163948f..3e5a017a); CHANGELOG [Unreleased] carries the entries
---

On every bound the child's conversation is folded by the engine (context.Summarize, delegate brief) and handed to the parent under [engine summary]; the child's closing reply follows only when it is a report. Plan items 1–2. Source: docs/handoffs/2026-09-18 - 00 - capped-delegate-report-loss.md F5, P3.
