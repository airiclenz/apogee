---
title: A running Firing holds no session record
status: done
type: bug
priority: 2
plan: 2026-09-24 - 00 - review-findings-follow-through-plan
aliases: [apogee-firing-holds-no-record]
created: 2026-09-24T09:16:54Z
closed: 2026-09-24T12:47:50Z
reason: "Resolved by review-findings-follow-through-plan (30ad3bbc) item 14: run.Once holds spec.RecordID while a Firing runs, so apogee undo is refused with HeldError"
---

apogee undo <id> takes store.Hold (cmd/apogee/undo.go), but run.Once takes no Hold on spec.RecordID, so the verb can rewrite a live Firing's journal.
