---
title: After /stop-server the staged flush opens on the first failed beat, under the offline debounce
status: done
type: bug
priority: 3
labels: [tui]
created: 2026-11-24T13:32:36Z
closed: 2026-11-24T14:25:53Z
reason: "A launcher verb's release primes the offline debounce one failure short (Model.primeOfflineCrossing), so the first failed beat after it crosses offline and the flush retry stays held; the wake now also asks blockedUpstream (wakeIfIdle). Tests: TestFailedOwedBeatHoldsTheFlush, TestFailedOwedBeatHoldsTheWake, TestWakeWaitsWhileOffline, TestLandedBeatAfterReleaseRestoresTheDebounce. ADR 0024 D7, ADR 0029 amend-1123 and ADR 0089 D3 amended 2026-11-24."
---

After `/stop-server`, the owed beat (`actuation.beatOwed`, adr-0029-amend-1123) is paid by the first beat sent after the release, whether it reached the server or not. On a stopped server that beat fails. With `hb.everOnline` set, one failure is below `offlineFailureThreshold` (2), so `foldBeatFailure` leaves `hb.offline` false (internal/tui/heartbeat.go, foldBeatFailure). `flushAfterFold`'s gate (`canRetryFlush` and `!blockedUpstream`, internal/tui/interject.go) then passes, and the staged rows open an Exchange into the server the human just stopped. The same holds for a pending workflow wake (`canOpenWake`).

The amendment promises that the flush retry "then asks `blockedUpstream` like a human opener", but after a stop that guard cannot fire until the second failed beat, about one `heartbeat.Interval` (60 s) later. This was already true before `stale-beat-pays-owed`. That fix only made sure the paying beat is one sent after the release.

Possible directions (owner call): pay the debt only with a beat that landed reachable or crossed offline, or believe the first failed beat after a launcher verb's release at once, as the cold start and a `/server` switch do.

## Acceptance

- After `/stop-server` with a staged row, a failed beat after the release neither flushes the row nor wakes a workflow; a test drives this case with an unreachable beat
