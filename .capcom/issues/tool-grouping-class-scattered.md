---
title: "A tool's grouping class is one registry column (architecture review 2026-11-21 #7)"
status: parked
type: task
priority: 3
labels: [tui]
created: 2026-11-21T12:27:13Z
---

Found by docs/reviews/architecture-review-2026-11-21.html, verified 2026-11-21 at 171d210f.

Problem: whether a tool joins the Tools umbrella, forms its own group or stands alone is assembled from scattered pieces: the `solo` column plus a nil `target` (task_list) in internal/tui/toolregistry.go:213-224, :575-629; the `workflowFinishToolName` exemption; hard-coded own-group tool names (`sub_agent`, `load_skill`) in transcript.go:2610-2614 and render.go:913, :966; the outcome hook's `out.Solo` (toolbranch.go:404); and the bridge re-deriving the answered-ask_user solo rule on decode (transcriptbridge.go:625-627). Smaller: distinct-path counting relies on a "path" argument convention (`groupPathArg`) outside the rows, and a row's `verb` repeats `groupPhrase.live` (groupsummary.go:52). Making a lone delegation or skill fetch a group of one (7303b12b) meant edits in transcript, render and subagentblock instead of a row.

Solution: each registry row declares its grouping class; `groupable` derives from that plus the persisted verdict. toolshape_test and toolpresent_test become per-row tables. Per-tool label, target and verb phrase are already one row each and stay. Builds on #1 (tool-block-shape-twice).

Must keep: ADR 0083 §1 (presenter verdicts stay on the wire, so the persisted `Solo` stays), ADR 0031 (no domain or engine field).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
