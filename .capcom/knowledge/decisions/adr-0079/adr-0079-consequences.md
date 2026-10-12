---
title: ADR 0079 consequences
---

- `internal/domain` gains `ContextCost`; `internal/agent` gains `Agent.ContextCost()` as a sibling
  producer over the `standingBlock` renders; the facade gains the `apogee.ContextCost` alias
  (`ContextCost` is not an Event, so `TestEveryDomainEventVariantIsAliased` is untouched).
- `internal/agent/testdata/contextcost.golden` is the bytes-per-piece golden; the eventlines goldens and the pinned
  `run_finished` line in `TestWriterEnvelopeOrderAndNulls` are refreshed.
- `apogee probe context` and `apogee probe context --live` ship; `docs/manual/probe.md` and
  `docs/manual/headless.md` describe the table and the frame keys.
- CONTEXT.md carries **Context cost** beside **Context files** and **System prompt**.
- Skill bodies and `@file` references stay outside the report: they are per-message, not Turn-1
  standing cost (ADR 0061 D2).
