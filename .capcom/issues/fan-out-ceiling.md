---
title: "Fan-out ceiling: a reply's sub_agent calls past delegate-fanout-rounds × width are refused; orientation states width, ceiling and step cap; running row shows the cap; first-Esc hint names what stopping discards"
status: done
type: feature
priority: 1
plan: 2026-09-20 - 00 - fan-out-ceiling-and-delegation-facts-plan
aliases: [apogee-ux5]
created: 2026-09-20T09:29:27Z
closed: 2026-09-21T20:34:51Z
reason: Shipped by plan 2026-09-20 - 00 (archived at c8b04049); CHANGELOG [Unreleased] carries the delegate-fanout-rounds key, the ceiling and the Delegation bounds orientation line.
---

Session 20260920T070407Z-0fd8eaff (/code-audit on deepseek-v4-flash, width 4): the coordinator fanned out 56 sub_agent calls in one reply, 35 never started, no result reached it for 1h41m, Esc rolled the whole Turn back (16 finished delegates lost from the conversation). Also: the skill asked max_steps 120, the host clamped to 80, and the clamp note rides only the result — the coordinator never learned it. Plan docs/plans/2026-09-20 - 00 - fan-out-ceiling-and-delegation-facts-plan.md (grilled 2026-09-20).
