---
title: "Write the Reaction core implementation plan (ADR 0076, staged: core → config → user cells)"
status: done
type: task
priority: 2
plan: 2026-09-07 - 01 - reaction-core-stage-1-plan
aliases: [apogee-pxd]
created: 2026-09-07T19:48:27Z
closed: 2026-09-08T15:12:09Z
reason: Stage-1 plan written 2026-09-07, executed and archived 2026-09-08 (all 22 items landed through d84dd988)
---

ADR 0076 (2026-09-07) decided one Reaction core over Moments with an origin×class policy matrix; doctrine only, no code yet. Write the docs/plans/ plan in the house format via /implement-plan Write mode. Stage 1 core: Moment, Reaction, single ladder, ReactionFiredEvent, delete the lab layer (registry, Deps, SwapCatalogue, EnableMechanisms, mechanisms: key, /settings row), internal/floor policy byte-identical, bench identity arm proves behaviour-identical. Stage 2 config: reactions: list, hooks: alias + one-time notice, three layers, adoption pin, repo config path on write deny list. Stage 3 user cells: gate as Approver stage (5s, escalate to ask), advise (10s fail-open, 8 KiB cap, trailer-on-tool-result slot, provenance ledger, ephemeral on resume, terminal redaction, exec fence), fixed-text machinery bench arm gates advise. Also: docs/manual/hooks.md + configuration.md rewrite; glossary entries Post-response decision / Deferred Response Action / Mechanism descriptor still describe the lab layer and need retiring with the code.

## Log

- 2026-09-08 beads: Plan written and EXECUTING. Run paused 2026-09-08 after item 11 of 22 (owner call: split across two sessions). Items 1-11 committed c2817e6b..d3920241; items 12-22 open. Resume with /implement-plan on the same plan file — the plan file is the resume state. Handoff: docs/handoffs/2026-09-08 - 00 - reaction-core-stage-1-run-part-1.md. Two FOLLOW-UPs gate closeout: tui TestFoldEventCoversEveryEventVariant (item 18 owns) and TestBenchReadinessContract (item 14 owns).
