---
title: Fork counts the parent's pre-cut sub-agent and Workflow spend as its own
status: done
type: bug
priority: 2
plan: 2026-10-04 - 00 - console-vision-fork-followups-plan
aliases: [apogee-fork-inherits-delegate-spend]
created: 2026-10-04T09:01:34Z
closed: 2026-10-04T11:42:56Z
reason: item 6 resolved by plan 'console-vision-fork-followups' (docs/plans/archived/2026-10-04 - 00 - console-vision-fork-followups-plan.md), archived at e1d81cb2
---

sessionHost.Fork (cmd/apogee/wire_session.go) zeroes Meta.Usage/DelegateUsage/Currency, but forkAt (internal/tui/fork.go) copies the transcript prefix with each run head's usage (UsageCostMicros, priced/unpriced counts). delegateUsageTotal sums those heads, so the fork's /usage delegate rows, session row, footer spend and first Save (Meta.DelegateUsage, the /sessions cell) include the parent's delegate spend D_pre. Summing parent + fork records counts D_pre twice. If the parent was resumed under another currency label, those micros are shown and saved under the fork's label, breaking ADR 0093 decision 6. The main-agent half (usageBase) is correct.
