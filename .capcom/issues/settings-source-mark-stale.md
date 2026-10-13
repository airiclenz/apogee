---
title: /settings source mark stays at the boot resolution after a save
status: done
type: bug
priority: 3
plan: 2026-10-13 - 00 - open-bugs-sweep-plan
aliases: [apogee-settings-source-mark-stale]
created: 2026-10-10T17:01:00Z
closed: 2026-10-13T09:16:00Z
reason: "Resolved by plan '2026-10-13 - 00 - open-bugs-sweep-plan' item 8 (archived at 77f00a79): /settings source marks and editability follow the layered files on disk after every save, reset and external reload; boot env/flag overrides still rank first."
---

Deferred from plan 'docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md' item 5. The /settings source mark comes from the boot resolution (cmd/apogee/settingsrows.go:281 settingSource reads config.Options.SourceOf for this run), so an external save or a pane edit does not move a row's mark until relaunch — an edited default row reads `true * (default)`.
