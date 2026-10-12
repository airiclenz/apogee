---
title: probe.GatherHost hands a nil Confiner a zero Cause
status: done
type: bug
priority: 2
plan: 2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan
aliases: [apogee-probe-host-nil-cause]
created: 2026-09-23T04:58:53Z
closed: 2026-09-23T06:55:57Z
reason: "Item 2: nil Confiner reports CauseBackendAbsent; resolved by plan '2026-09-23 - 00 - slow-box-follow-ups-and-stale-state-plan' (archived at 152dc896)"
---

internal/probe/host.go:73 initialises 'caps := domain.ConfinementCaps{}' when in.Confiner is nil — FSWrite false with an empty Cause. Item 3 of 'docs/plans/2026-09-22 - 01 - slow-box-deadlines-plan.md' introduced domain.ConfinementCaps.Cause with the invariant, stated in its field doc, that a cause is set at every site returning caps with FSWrite false and is empty exactly when FSWrite is true. This is the one place that invariant does not hold.
