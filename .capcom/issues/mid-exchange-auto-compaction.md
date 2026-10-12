---
title: Mid-Exchange auto-compaction for the MAIN loop (fire at Turn boundaries under budget pressure)
status: parked
type: feature
priority: 3
aliases: [apogee-1bf]
created: 2026-09-03T18:40:20Z
---

**Status:** parked 2026-07-05 (guided-decomposition grill); the CHILD half shipped 2026-08-26.
A child agent now folds at quiescent *Turn* boundaries under budget pressure
(`midExchangeCompaction` — `internal/agent/compact.go`, set by `newChildAgent`), because a
delegation's whole life is one Exchange and the boundary the trigger waits for never comes for it.
What stays parked is the same lift for the MAIN loop, where a long multi-Turn Exchange still has
no generative reducer available for its entire life — the only thing that reduces mid-Exchange
there is the tool-result-cap **Floor guard** (the `tool-result-cap` key, on by default), and it
trims the projected request rather than folding the conversation. That change touches a structural
reducer's contract on the very arm the bench measures (it interacts with the saturation logic, the
protected prefix, and bench comparability),
so it needs its own grill and bench evidence — deliberately not a rider on the decomposition work.
