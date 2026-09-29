---
Status: accepted
Amends: ADR 0063 (D4 — a level of the view stack may be a workflow stage, which is not a run), and reverses the engine-run workflows plan's item 43 rule that the workflow block "paints one way and never collapses" and that "a head with many runs has no run view"
---

# Workflow stages are enterable views

## Context

ADR 0087 lets the engine run a Workflow: the model asks with `fan_out`, or the human launches a
Recipe with `/<id>`. Each item of a Workflow is done by a child run. The first cut
(`docs/plans/archived/2026-09-27 - 00 - engine-run-workflows-plan.md`, item 43) drew a Recipe's
Workflow as one block that "paints one way and never collapses". Every item run's narration and tool
calls painted railed beneath that block, in the order they happened. A `fan_out` card did the same
under its own fold, and "a head with many runs has no run view".

That was readable for two items and not for twelve. The item runs ran side by side, so their
entries interleaved, and one item's work could not be read on its own. An item run could also not be
reached the way ADR 0063 lets a reader reach any delegation: there was no view to open, no box to
message it from, and no row for `^x` (ADR 0086) to stop it on. The items are sub-agent runs like any
other, but they had none of the surfaces a sub-agent run has.

## Decision

**A workflow block shows one row per stage, and each row opens that stage's work in the transcript
slot, the way a run view does.** The item runs no longer paint in the conversation.

**D1 — One row per stage, every stage from the start.** The block keeps its header
(`✦ Workflow <name> — running`, then `waiting for you`, `finished`, `stopped` or `failed`). Beneath
it stands one row for each stage of the Plan, in its order, from the moment the Workflow starts. A
row wears a delegation row's shape: the stage's name, a dotted leader, and its state in the outcome
slot — `pending`, `running` (`2/5 · running` for a stage of more than one item),
`waiting for you`, `done`, `failed` or `stopped`. A stage that has not started is dim and has no ▶.
A stage one of whose item runs has started wears ▶, because it now has something to open. A stage
that ended with every item `ok` earns the ✓. A stage a repeat re-runs keeps **one** row, which moves
on to each round and says so: `round 2/3 · running`. Beneath the rows the block keeps a line for
each item whose receipt is not `ok` (`<stage> · <item> — <status> — <summary>`), an `ask` stage's
question, and — once the Workflow has ended — the totals line and a failure's cause.

> **Amended 2026-09-29.** A row can also read `skipped`: the stage never ran, because its `when:` was
> false or the stage it works over was skipped. Like a pending row, it is dim and has no ▶. A stage's
> finished phase now carries its outcome (`done`, `failed`, `stopped` or `skipped`), and the row
> paints that outcome. It no longer infers the outcome from its item counts, so a merge that left no
> report reads `failed` and not `done`. An empty outcome, from an emitter that reports none, falls
> back to the counts: `stopped` when an item the stage counted never finished, `done` otherwise.

> **Amended 2026-09-29.** A block is one **run** of a Workflow, not the Workflow itself. Re-running
> a stopped Recipe's Workflow — the same recipe line, which finds it by its plan hash (ADR 0087
> D4) — opens a new block under the new call and seats its item runs there; the stopped block stays
> frozen as it ended, and no later phase of the same Workflow id repaints it. A re-issued `fan_out`
> call draws a card of its own, as every call does.
> Every phase folds into the newest block of its Workflow id, and a started phase for an id whose
> newest block is still running is a duplicate, not a new run. A stopped Recipe block also ends on
> the command that re-runs it, carried on its started phase: `` re-run `<line>` to resume `` for a
> typed launch, `` run `/<id>` again with the same inputs to resume `` for one launched with its
> inputs bound. A `fan_out` card shows none; its model reads the equivalent line in the call's
> result (ADR 0088 D3, amended 2026-09-29). The command is kept in the session record with the block's
> structure (D5), so after a session resume a replayed stopped block still shows it, and re-running
> it in the resumed session opens a new block exactly as in the live one.

**D2 — Each item run has a run head of its own.** When an item's run starts, a head for it is seated
under the Workflow's block, inside the block's span. It is a delegation's head in every way a view
asks: it opens as the run's view, `^x` stops that one run, a message typed in its view reaches its
child, and the status line's gauge states its fill. It never groups into a `✦ Sub-Agent (N)` list.
The item's receipt folds onto it as the run's report, so its row reads the receipt's summary. A
retried item keeps **one** row, which opens its latest attempt. `/usage` keeps one row per Workflow.

**D3 — A stage opens as a stage view, or as its one item's run view.** Opening a stage row — a
motionless click, or `⏎` on the block cursor — does one of three things:
- a stage none of whose items has started (a pending stage, or a stage that runs no child, such as
  `ask` or `script`) opens nothing;
- a stage of one item, run once, in one attempt, opens that item's run view directly;
- every other started stage opens a **stage view**: a new level of the view stack that lists one
  delegation-style row per item, with the item's receipt summary on its row. A repeated stage
  groups its items under `round N` sub-headers. An item that was retried shows its earlier attempts
  as dim `attempt N` sub-rows beneath its row, each of which opens its own run view.

The breadcrumb reads `← main › <workflow> › <stage>` on a stage view and on a one-item stage's run
view, and `← main › <workflow> › <stage> › <item>` on an item opened from a stage view. `esc`, or a
click on the band, goes up one level, as ADR 0063 D4 says of every level.

A stage view is **not a run**. Its prompt box is read-only and says so
(`stage <name> · read-only · esc back`), and a message sent there is refused with a flash that
points to the items. It states no context gauge, since it fills no window. `^x` stops no stage: on
an item's row under the block cursor it stops that item's run, as on any delegation row, and
stopping a whole Workflow stays `/workflows`' `^x` (ADR 0089). A stage row answers `^x` with
nothing.

**D4 — A `fan_out` card has one stage, so it lists its items.** The card the model's `fan_out`
call draws paints one enterable row per item beneath it, each opening that item's run view directly
(its crumb names the item). The card's own fold, born collapsed, hides the card's own body and never
those rows.

**D5 — The structure is kept, so the rows reopen after a resume.** The session record keeps the
workflow block's structure (its stages, their states and rounds, and the finished items) beside the
block's text, and it keeps each item run's head. A resumed session paints the same stage rows, and
they open read-only views over the item runs replayed beside them. A Workflow that was still running
when its record was written died with the engine that ran it, so on replay its running stages read
`stopped` and so does the Workflow. A block from a record written before the structure was kept
paints its text, and its item rows stay painted beneath it as the way into its runs. The open view
itself stays Driver state and is never restored (ADR 0063 D4 stands).

**D6 — What does not change.** A **Background workflow** (ADR 0089) still stays out of the
conversation: it keeps its status-line readout and `/workflows`, and none of this applies to it.
Headless and daemon runs still print none of these phases.

## Considered options

- **Keep painting the item runs inline under the block.** It needs no new surface, but it is the
  interleaving this decision exists to end.
- **One row per item in the conversation, with no stage level.** The reader would reach an item in
  one press, but a forty-item fan-out stage would put forty rows in the conversation. The stage is
  the unit the Recipe names and the reader launched, so it is the unit the conversation shows.
- **A stage view for every stage, one-item stages included.** It is one shape instead of two, but
  it costs a press to reach the only run there is. The one-item stage opens its run, and its crumb
  still names the stage.
- **Show a stage only once it starts.** The block would grow as it ran, and the reader could not see
  what was still to come. Every stage shows from the start and reads `pending` until it starts.

## Consequences

- The engine-run workflows plan's item 43 rule is reversed: the workflow block still has no fold of
  its own, but it paints its stage rows rather than its item runs, and every item run has a view.
- ADR 0063 D4 is amended: a level of the view stack is a run or a workflow stage. A run still has
  exactly two shapes (ADR 0063 D5) — its row and its view — and an item run is a run.
- `layout.md` ("The workflow block", "Run view"), `CONTEXT.md` (**Stage view**, **Workflow**) and
  `docs/manual/workflows.md` describe the new shape.
- Nothing here is a Mechanism. The rows and views change only what the human sees and can reach,
  so there is nothing for Bypass to switch off, and the Bypass floor is untouched.
