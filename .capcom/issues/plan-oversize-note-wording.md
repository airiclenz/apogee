---
title: Plan 2026-09-03 - 01 quotes the pre-task-list oversize-note wording as a verbatim move
status: done
type: bug
priority: 2
labels: [docs, residue, tui]
aliases: [apogee-nec]
created: 2026-09-03T21:13:23Z
closed: 2026-09-04T04:06:41Z
reason: Plan 01's quoted oversize literal updated to the widened task_list wording
---

The unexecuted plan docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md:99 specifies the standing-content oversize warning as a verbatim move into internal/notice, quoting the literal "— trim context files or the system prompt". Item 7 of plan 2026-09-03 - 00 widened that literal to "— trim context files, the task list or the system prompt" (internal/tui/model.go:793, pinned by internal/tui/contextfiles_test.go:127). Executing plan 01's sentence literally would revert the widened wording. Update the plan's quoted literal before that item runs.
