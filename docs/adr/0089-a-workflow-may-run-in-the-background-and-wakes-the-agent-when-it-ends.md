---
Status: accepted
Amends: ADR 0086 (its "we keep the blocking model and do not build asynchronous delegation" now binds `sub_agent` alone), ADR 0007 (an Exchange may open on an engine note rather than a user message)
---

# A workflow may run in the background and wakes the agent when it ends

## Context

ADR 0086 kept delegation blocking and rejected asynchronous delegation "for now, not denied". The
rejected shape was a spawn/message/stop/wait tool family around `sub_agent`: it would give small
models four more tools, and it would reach about ten subsystems.

ADR 0087 makes a Workflow an object that outlives a reply. It has a folder, receipts on disk, and
resume by hash. That removes most of the reasons asynchrony was hard. In the 2026-09-27 grill the
owner put background workflows in the first cut, launchable by the user and by the agent, and gave
the agent a full control tool.

## Decision

**A Workflow may run in the background while the conversation goes on. When it ends, a one-line
note reaches the agent — and wakes it if it is idle.** `sub_agent` stays blocking; ADR 0086's
rejection of async *delegation* stands for it.

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md)).** `sub_agent` no longer stays blocking without exception: on the
> classes this ADR's gate opens for, a `sub_agent` call may carry `background: true` and runs as a
> one-item Background workflow. ADR 0086's rejection of a spawn/message/wait tool family stands.

**D1 — Two launchers.**
- **The user** launches a recipe skill in the background.
- **The model** launches one through `fan_out`'s `background` switch.

The switch, and the `workflow` tool (D4), are shown only to model classes the bench has approved.
Every other class sees `fan_out` as blocking-only, as it sees `fan_out` itself only where ADR 0087
D8's gate is open. Headless and daemon runs offer no background: with no conversation to go on, a
workflow there blocks.

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md) D1, D7).** A third launcher: **the model** through `sub_agent`'s
> `background` switch, shown behind the same gate as `fan_out`'s (the class is bench-approved and the
> tool policy lifts `workflow`). Headless and daemon runs never show it, and a background `sub_agent`
> the engine cannot keep runs blocking.

**D2 — Capacity.** A background workflow runs at the server's Parallel-agents width minus one, so
one slot stays free for the conversation. On a width-1 server it has to share that one slot: the
workflow's children and the conversation's requests take turns. A second background workflow on the
same server waits in line behind the first.

**D3 — The finish note, and the wake.** When a background workflow ends, the engine emits a finish
event. The note it carries is one line: name, item counts by status, headline receipt tallies, and
report path.
- **A reply is under way:** the note is delivered at the next between-Steps boundary, as an
  Interjection is (ADR 0025).
- **The agent is idle:** the Driver opens a new Exchange on the note itself. This is the **wake**.
  The wake's reply is bounded by the Mode and the approval rules like any other reply.

The engine only emits the event; the Driver decides to wake (ADR 0031's wire-silent engine). The
`workflow-wake` config key, `on` by default, turns the wake off. With it off, the TUI shows the
finish and the note rides on the user's next message. ADR 0007 is amended: an Exchange may open on
an engine note as well as a user message.

> **Amended 2026-09-30.** The note's item counts are the engine's tally of the run (`workflow.Tally`):
> its fan-out items only, each counted on the receipt its latest round ended on, a skipped fan-out
> left out; a verify or merge receipt is not an item. The finished or stopped end phase carries the
> same tally on its Go event (the `workflow_phase` NDJSON line is unchanged), so the note, the TUI's
> totals and background finish line, the `/workflows` listing, and the headless and daemon exit
> verdict count the same item set. The note's text is unchanged. A blocking re-issue of a workflow
> whose folder this background run still drives is refused (ADR 0087 D4, amended 2026-09-30).

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md) D3).** "The note it carries is one line" binds a `fan_out` or
> Recipe workflow. A background `sub_agent`'s note opens on the lead line
> `sub_agent <name> <outcome> — transcript: <path>`, with no item counts and the transcript part
> only when one was written, and carries the child's multi-line report under it, under the blocking
> result's 64 KiB cap. Delivery and the wake are unchanged.

**D4 — The `workflow` control tool.** A model shown the background switch also gets `workflow`, with
three actions:
- **status:** every workflow in the session, or one in detail (stages, item counts, receipts so far);
- **stop:** stops a workflow and keeps its finished items (ADR 0088);
- **message:** sends a message to one running item's child, by the run id or item name the status
  listing shows. It rides the child's mailbox exactly as a human Interjection does (ADR 0063).

**D5 — Lifetime.**
- `esc` stops the current reply and never a background workflow (ADR 0088 D3).
- `/clear` with a workflow running asks whether to stop it. A kept workflow's finish note goes to
  the new conversation.
- Quitting apogee stops every running workflow. Resuming the session resumes them from their
  folders.
- The user inspects, stops, re-runs failed items of, and saves-as-recipe any workflow from the
  `/workflows` view.

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md) D9).** A background `sub_agent` lists with a `sub_agent` origin and
> offers no save-as-recipe: one delegation is not a recipe.

## Considered options

- **Background for user-launched recipes only.** It is simpler, but a frontier agent could not park
  a long audit and keep working, which is the case the owner values most. The per-class gate keeps
  small models on the blocking path instead.
- **Wait for the user instead of waking.** It is kept as the `workflow-wake: off` setting. It is not
  the default, because the agent (or the user) started the workflow to use its result.
- **Stop-only control (a `stop` field on `fan_out`).** It is smaller, but the agent could neither see
  progress nor redirect a helper going wrong. The owner chose the full tool, behind the same bench
  gate.

## Consequences

- The TUI gains a `/workflows` view and a status-line indicator for background workflows. The
  headless NDJSON stream gains workflow events.
- The bench judges the background switch and the `workflow` tool per model class, separately from
  `fan_out` itself.
- CONTEXT.md gains **Background workflow**. **Exchange** gains the wake as a second way one opens.
