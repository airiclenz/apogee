---
Status: accepted
Supersedes: ADR 0013 §5(b) (a cancel rolls the whole parent Turn back and keeps no partial result), ADR 0007's "cancellation rolls the whole Turn back" consequence
Amends: ADR 0039 D4 (a cancelled pool no longer drops its group), ADR 0086 D3 (a cancel no longer restores the retained set), ADR 0011 C3 (the Step settles rather than rolls back), ADR 0075 D12 (a cancelled delegation is no longer one the parent Turn rolls back)
---

# Cancel settles and never rewinds finished work

## Context

`esc×2` cancels the Exchange. Stage A of bead `apogee-2un` (2026-09-19) already made the cancel
**settle** rather than abort: the Turns that finished before the stop stay (`Agent.SettleExchange`).
The Turn in flight, though, is still rolled back. When it is a delegation pool, the pool joins every
child and then drops the whole group, finished siblings included (ADR 0013 §5(b), ADR 0039 D4), and
the retained-delegation set returns to its Turn-start value (ADR 0086 D3). The incident behind
`apogee-2un`: two cancels wiped a saved plan and three reviewer reports. Stage B, keeping the
finished siblings, stayed open because it needed a grill on the wire shape.

ADR 0087 makes a Workflow durable, so a cancelled `fan_out` would keep its finished items on disk
while a cancelled `sub_agent` pool beside it threw its finished children away. In the 2026-09-27
grill the owner settled both at once: "reverting is what git was built for; this does not need to
be in apogee."

## Decision

**A cancel stops work and keeps everything that finished; it never rewinds.** Undoing a file change
is git's job. Rewinding a conversation is `/undo`'s job, and `/undo` is unchanged.

**D1 — The Turn in flight is settled, not rolled back.** If the cancelled Turn issued tool calls,
its reply stays in the conversation and every call gets a result:
- a finished call keeps its real result;
- a running call ends through its `ctx` and gets `cancelled by the user while it ran`;
- a call that never started gets `not run: cancelled by the user`.

The Exchange then closes without an answer, with the `[engine — cancelled]` marker Stage A already
places. A Turn cancelled before its reply finished streaming issued nothing that ran, and is dropped
as today. An Exchange with no finished Turn is still scrapped (`Agent.AbortExchange`).

**D2 — A delegation pool keeps its finished children.** Under a cancel:
- a finished child's report is its result;
- a running child is **stopped** exactly as `^x` stops it (ADR 0086 D4): it gets a fold, a non-error
  partial result, and retention under its name, so the parent can `continue:` it;
- a queued child gets the not-started result.

The stop's summary is bounded: each running child gets **20 seconds** to fold, and one that cannot is kept with `summary unavailable`; a second `esc×2` while folds run skips the rest at once. A cancel that is a quit or a daemon shutdown (a distinct context cause) skips the folds entirely, so neither ever waits on a child (owner, 2026-09-27).

The retained set is no longer restored to its Turn-start value; what a finished or stopped child
left is kept. This closes Stage B of `apogee-2un`.

**D3 — A workflow is stopped and kept.** A blocking `fan_out` under a cancel stops its running
items and answers the call with how many items finished and the report-so-far path. A later
`fan_out` with the same brief, items and inputs resumes it (ADR 0087 D4). `esc` never touches a
**Background workflow** (ADR 0089); the `/workflows` view and the `workflow` tool stop those.

**D4 — Finished work written to disk stays written.** Nothing apogee does on a cancel reverts a
file. A settled Turn's results tell the model what happened, so its next request starts from the
truth rather than from a conversation that forgot work the workspace still shows.

## Consequences

- ADR 0013 §5(b) is superseded. §5(a) (no snapshot mid-child) and §5(c) (resume only before or after
  a delegation) stand: a child is still atomic to the *snapshot*, just no longer to the *cancel*.
- `apogee-2un` closes with the plan that implements this record.
- CONTEXT.md **Exchange** loses its "a cancel inside a delegation pool still rolls the whole parent
  Turn back first" clause. **Retained delegation** loses "restored by a cancelled Turn's rollback".
  **Stop (a delegation)** now names cancel as a stop of every running child.
