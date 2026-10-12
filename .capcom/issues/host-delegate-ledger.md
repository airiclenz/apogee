---
title: Host-side delegate ledger (name → outcome → output exists) handed to the coordinator
status: done
type: feature
priority: 3
plan: 2026-09-19 - 01 - ten-urgent-beads-plan
aliases: [apogee-clb]
created: 2026-09-14T20:28:25Z
closed: 2026-09-20T06:57:55Z
reason: implemented by docs/plans/archived/2026-09-19 - 01 - ten-urgent-beads-plan.md (merged to main, closeout 5a415e13)
---

Session-mining item 10b: coordinators misremembered which delegates faulted and recovered only by listing OUT files on disk (50e9123c STATE [137]-[138]); a capped task was re-dispatched verbatim (6b3ca4b5 [563], 7fd98ffb ×5). Shape: a per-Exchange []delegationRecord on the Agent exposed as a read-only delegation_log tool or an orientation line; a byte-identical re-dispatch of a capped task gets a one-line note. New model-facing surface — Floor-guard/advise class with a key if it steers. The per-result validations (missing output_path, vendor markup, sub-sentence result) land in plan 2026-09-14 - 03. Deferred by owner call.

## Log

- 2026-09-19 beads: (plan 2026-09-18 - 00, P6): the 'never re-dispatch a capped task verbatim' half of this bead is covered — a capped delegation's result now ends on '[to continue this delegate: sub_agent with continue: "<name>"]' and sub_agent's continue: argument restarts it from the engine fold under a fresh cap (internal/agent/children.go, subagent.go; CONTEXT.md Step cap; ADR 0013/0022 addenda of 2026-09-18). The ledger half (name -> outcome -> output exists, handed to the coordinator) remains open here.
