---
title: "Vision refusal names a blank server when no servers: entry is named"
status: done
type: bug
priority: 3
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-vision-refusal-blank-server]
created: 2026-10-03T18:24:40Z
closed: 2026-10-03T23:28:51Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) item 3: unnamed server reads 'this server does not accept images'"
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 12 (7ba17a81). internal/agent/loop.go:1413 (used at :1462 and :1479): the vision refusal formats cfg.ServerName, so a session with no named servers: entry reads 'server "" does not accept images'.
