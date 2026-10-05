---
Status: accepted
Amends: ADR 0086 ("we keep the blocking model" — a `sub_agent` call may now ask for the background), ADR 0087 ("exactly two sources" — a third: `sub_agent` background; D2's unchanged `sub_agent`, D9's no-delegation rule and D10's entry points for that third source), ADR 0089 (D1's launchers and the lead's "`sub_agent` stays blocking"; D3's one-line note; D5's save-as-recipe), ADR 0039 (the `Amended 2026-09-20` fan-out ceiling no longer counts a background `sub_agent`)
---

# sub_agent may run as a one-item background workflow

## Context

ADR 0086 kept delegation blocking. ADR 0089 then let a Workflow run in the background and wake the
agent when it ends, but it said `sub_agent` stays blocking. So a frontier agent that wants one
helper to work while the conversation goes on has two poor choices. It can block on `sub_agent`, and
then every message the user sends waits until the child is done. Or it can wrap the one task in a
`fan_out`, and then it gets a one-line Receipt back instead of the child's report.

Bead `apogee-background-sub-agent` asks for the missing shape. The owner settled it on 2026-10-05
(plan `docs/plans/2026-10-05 - 00`) as option A: run the delegation as a one-item background
workflow on ADR 0089's machinery. Option B, an early placeholder result in the blocking tool round,
was rejected.

## Decision

**A `sub_agent` call may carry `background: true`. The engine then runs that one delegation as a
one-item Background workflow and answers the call at once. The child works exactly as a blocking
child does. Only the delivery changes: its report comes back in the workflow's finish note.**

**D1 — The gate is `fan_out`'s.** The `background` switch is on `sub_agent`'s schema only where
ADR 0089 D1 shows `fan_out`'s background switch: the model class is bench-approved for it
(`OffersBackground`) and the tool policy lifts `workflow`. So a model that can start a background
`sub_agent` can also check, stop and message it with the `workflow` tool (ADR 0089 D4). Everywhere
else `sub_agent`'s schema is byte-identical to today's. The bench judges this switch per model class,
apart from `fan_out` and its own switch.

**D2 — The child runs the blocking `sub_agent` path.** It is a `sub_agent` child, not a workflow
item child. It writes a prose report and never calls `finish`. It keeps `max_steps` and the step-cap
clamp, the `read-only` tool roster, `output_path`, and `sub_agent`'s own depth rule
(`delegate-max-depth`), so it may delegate where a blocking child may. ADR 0087 D9 ("workflow
children do not delegate") binds `fan_out` and Recipe items only. The workflow item records the
outcome as a Receipt the engine builds itself, and keeps the child's report as the item's output.
The item runs once: it is never retried.

**D3 — The finish note carries the report.** ADR 0089 D3's note is one line, but a background
`sub_agent` was asked for its report. Its finish note opens on one lead line with no item counts,
`sub_agent <name> <outcome> — transcript: <path>`, whose `— transcript: <path>` part names the
child's transcript in the workflow folder and is there only when one was written. Under that line
comes the child's report, inline, under the same 64 KiB cap a blocking result has
(`delegateResultMaxBytes`). The note is delivered, and wakes the agent, exactly as ADR 0089 D3 says.

**D4 — Retention and `continue`.** A named background child is retained when it finishes, under the
same rule as a blocking one (ADR 0086 D1). A capped, faulted or stopped one is retained under any
name it wore. A later `sub_agent` call can `continue: "<name>"` it. That continuation runs
blocking: a call that carries both `continue` and `background: true` is refused.

**D5 — Capacity is ADR 0089 D2's.** The workflow runs at the Parallel-agents width minus one, so a
one-item workflow takes one slot. A second background workflow on the same server waits in line,
whether a `sub_agent` or a `fan_out` started it.

**D6 — Not in the tool round.** The call returns at once, so the child is never a member of the
reply's tool round. It is not subject to ADR 0025's preempt: a message sent now never skips it. It
is not counted against ADR 0039's fan-out ceiling either: that ceiling bounds the full reports a
coordinator must read before the round returns, and this report arrives later, on its own note.

**D7 — Headless and daemon run it blocking.** With no conversation to go on (ADR 0089 D1), those
Drivers offer no background, so they never show the switch. Anywhere the engine cannot keep a
background workflow, a `background: true` call runs as a plain blocking `sub_agent`.

**D8 — Each call is a new run.** The workflow's plan hash is salted with the call id. Re-issuing the
same task therefore starts a fresh run and never resumes a finished one, and ADR 0087 D4's refusal
of a blocking re-issue against a folder a background run still drives never fires between two
`sub_agent` calls. The salt enters only this kind of plan: every `fan_out` and Recipe plan hashes as
before.

> **Amended 2026-10-05 — the salt is the call id plus a minted nonce.** A call id is the upstream's
> choice, not the engine's, and nothing guarantees it unique: a server that numbers its calls afresh
> each reply hands the same id again, and a re-issue of the same task with that id would hash to the
> earlier run's plan and resume its finished folder instead of running. The plan is therefore
> salted with the call id **and** a run id the engine mints for the call from the tree's run-id
> minter, so every background `sub_agent` call is a new run whatever id it arrives with. A crash
> resume or a `/workflows` re-run reads the plan back from the run's own folder, nonce included, so
> it still finds and drives that folder.

**D9 — `/workflows` lists it as a `sub_agent` workflow.** The listing names its origin as
`sub_agent`. It offers no save-as-recipe, because one delegation is not a recipe. Inspect, stop and
re-run work as for any workflow (ADR 0089 D5).

**D10 — ADR 0031 holds.** The engine emits the finish event and the Driver decides whether to wake;
nothing in the engine writes to the wire. The bench drives the switch through the root facade like
any other Driver.

## Considered options

- **Option B: an early placeholder result.** The blocking round would return a stub at once and the
  report would replace it later. It rewrites a result the model has already read, and it reaches the
  tool round's commit order. Rejected by the owner.
- **A `fan_out` with one item.** It works today, but the agent gets a Receipt where it wanted a
  report, and it has to write a brief template for one task. The verb a small model gets right is
  still "one helper": `sub_agent`.
- **A spawn/message/wait tool family (ADR 0086's option A).** It is still not built. The `workflow`
  tool already covers status, stop and message, so this ADR adds one switch and no tool.

## Consequences

- `sub_agent` gains an optional `background` field on gated classes. Every other class, and every
  headless and daemon run, sees the tool unchanged.
- A Workflow now has three sources: `fan_out`, a Recipe, and a background `sub_agent`. Not every
  item's child calls `finish`: a `sub_agent` item's Receipt is the engine's.
- The TUI paints the call as a background-workflow call, and its finish note may span many lines.
- CONTEXT.md's **Workflow**, **Background workflow** and **Sub-agent** entries speak it.
