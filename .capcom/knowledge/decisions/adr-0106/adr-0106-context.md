---
title: ADR 0106 context
---

The owner asked for the terminal's mouse pointer to show what a click does while it hovers
(issue `pointer-shapes-on-hover`, design sketch of 2026-11-02). It is distinct from the text
caret: `cursor-shape` (DECSCUSR) is shipped and stays as it is.

What the code had when the plan was written (2026-11-03, base c8feea33):

- **apogee captures the mouse.** View sets `tea.MouseModeCellMotion`, so the wheel scrolls and a
  click positions the caret or selects. Capturing the mouse is also why the terminal's own
  selection and its own pointer feedback never show.
- **Button-less motion is ignored on purpose.** `handleMouseMotion` drops it, and the settle
  path never repaints on a `tea.MouseMotionMsg` (`TestMouseMotionNeverRepaints`). Cell-motion
  tracking reports motion only while a button is held, so a hover produces no traffic at all.
- **The frame already classifies every cell.** The click and wheel chain (`pointerPanes`,
  `clickArm`, the pane table in `panes.go`) knows whether a point is over the editable prompt
  (ADR 0025's `inputEditable`), the transcript, a toggleable tool block, a pane row or the
  footer's mode marker.
- **Raw escapes already go out on demand.** `reassertMouse` writes the mouse modes again through
  `tea.Raw` after tool results and on resize, because the renderer writes mouse modes only when
  they change and a tool child can reset terminal state underneath it.

OSC 22 is the only cross-terminal pointer-shape mechanism, and it is not universal. The kitty
specification (https://sw.kovidgoyal.net/kitty/pointer-shapes/) sets a shape with
`OSC 22 ; <name> ST`, resets it with an empty name, and answers a query `OSC 22 ; ?a,b,c ST` with
`OSC 22 ; 1,0,1 ST`, one positional flag per name. It keeps separate shape stacks for the main
and the alternate screen. kitty, Ghostty, WezTerm and foot speak its CSS names; xterm speaks X11
names; an older terminal ignores the sequence; tmux and ssh hops may strip it or answer nothing.

ADR 0040 rejected OSC 11 light/dark auto-detection because detection is unreliable and its
failure mode is an unreadable screen the user cannot override. This case is different. A
terminal that stays silent here costs nothing: the opt-in just stays off, and the screen is
exactly what it is today.

The owner ratified the transcript-body shape, the tmux posture, the config key and the choice to
record the gate as an ADR on 2026-11-03. The plan's writer settled the gate, the query timing and
the reset points the same day (`docs/plans/2026-11-03 - 03 - pointer-shapes-on-hover-plan.md`).
