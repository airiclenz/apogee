---
title: "sub_agent continue: the retained fold is consumed before the run_on and tools refusals, so a refused continuation loses it"
status: done
type: bug
priority: 3
plan: 2026-09-18 - 00 - capped-delegate-report-loss-plan
aliases: [apogee-if9]
created: 2026-09-19T09:56:52Z
closed: 2026-09-19T16:00:12Z
reason: "Implemented by docs/plans/archived/2026-09-19 - 00 - deferred-follow-ons-plan.md (items ✅ DONE 2026-09-19; commits 9f1b6a7f 0a01b4ce 104d0c93 4e2d1413 and the if9 item); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

In internal/agent/subagent.go:865-906 a `continue` call takes the retained entry (a.retained.take, subagent.go:866) BEFORE the seat (`run_on`, parseDelegationSeat, subagent.go:~893) and roster (`tools`, requestedChildTools, subagent.go:~903) refusals, so a continuation whose own call carries a bad run_on or an unknown tool name is refused AND loses the retained fold — a corrected retry then meets the unknown-name refusal. Reorder the take after those resolutions (or re-retain on refusal). Reasoned from code, not observed failing. Deferred out of plan 2026-09-18 - 00 (item 9).
