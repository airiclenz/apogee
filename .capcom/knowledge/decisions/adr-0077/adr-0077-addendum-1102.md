---
title: "ADR 0077 addendum of 2026-11-02: the time-budget notice is the third structural bound notice — the ladder is unchanged"
---

Decided by the owner on 2026-11-02 (architecture review of that day, card #7, the delegate bounds);
shipped by plan `docs/plans/2026-11-02 - 05 - request-room-and-delegate-bounds-plan.md` item 9.

A delegate's third bound, `delegate-timeout`, gains the notice its step and token siblings carry
(`adr-0077-addendum-0919b`): the **time-budget notice**, a third structural engine note. It is
fired at depth ≥ 1 on the first tool result committed once the wall clock the time bound itself
reads (from the head of the child's Run, through the Agent's injected clock) reaches
**ceil(0.75 × delegate-timeout)**, which is 1h30m at the default 2h. It is fenced
`[engine — time budget]` … `[end engine — time budget]` and reads
`time: 1h30m4s of 2h0m elapsed — 29m56s left before the wrap-up Turn; write your output now`
(`prompts/time-notice.txt`): the time elapsed, the cap and the time left, in whole seconds, each
spelled as the time-limit markers spell the cap, so the default reads `2h0m`. The share is the
same fixed ¾ as its siblings, with no key.

It shares everything else with them. The three notices are rows of one bound table
(`boundRows`, `internal/agent/delegatelimits.go`), read at one seam (`budgetNotices`) in
`appendToolResult`. The latch is the note's own presence in the conversation, so a fold, a prune
stub or a rollback that removes it lets the next result carry it again. It is silent at depth 0
and for an unbounded timeout (`delegate-timeout: 0`), books no firing and stays on under Bypass.
The fence order on a result is now tool output, advice, step note, token note, time note, and the
wrap-up directive last. Like the other trailers it counts inside the result's share of the room
left (ADR 0018 `adr-0018-amend-1102b`). It is **not a rung of this decision's ladder**: the
ladder, its three rungs, its switch and the `context-fill-notice` Reaction are unchanged.
