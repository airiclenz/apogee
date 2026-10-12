---
title: Run view
---

**Run view**:
The surface one **Sub-agent** run is read and addressed in. Expanding a framed delegation — from the
block cursor or a click on its row — opens the run view rather than flipping a fold flag: the TUI's
transcript slot paints that run alone, rooted at its own task, opened on its latest line and
following it as it grows, while the status line, prompt box and footer stay exactly where they are
(a pane inside apogee's own frame, never an alternate screen — ADR 0035 stands). A clickable
breadcrumb header (`← main › planner › repo-scout`) and `esc` each go **one** level up, the status
line's right slot states the **viewed run's** context gauge once that run has reported usage — never
the top-level agent's — and reads `esc back` until it has (`esc back · ^x stop` while the viewed run
is running). `^x` stops the viewed run alone (**Stop (a delegation)**); `esc×2` at the top level
still cancels the whole Turn. Inside a view of a **running** child the prompt box addresses that child (see
**Interjection**); a view of a finished or scheduled one opens **read-only**. A run therefore has
exactly two shapes — the collapsed row and the run view — while the `✦ Sub-Agent (N)` umbrella
still opens inline to its member rows. It is **Driver state**: a stack of open runs in the TUI's
`Model`, never encoded in the transcript, never written to a **Session record** and never restored,
so a resumed session opens at the top level. Ratified 2026-08-30
(ADR 0063).
A **Workflow**'s item runs are runs like any other and open the same way, from their stage's row
or its **Stage view**
(ADR 0090).
_Avoid_: "full screen" (the frame's other rows stay — only the transcript slot is taken),
"expanded sub-agent" (the inline expanded shape is gone; a run has no fold state), "drill-down".
