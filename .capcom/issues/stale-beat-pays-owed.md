---
title: A beat sent before a launcher verb's release pays the owed beat
status: done
type: bug
priority: 3
labels: [tui]
created: 2026-11-24T11:48:24Z
closed: 2026-11-24T13:36:32Z
reason: Every launcher verb's release arms a fresh beat chain (Model.armOwedBeat), so a beat sent under the latch lands inert; TestStaleBeatDoesNotPayTheOwedBeat covers stop and unload; adr-0029-amend-1123 amended 2026-11-24.
---

Deferred out of plan `2026-11-23 - 06 - architecture-review-cleanups-plan.md` (item 12): with `/stop-server` or `/unload-model`, a heartbeat sent before the hold's release but folded after it also clears `actuation.beatOwed` (internal/tui/heartbeat.go:275), because `foldActuationDone` keeps the beat chain's generation and does not re-arm it at release (internal/tui/actuation.go:385). The staged-message flush and a workflow wake (`canRetryFlush`, `canOpenWake` via `engineHolds.isBeatOwed`, internal/tui/engineholds.go:140) can then open before any beat has observed the server after the verb. This matches the plan's rule ("a beat has folded since the release").

## Acceptance

- Only a beat sent after a launcher verb's release clears beatOwed (or the rule is recorded as accepting the in-flight beat), with a test for the stop/unload case
