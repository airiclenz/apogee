---
title: Daemon Firings record no per-server stats
status: done
type: bug
priority: 3
aliases: [apogee-daemon-no-server-stats]
created: 2026-09-24T04:48:57Z
closed: 2026-09-24T05:59:39Z
reason: Fixed in d00f0f9c; race run pending on a TSan-capable host
---

The daemon Driver's Firing path (cmd/apogee/daemonfire.go:367, raise(ctx, firingInputs{...})) does not wrap its event sink with the statsRecorder (cmd/apogee/serverstats.go:92), so scheduled runs fired by the daemon append nothing to ~/.apogee/server-stats.jsonl, while the TUI, headless and TUI /schedule Drivers do. Item 9's Goal named only the TUI and headless Drivers. Deferred from plan docs/plans/archived/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md item 9.
