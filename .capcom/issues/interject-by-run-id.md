---
title: Address InterjectChild by run id, not spawn call id
status: done
type: task
priority: 3
plan: 2026-09-25 - 00 - sub-agent-stop-and-session-retention-plan
aliases: [apogee-interject-by-run-id]
created: 2026-09-24T14:07:14Z
closed: 2026-09-26T04:43:56Z
reason: "Resolved by sub-agent-stop-and-session-retention plan item 2 (archived at 36bc4e6d): InterjectChild and run-view steering address a child by run id"
---

InterjectChild (and the run view's steering) addresses a child by its spawning call id; childRegistry.register is last-in-wins, so two delegations sharing a call id (duplicate ids in one reply, or nested collisions) steer the wrong child. Once plan docs/plans/2026-09-24 - 01 - subagent-premature-done-plan.md lands an engine-minted run id, address children by it. Supersedes ADR 0063 D1's addressing by spawn call-ID.
