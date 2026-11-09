---
title: Prompt box shows no scrollbar when its content outgrows the box
status: done
type: task
priority: 2
plan: 2026-11-09 - 03 - prompt-box-scrollbar-plan.md
created: 2026-11-09T14:31:59Z
closed: 2026-11-09T17:42:48Z
reason: "Resolved by plan '2026-11-09 - 03 - prompt-box-scrollbar-plan' (archived at 60ccb441): overflow-only scrollbar column, wheel scrolls the overflowing draft, layout.md states the rule."
---

The prompt box stops growing at `maxInputRows` and the textarea then scrolls internally, but the only sign of the hidden rows is the `+N` elision marker in the box's top border (`Model.inputElisionEdge`). Other TUI surfaces — the transcript, the boxed pop-ups (`popupRowScrollbar`) — show a scrollbar column when their content overflows; the prompt box alone leaves the human without a visual scroll affordance, so it is not obvious the box can be scrolled or how much is hidden beyond the count.

Code, traced 2026-11-09: `promptEditor.rows` (`internal/tui/prompteditor.go:444-452`) clamps to `maxInputRows`; `Model.hiddenDraftRows` (`internal/tui/model.go:3239`) derives the hidden-row count; the marker is `inputElisionEdge` (`internal/tui/model.go:3966`), which renders a border row, not a scroll affordance. No scrollbar exists on the prompt box (`grep -i scroll internal/tui/prompteditor.go` finds none).


## Acceptance

- When the draft's wrapped rows exceed the box's height (the `maxInputRows` cap or the frame's row ceiling), the box shows a vertical scrollbar column that reflects the hidden rows — the same overflow rule the transcript and the boxed pop-ups follow.
- While the box shows the scrollbar it is usable: scrolling the hidden draft rows with the mouse wheel or the keyboard reaches every row of the draft.
- When every wrapped row fits, no scrollbar column (and no reserved gap for it) appears — the box at `minInputRows` with one line of text is unchanged.
- A frame test pins the bar for an overflowing draft and its absence for a fitting one, and `layout.md` (or `docs/layout/prompt-box-layout.md`) states the rule.
