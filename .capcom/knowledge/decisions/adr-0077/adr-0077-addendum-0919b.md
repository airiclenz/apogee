---
title: "ADR 0077 addendum of 2026-09-19: the token-budget notice is the second structural bound notice — the ladder is unchanged"
---

Defect `apogee-zuu` (session `20260918T143011Z`): a delegate at 10.7M cumulative prompt tokens
under the default 20M `delegate-max-tokens` budget heard nothing, because the context-fill ladder
measures **one request's fill against the working window** — a 1.3M window kept every rung silent
— and the step-budget notice counts Turns. The two bounds are independent, so the step notice gains
a **twin**: the token-budget notice, a second structural engine note fired at depth ≥ 1 on the tool
result that closes the Turn at which the child's cumulative prompt tokens (its own usage tally, the
figure the bound is enforced against) reach **ceil(0.75 × delegate-max-tokens)** — 15M at the
default — fenced `[engine — token budget]` … `[end engine — token budget]`:
`tokens: 15.2M of 20.0M spent — 4.8M left before the wrap-up Turn; write your output now`. It
rides the same latch and re-arm seams as the step notice (once while its copy survives; a fold, a
prune stub or the rollback of the Turn it rode re-arms it) (amended 2026-10-01: neither notice
keeps a latch or re-arm seam of its own — the latch is the note's own presence in the
conversation, `Conversation.HasEngineNote` on its topic, so a fold, a prune stub or a rollback that
takes the note away leaves the next result to be told again, and nothing is reset; plan
`2026-09-30 - 01` item 12), lands beside it in `appendToolResult`
so the fence order on a closing result is fixed (tool output, advice, step note, token note, the
wrap-up directive last), is silent at depth 0 and for an unbounded budget, books no firing and
stays on under Bypass. It is **not a rung of this decision's ladder** — the ladder, its three
rungs, its switch and the `context-fill-notice` Reaction stand untouched — and the default budget
stays 20M: the per-call lever for a child whose single requests run large is `working-window:`.
The token renderer shared with the fill notice gains an M tier, so a fill notice over a window of
a million tokens or more now reads `1.3M` where it read `1300k`. Plan `2026-09-19 - 01` item 3.
