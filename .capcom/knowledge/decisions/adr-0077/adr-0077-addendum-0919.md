---
title: "ADR 0077 addendum of 2026-09-19: the step-budget notice leaves the Reaction ladder — superseding the 2026-09-15 addendum"
---

The 2026-09-15 addendum is superseded. The step-budget notice is **not** an engine advise Reaction
and has no switch: it is a **structural floor** of every bounded delegation, fired by the engine
at depth ≥ 1 — on everywhere, Bypass included — as an engine note on the closing tool result
(`[engine — step budget]` … `[end engine — step budget]`, the wrap-up directive's fence, never
the advice fence). The `step-budget-notice` key, its `/settings` row, the `Options`, `Config` and
`Generation` fields and the reserved Reaction id are removed; a home config still carrying the key
is exempted from the unknown-key walk, read-only — nothing is stripped or rewritten. The notice
books no firing, and a compaction fold re-arms it as it re-arms the fill notice (amended
2026-10-01: there is no re-arm call — the notice's latch is its note's own presence
(`Conversation.HasEngineNote`), so a fold that swallows the note leaves the next result to be told
again). Evidence: the
2026-09-18 capped-delegate handoff §3 F3 — a delegate read the notice's advice fence on a
`read_file` result as part of the file it read, so the fence header must say *engine*, not
*reaction* — and the 2026-09-14 session-mining review's headline 3: announcing the cap is part of
the bound's contract, which a model-shaping switch cannot be allowed to withhold. The
`context-fill-notice` Reaction and this decision's other parts stand untouched. Plan `2026-09-18 -
00` items 6 and 7.
