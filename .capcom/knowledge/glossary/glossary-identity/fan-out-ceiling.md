---
title: "Fan-out ceiling: how many `sub_agent` calls one reply may fan out before the rest are refused"
---

**Fan-out ceiling**:
How many `sub_agent` calls ONE reply may fan out before the rest are refused:
`delegate-fanout-rounds` (a file-only key, default **2**; `0` switches it off) **rounds** of the
width the engine states to the model — the **Sub-agent server**'s cap once it has stated one since
the seat last moved, else the session server's **Parallel agents** cap, and **1** on a sub-agent.
The first `rounds × width` `sub_agent` calls in emitted order run as any group does; every later
one in the reply commits, in call order, the tool result `sub-agent not started: this reply fanned
out N delegations and the ceiling is C (R rounds × width W) — the first C ran; delegate the rest
again once their results are in` — followed by ` — for more items, use fan_out` while `fan_out`
is on the refusing Agent's roster — with a finished phase and no started one — the **Interjection**
skip's shape — takes a `refused` row in the **delegate ledger**, and leaves the group without a
width line (the refusal already names the width). Leaf tools in the same reply are never counted.
Relative to the width because the width is what one round costs in time: two rounds is two rounds
on any server. There is deliberately **no floor** and it applies at **every depth**: an unkeyed
local server has width 1, so at the default rounds a reply of three delegations there refuses the
third, and a sub-agent's own ceiling is the round count itself. Refused rather than held and
re-issued by the engine: the bound exists so a coordinator cannot commit itself to more work than
it can read a result of before the group returns (the 2026-09-20 session it closes fanned 56 out at
width 4, 35 never started, and lost the lot to one Esc), and the model is told the ceiling before
its first call by the **Orientation block**'s `Delegation bounds:` line and the `max_steps`
schema text. It binds `sub_agent` alone: a `fan_out` **Workflow** runs its items in waves of the
width with no ceiling, since each item hands back a one-line **Receipt** rather than a full report,
and when `fan_out` is enabled the refusal names it (ADR 0087 D7). A `sub_agent` call carrying
`background: true` is outside it too: it returns at once and runs as a one-item **Background
workflow** outside the reply's tool round, so it is neither counted against the ceiling nor
refused by it (ADR 0094 D6).
Ratified 2026-09-20 (ADR 0039, amended the same day; amended 2026-09-27 by ADR 0087 and
2026-10-05 by ADR 0094).
_Avoid_: "fan-out cap" (the cap is the width; this is a count of rounds of it), "delegation
limit" (says nothing about what is bounded — calls per reply, not per session).
