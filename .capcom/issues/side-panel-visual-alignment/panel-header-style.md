---
title: Panel section headers do not match transcript headings in tone or wording
status: open
type: task
priority: 2
labels: [tui]
deps: [panel-section-user-fold]
plan: 2026-11-09 - 04 - side-panel-visual-alignment-plan.md
created: 2026-11-09T18:56:44Z
---

**Today**
- The lead glyph, title and summary render as one bold white run (`panelHeader` = Bold + `userText`,
  `internal/tui/theme.go`, painted at `internal/tui/sidepanel.go:433`).
- The summary has no brackets: `◆ Tasks 1/3`.
- At the default 36-column width the default header cuts mid-word:
  `◆ Sub-agents 2 running · 1 queu… ⊙`, seen in every rendered frame.

**The rest of the UI**
- Transcript headings render the label bold in the tool-header tone (`toolLabel`) and the count in
  the muted count tone in parentheses: `✦ Sub-Agent (4)`, `✦ Task List (1/3)`
  (`typeRowPaint` / `umbrellaHeaderPaint`, `internal/tui/toolblock.go:254-303`).
- The two surfaces also name the same things differently:

  | Transcript | Panel |
  |---|---|
  | `Sub-Agent` | `Sub-agents` |
  | `Task List` | `Tasks` |
  | a not-yet-started run is `scheduled` | the same run is `queued` |

**Wanted**
- Header label in the transcript's heading tone, bold.
- Summary in parentheses, muted and not bold. The lead glyph is not bold.
- One vocabulary for section names and run phases across panel and transcript; pick the better
  word on each pair.
- A summary short enough not to truncate at width 36 in the common cases (for example `(2 · 1
  queued)`, or a count).

This depends on `panel-section-user-fold`, which fixes the header's lead and right-edge glyphs.

## Acceptance

- Rendered dark and light frames show the header label in the heading tone, bold, and `(summary)` muted and not bold.
- Section names and phase words match between the panel and the transcript (spec, glossary and manual follow).
- At width 36 the default Sub-agents header with 2 running and 1 queued does not truncate.
