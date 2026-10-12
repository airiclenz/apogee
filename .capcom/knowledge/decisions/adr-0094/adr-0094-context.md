---
title: ADR 0094 context
---

ADR 0086 kept delegation blocking. ADR 0089 then let a Workflow run in the background and wake the
agent when it ends, but it said `sub_agent` stays blocking. So a frontier agent that wants one
helper to work while the conversation goes on has two poor choices. It can block on `sub_agent`, and
then every message the user sends waits until the child is done. Or it can wrap the one task in a
`fan_out`, and then it gets a one-line Receipt back instead of the child's report.

Bead `apogee-background-sub-agent` asks for the missing shape. The owner settled it on 2026-10-05
(plan `docs/plans/2026-10-05 - 00`) as option A: run the delegation as a one-item background
workflow on ADR 0089's machinery. Option B, an early placeholder result in the blocking tool round,
was rejected.
