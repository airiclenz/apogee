---
title: Named delegations stay continuable for the whole session
status: done
type: feature
priority: 2
plan: 2026-09-25 - 00 - sub-agent-stop-and-session-retention-plan
aliases: [apogee-session-delegate-retention]
created: 2026-09-25T18:44:35Z
closed: 2026-09-26T04:43:55Z
reason: "Resolved by sub-agent-stop-and-session-retention plan (archived at 36bc4e6d): named delegations retained for the session, saved, fork-cut, /clear-dropped, rollback-restored"
---

ADR 0086 D1-D3: retain every named completed delegation (plus capped/faulted/stopped as today) across Exchanges, saved as an additive 'retained' snapshot key. Entry = original task + rounds (instructions, report); continue seeds newest-first into 4096 tokens; refusal lists 16 most recent + '(and N more)'. /clear drops, fork keeps pre-cut entries, cancelled-Turn rollback restores the pre-Turn set (fixes two leaks), resume/live restore loads. Fix sub_agent schema 'in this conversation' drift. See docs/adr/0086-a-delegation-is-stopped-singly-and-a-named-one-stays-continuable-for-the-session.md.
