---
title: Session record closes a delegation's call entry by call id alone
status: done
type: bug
priority: 3
aliases: [apogee-record-close-by-call-id]
created: 2026-09-24T16:22:31Z
closed: 2026-09-24T16:49:30Z
reason: appendToolResult pairs a result with its open call by run (RunID, Depth, SpawnCallID, SpawnRunID where both carry one) via answers(), mirroring the TUI, not by call id alone
---

internal/run/transcript.go:166 (transcriptFold.appendToolResult) closes the open toolCall entry by CallID alone. Two sibling delegations with colliding call ids can close each other's entry in a mid-run save, even though SpawnRunID is now available to match on exactly. Deferred from plan docs/plans/archived/2026-09-24 - 01 - subagent-premature-done-plan.md.
