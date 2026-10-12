---
title: A 10s bwrap probe that times out reports confinement unavailable for the whole session
status: done
type: bug
priority: 2
labels: [slow-box]
aliases: [apogee-3mh]
created: 2026-09-22T16:03:20Z
closed: 2026-09-23T06:01:53Z
reason: Closed
---

internal/platform/namespace_linux.go:111 namespaceProbeTimeout = 10 * time.Second bounds one real bwrap launch at startup. On expiry the result is "bwrap timed out" — confinement is reported unavailable for the entire session, i.e. a silently weaker sandbox. A cold-cache namespace setup on a loaded box is exactly the case that exceeds it.

Found by the slow-box deadline sweep, 2026-09-22.

## Acceptance

A slow probe does not downgrade the sandbox; either the budget is generous enough for a cold start, or a timeout is reported distinctly from an unavailable bwrap.

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af (slow-box-deadlines plan, items 3 and 4, commits 255be681 and 66af40ba). Both halves of the acceptance landed, not just one.

  - internal/platform/namespace_linux.go: namespaceProbeTimeout 10s -> 60s, with the reasoning recorded (the budget is only spent on a host that is failing to answer, so a generous ceiling costs a healthy host nothing; a Pi paging its first bwrap off an SD card stops being told it cannot fence when it can). The stated price: a truly wedged bwrap now stalls startup for a minute.
  - The timeout is now a TYPED cause, never collapsed with a refusal: internal/domain/confinement.go gained ConfinementCause with CauseBackendAbsent / CauseProbeTimedOut / CauseLaunchRefused on ConfinementCaps, and probeNamespace returns 'bwrap timed out' with domain.CauseProbeTimedOut versus 'bwrap refused: <stderr>' with domain.CauseLaunchRefused. Every caps-returning site was updated (confiner_linux.go, confiner_windows.go, landlock_linux.go, seatbelt.go, platform.go) and the confinement execution contract documents the cell.
  - internal/probe/confinement.go renders the distinction, so the degraded-confinement notice names WHY the backend cannot fence instead of one undifferentiated 'unavailable'.

  Acceptance met.
