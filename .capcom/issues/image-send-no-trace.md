---
title: Sent image leaves no mark in the live transcript
status: done
type: bug
priority: 3
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-image-send-no-trace]
created: 2026-10-03T18:24:41Z
closed: 2026-10-03T23:28:53Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) items 7-8: sent user block shows the attached row, persisted with the session"
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' items 12-13. internal/tui/model.go:2137-2138: the pending images ride the send (in.Images) but transcript.addUser gets only in.Text, so an image-only send leaves an empty user block in the live transcript, and nothing on screen marks the attached image after it is sent.
