---
title: Live config reload announces loader notices (unknown keys, migrations) in the TUI
status: done
type: task
priority: 3
aliases: [apogee-ibd]
created: 2026-09-14T16:05:16Z
closed: 2026-09-16T22:06:37Z
reason: live config reload announces loader notices (941e07f7)
---

Ratified 2026-09-14 while writing docs/plans/2026-09-14 - 00: the unknown-key notice lands at startup only. The ADR 0041 live-reload path (cmd/apogee/settingsedit.go externalEdit.projection) passes func(string){} to config.ApplyConfig and discards every loader notice; carrying them from externalEdit.changed() into the TUI's applied-settings notice line is a separate seam.
