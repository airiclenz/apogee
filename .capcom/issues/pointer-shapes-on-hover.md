---
title: Pointer shape on hover in the TUI (OSC 22)
status: open
type: feature
priority: 3
created: 2026-11-02T14:40:50Z
---

Today apogee captures the mouse (View sets tea.MouseModeCellMotion) so the wheel scrolls and click positions/selects — which is exactly why the terminal's own selection AND its own pointer feedback never show. Button-less motion is ignored on purpose (mouse.go handleMouseMotion; settleFrame skips repainting on MouseMotionMsg, pinned by TestMouseMotionNeverRepaints). The frame already knows, for every cell, what it is over: the pointerPanes click/wheel chain, clickArm and the pane table (panes.go) classify a point into the editable prompt, the transcript, a toggleable tool block, a pane row (/settings, /sessions, picker, dropdown), the footer's mode marker, an approval/ask box. Raw escapes are already emitted on demand via tea.Raw (mousereassert.go reassertMouse, after tool results and on resize, because the renderer writes mouse modes only on a MouseMode diff).

This issue: change the terminal's MOUSE POINTER shape while it hovers — text over the editable prompt, pointer/hand over clickable rows and toggleable blocks, default over plain transcript — using the xterm-originated, kitty-specified OSC 22 escape (ESC ] 22 ; <name> ESC \). Distinct from the text caret: cursor-shape (DECSCUSR) is already shipped and stays as is.

## Design

Sketch agreed 2026-11-02 in the owner conversation; decisions below are the ones a plan should ratify, not final.

- OSC 22 is the only cross-terminal pointer mechanism and it is NOT universal: kitty (0.31+) and Ghostty, WezTerm, Foot speak the CSS/kitty names; xterm speaks X11 names; old terminals ignore the sequence (default arrow, nothing breaks); tmux/ssh may strip it. So the feature MUST be gated: probe support by querying the terminal (ESC ] 22 ; ?pointer,… ESC \ → answers read back the way probe's DSR-CPR machinery reads reports), switch View.MouseMode to AllMotion (?1003h) ONLY on a positive answer, and keep CellMotion otherwise — a terminal that cannot show shapes must not pay all-motion traffic either.
- Motion stays a side-effect, never a repaint: hover decisions run in the existing handleMouseMotion path and emit, on shape CHANGE only, one tea.Raw(pointerSeq). The TestMouseMotionNeverRepaints invariant holds; motion never triggers settle/repaint.
- Shape per region follows the existing pointerPanes order and rectangles: text over the prompt content rect (inputContentRect), pointer over rows the click chain would arm (clickArm.holds: toggleable blocks, pane rows, dropdown options, footer mode marker), default over the transcript body and empty rows. Emit only when the region class changes.
- Config switch, default OFF (e.g. ui.pointer-shapes: on|off under the existing ui block, registry pattern of ui.spinner) so a terminal that mishandles OSC 22 can disable it, and the feature ships off until real-use evidence turns it on. Purely presentational: no model-visible change, no Floor/Bypass involvement (ADR 0071/0076); if hover info were ever routed to the model, THAT would need a bench arm.
- Re-assert the current shape after tool results and on resize, beside reassertMouse (a tool child that resets terminal state would otherwise leave the pointer shape stuck). No reset needed on normal exit: kitty-family terminals keep per-screen shape stacks and the alternate-screen session leaves the main screen's stack untouched — verify per terminal in the plan.

Effort S–M, TUI-only: internal/tui (mouse.go, model.go View, panes.go), internal/config registry + defaults + docs (manual + defaults config.yaml), internal/probe if the capability query reuses its machinery, mouse_test.go / mousereassert-style tests.
