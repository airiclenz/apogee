---
title: Pop-up scrollbar sits flush against the content with no gap column
status: done
type: task
priority: 3
plan: 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan
created: 2026-11-01T10:19:29Z
closed: 2026-11-01T14:08:29Z
reason: Item 2 resolved by plan 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan (archived at 639c3bfc)
---

In boxed pop-ups (seen in the /thinking pane), the vertical scrollbar takes the column directly next to the text. Wrapped lines run right up to the bar, which looks cramped. The transcript already leaves a gap here: `layout.md:2-4` says "one column stays free between the text and the scroll bar". The pop-up rule (`layout.md:682-693`) promises no such gap.

Code, traced 2026-11-01:

- The shared painter is `popupRowLines` (`internal/tui/popup.go:887-898`): `rowInner := inner - scrollbarWidth`, with `scrollbarWidth = 1` (`internal/tui/model.go:2783`). `popupRowScrollbar` (:914, line 931) appends the bar cell directly after `squareOnField(..., rowInner)`.
- The theme's `Padding(0,1)` (`internal/tui/theme.go:530-540`) puts its pad column *outside* the bar, between the bar and the border, not between the text and the bar.
- /thinking wraps at `thinkingWrapColumn` (`internal/tui/thinkingpane.go:70`) = `inner - popupRowIndent - scrollbarWidth`, so text can fill the cell next to the bar.

## Acceptance

- Every boxed pop-up whose list overflows leaves one blank column between its content and the scrollbar, using the same rule as the transcript.
- The gap column is reserved only while the bar is shown, matching the existing "only while the list overflows" rule.
- `layout.md` §pop-ups states the gap, and a frame test pins it for the /thinking pane and one list pop-up.
