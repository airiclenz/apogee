---
title: Let sub_agent run in the background as a one-item workflow
status: done
type: feature
priority: 2
related: [subagent-preempt-wave]
plan: 2026-10-05 - 00 - subagent-wave-and-background-plan
aliases: [apogee-background-sub-agent]
created: 2026-10-05T12:41:47Z
closed: 2026-10-05T16:11:30Z
reason: "Resolved by plan 'subagent-wave-and-background-plan' (items 6-12; archived at 959f9dd9): ADR 0094, sub_agent background:true as a one-item background workflow behind fan_out's gate, finish note, /workflows, manual."
---

## Goal
The model should be able to receive the user's messages while sub-agents run. Today a wave of `sub_agent` calls is one blocking tool round. Both wire formats need a result for every tool call before a new user message, so the model hears the user only after the whole wave ends. (The preempt workaround and its problems are in apogee-subagent-preempt-wave.)

## Decision taken (owner, 2026-10-05): option A
Give `sub_agent` a `background: true` switch that runs the delegation as a **one-item background workflow**. It reuses ADR 0089's machinery as is, with no new tool family:
- the tool call returns at once with a handle (run id / workflow name), and the conversation goes on;
- the finish note, delivered at the next between-Steps boundary or as a wake when the agent is idle (ADR 0089 D3, `workflow-wake` key);
- the `workflow` control tool for status, stop and message (ADR 0089 D4), plus the `/workflows` view, the status-line indicator, and resume from the folder (ADR 0087);
- capacity: Parallel-agents width minus one (ADR 0089 D2).

## Rejected: option B
Early "still running" placeholder results with the real results later. That builds async delegation a second way, beside workflows.

## Constraints
- **ADR needed.** ADR 0086 keeps `sub_agent` blocking, and ADR 0089 says its rejection of async delegation "binds sub_agent alone". A new ADR must amend or supersede that clause in both (ADR 0086 and ADR 0089's front-matter/decision).
- **Bench gate.** Show the switch only to model classes the bench has approved, the same gate as `fan_out`'s `background` (ADR 0089 D1). Every other class keeps blocking `sub_agent`. This is the Floor invariant: nothing may make a model do worse than the bare loop.
- No background in headless or daemon runs (no conversation to go on); there it blocks, as for workflows.
- ADR 0031 north star: the engine emits only; the Driver decides on wake.
- Check named-delegation continuability (ADR 0086: a named one stays continuable for the session) and single-stop semantics for a background child. Check how ADR 0063 run views open it.
- Domain docs: CONTEXT.md (Delegation / Background workflow), docs/manual. If the tool count or the default menu changes, the README counts are pinned by tests (TestReadmeStatesTheToolCounts).

## Related
- apogee-subagent-preempt-wave: the "finish the wave, then deliver" fix for the blocking path. It is still needed for model classes without the switch.
- Skills (code-audit) that drive waves through `sub_agent` could switch to background `fan_out` on approved models even before this lands.

## Acceptance

- An ADR (amending ADR 0086 and ADR 0089) records sub_agent's background switch as a one-item background workflow behind the bench gate.
- On an approved model class, sub_agent background:true returns at once; the user's messages reach the model while the child runs; the finish note arrives at the boundary or wakes an idle agent; the workflow tool and /workflows can see, message and stop it.
- Unapproved classes, headless and daemon see blocking sub_agent unchanged.
- Driven tests cover the launch, a mid-run user message, and the finish note/wake; CONTEXT.md and docs/manual are updated.
