---
title: /settings source mark stays at the boot resolution after a save
status: open
type: bug
priority: 3
aliases: [apogee-settings-source-mark-stale]
created: 2026-10-10T17:01:00Z
---

Deferred from plan 'docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md' item 5. The /settings source mark comes from the boot resolution (cmd/apogee/settingsrows.go:281 settingSource reads config.Options.SourceOf for this run), so an external save or a pane edit does not move a row's mark until relaunch — an edited default row reads `true * (default)`.
