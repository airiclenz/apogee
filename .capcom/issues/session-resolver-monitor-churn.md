---
title: Session seat builds a Monitor on every fault when its entry has no fallback
status: done
type: bug
priority: 3
plan: 2026-11-03 - 01 - open-bugs-plan
created: 2026-11-02T22:07:31Z
closed: 2026-11-03T17:58:59Z
reason: "Resolved by item 2 of plan '2026-11-03 - 01 - open-bugs-plan' (archived in b3ffc752): a session seat with no fallback builds no Monitor per fault; TestTUIFallbackNoFallbackEntryBuildsNoMonitor pins it."
---

`sessionFallback.resolver()` (cmd/apogee/fallback.go:609) builds the bound binding's Monitor (`f.binding().Monitor()`, a provider client, no network) before `newSeatResolver` (cmd/apogee/fallback.go:466) returns nil for an entry whose primary names no `fallback:`. Since `f.current` stays nil, every fault on such an entry builds a fresh Monitor and throws it away. Before the seat-resolver constructor (plan `2026-11-02 - 04 - seat-and-resolved-entry-plan.md`, item 3), the Monitor was built only when a fallback list existed.

## Why

Wasted construction on every upstream fault of the common no-fallback entry, introduced by item 3 of the seat-and-resolved-entry plan.

## Acceptance

- A fault on a session-seat entry with no `fallback:` builds no Monitor; a test pins it.
