---
title: "Cancel (Esc×2) keeps finished Turns: drop only the in-flight Turn, keep committed tool results and finished sibling delegations on the wire"
status: done
type: feature
priority: 3
related: [engine-run-delegation]
plan: 2026-09-27 - 00 - engine-run-workflows-plan
aliases: [apogee-2un]
created: 2026-09-14T20:28:23Z
closed: 2026-09-28T15:34:36Z
reason: "Stage B delivered by plan 'engine-run-workflows-plan' item 2 (archived at 8d66b4f3): a cancelled delegation pool keeps finished children, stops running ones with a bounded fold, and commits the not-started result for queued ones (Stage A landed earlier via 2026-09-19 plan)."
---

Session-mining headline 5 / item 13: 48 sessions saved with messages: null; two cancels wiped a saved plan and three reviewer reports (30a3b2df), three of four finished scouts discarded (439c7787). Today AbortExchange drops from exchangeBoundary(); the quit half was fixed in def800f1. Reverses ADR 0013 D5 / ADR 0017 §2 — needs its own grill on the wire shape (Turn kept with no answer vs snapshot-to-record vs bridge note; Interjected mark per ADR 0025 D8/D9). Anchors: internal/agent/agent.go AbortExchange, internal/tui/model.go foldCancelled, dispatch.go runDelegationPool (sibling results committed only after the group joins). Deferred from the 2026-09-14 session-mining plans by owner call.

## Log

- 2026-09-19 beads: plan 'docs/plans/2026-09-19 - 01 - ten-urgent-beads-plan.md' items 6-8 implement Stage A (settle the Exchange: finished Turns kept, cut marked by a live-only engine note). Stage B (finished siblings of a cancelled pool, amending ADR 0013 §5 / ADR 0039) stays open here and needs its own grill.
- 2026-09-20 beads: the fan-out ceiling (plan 2026-09-20 - 00) bounds the loss Stage B would recover to one round (delegate-fanout-rounds × width).
- 2026-09-25 Airic Lenz: 2026-09-25: ADR 0086 kept this separate. A single-delegation stop (apogee-single-delegation-stop) now covers most of the need and shares its code sites (runSubAgent's cancel path, dispatch.go runPool — the bead's runDelegationPool anchor is stale).
- 2026-09-26 Airic Lenz: 2026-09-26: Stage B (finished sibling delegations survive Esc×2) is folded into apogee-engine-run-delegation: durable on-disk runs would keep finished items and resume them. The grill in 'docs/handoffs/2026-09-26 - 00 - engine-run-sub-agent-orchestration-planning.md' decides whether that plan closes or supersedes this bead.
