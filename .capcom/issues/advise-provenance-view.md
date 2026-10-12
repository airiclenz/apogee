---
title: "/settings 'what the model saw this Turn': surface the advise provenance ledger"
status: done
type: feature
priority: 4
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-d6t]
created: 2026-09-09T07:19:56Z
closed: 2026-09-20T06:57:56Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

ADR 0076 D6 says /settings CAN show the advise spans the model saw this Turn. Stage 3 records the ledger (domain.AdviceSpan on the tool-result Message) and shows spans only in the hidden debug view (ReactionFiredEvent Detail) and on the wire (/inspect). A /settings or transcript surface listing {reaction, origin, moment, turn} per span is unbuilt.
