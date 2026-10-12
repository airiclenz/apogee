---
title: No test asserts the TUI's own Config.Report wiring (wire_boot.go)
status: done
type: task
priority: 3
labels: [reactions, stage-3, tests]
aliases: [apogee-a3m]
created: 2026-09-12T20:07:01Z
closed: 2026-09-12T21:05:41Z
reason: fixed in 0fa84a23
---

Deferred out of the stage-3 run (plan 2026-09-09 - 01, item 10). The session Config literal in cmd/apogee/wire_boot.go:233 sets `Report: w.bridge.NotifyHook` so a failed sync-lane (advise:/gate:) reaction's one line reaches the TUI's notice line, but no test asserts it: the reporter seam is proved only at `firingConfig` (`TestFiringConfigWiresTheSyncLanesReporter`, cmd/apogee/wire_firing_test.go:1112). A dropped `Report:` in the TUI's Config literal would go unnoticed. Missing test: a TUI-side wiring assertion that the bound engine's Config.Report is the bridge's NotifyHook (or that a sync-lane failure reaches the notice line).
