---
title: /compact zeroes the gauge readings but leaves isEstimateNewer set
status: open
type: bug
priority: 3
created: 2026-11-01T13:59:11Z
---

The /compact reset (internal/tui/commandrun.go:651) zeroes liveStats.ctxUsed and ctxEstimate but leaves liveStats.isEstimateNewer (internal/tui/model.go:719) as it was. The flag is set by a depth-0 ContextEstimateEvent (internal/tui/fold.go:202) and cleared only where a non-Maintenance reading sets ctxUsed (internal/tui/fold.go:187). It is inert today, since the gauge rule (internal/tui/model.go:4638) reads nothing while both values are 0, but a later change to that rule could trip over a stale flag after a compaction. Deferred from the 2026-11-01 tui-row-detail-gauge-and-notify run (item 7).

## Acceptance

- The /compact reset clears isEstimateNewer with ctxUsed and ctxEstimate, and a fold test pins the flag false after a compaction.
