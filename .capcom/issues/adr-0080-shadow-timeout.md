---
title: ADR 0080 decision 6 still states the superseded shadow-scan contract
status: done
type: task
priority: 2
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-adr-0080-shadow-timeout]
created: 2026-09-23T04:58:41Z
closed: 2026-09-23T06:55:55Z
reason: "Item 1: ADR 0080 and 0056 carry dated amendments; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

Item 2 of 'docs/plans/2026-09-22 - 01 - slow-box-deadlines-plan.md' replaced git_commit's commit-secrets pre-check contract: the budget is now one 30 s context covering all four shadow-index git runs, and an expired budget or a git failure after the repository resolved forces the approval look instead of skipping the check. docs/adr/0080 decision 6 still records the superseded contract — 'each shadow run carries a 2 s timeout ... every failure skips the check'. The code names the supersession, but the ADR was outside item 2's Files list, so the decision record now contradicts the shipped behaviour. Remedy: a dated amendment on ADR 0080 decision 6 (and check ADR 0056 decision 4, which the same item supersedes).
