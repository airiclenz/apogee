---
title: "Vision refusal on an --endpoint run advises a servers: entry that does not exist"
status: done
type: bug
priority: 3
plan: 2026-10-04 - 00 - console-vision-fork-followups-plan
aliases: [apogee-endpoint-vision-advice]
created: 2026-10-04T09:01:33Z
closed: 2026-10-04T11:42:56Z
reason: items 3-4 resolved by plan 'console-vision-fork-followups' (docs/plans/archived/2026-10-04 - 00 - console-vision-fork-followups-plan.md), archived at e1d81cb2
---

visionRefusal (internal/agent/loop.go) says 'server %q does not accept images: set vision: true on its servers: entry'; the TUI ctrl+v flash restates it as noVisionNote (internal/tui/clipboard.go). An --endpoint run has no servers: entry: config.resolveStartupEntry builds an unnamed entry, ApplyConfig names it via aliasFromEndpoint and sets opts.StartupEphemeral, and there is no flag to turn vision on. The user is told to edit an entry they do not have. Nothing downstream of ApplyConfig (domain.Config, RebindSpec) knows the bound server is ephemeral.
