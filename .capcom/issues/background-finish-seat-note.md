---
title: Background workflow's finish note omits the seat-fallback note
status: done
type: bug
priority: 3
aliases: [apogee-background-finish-seat-note]
created: 2026-09-28T20:29:53Z
closed: 2026-09-29T05:59:57Z
reason: "Fixed in 164b9f0a: a background workflow's finish note ends on SeatFallbackNote once when an item fell back"
---

Deferred from plan docs/plans/2026-09-28 - 00 - enterable-workflow-stages-plan.md (item 2). A background workflow's end message does not go through workflowAnswer (internal/agent/workflowcall.go:530), so it carries no answer-level SeatFallbackNote when an item fell back from the sub-agents server to the session server: finishNote (internal/agent/background.go:1203) builds the parent's line without consulting seatFellBack (internal/agent/workflowcall.go:545). Its items' phase results do carry the note.
