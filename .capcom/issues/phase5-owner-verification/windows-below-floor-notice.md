---
title: Degradation notice on a below-floor Windows host (< build 17763)
status: parked
type: task
priority: 4
labels: [owner-run, windows]
aliases: [apogee-2uh.3]
created: 2026-09-03T18:40:50Z
---

- **Degradation notice on a below-floor Windows host (< build 17763)** — the deny-vs-token
  decision itself is table-proven (`TestBelowWindowsFloor` in the untagged `winguard.go`
  predicate); only the on-host UX observation of the notice stays untested (recorded so in
  [ADR 0020](.capcom/knowledge/decisions/adr-0020.md)'s
  consequences), verifiable only if such a host turns up.
