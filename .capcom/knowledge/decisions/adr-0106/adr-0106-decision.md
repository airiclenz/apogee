---
title: ADR 0106 decision
---

**The TUI sets the pointer shape on hover through OSC 22, only when `ui.pointer-shapes` is on
and the terminal has confirmed it supports the three shapes used. Otherwise nothing changes on
the wire.**

1. **Three CSS shape names.** apogee writes only `default`, `text` and `pointer`. `text` (an
   I-beam) shows over the editable prompt, over a settings text field and over transcript lines
   that are not toggle targets, since those are drag-selectable. `pointer` shows over every row
   or block a click would act on: pane rows, dropdown options, toggleable tool blocks, the
   footer's mode marker. `default` shows everywhere else. The classification follows the
   existing click and wheel chain, so a shape never promises a click the chain would not take.
2. **The gate: the setting on, and a full answer.** The feature runs only when `ui.pointer-shapes`
   is on and the terminal answers the query `OSC 22 ; ?default,text,pointer ST` with `1,1,1`.
   The query is followed by a DA1 request (`CSI c`) as a sentinel. Terminals answer DA1 in
   order, so a DA1 reply that arrives before an OSC 22 answer means unsupported, as does any
   answer other than `1,1,1`. There is no timer. All-motion tracking (`?1003h`,
   `tea.MouseModeAllMotion`) is on only while the gate is open; otherwise View keeps
   `tea.MouseModeCellMotion`, so a terminal that cannot show shapes pays no motion traffic.
3. **The query is sent only while the setting is on, once per session.** With the setting off,
   apogee sends no query at all. Turning it on in /settings sends the query then. One answer per
   session is kept: a later toggle reuses it and sends no second query.
4. **Raw writes, no tmux passthrough.** The query and every shape are written raw, never wrapped
   in `ansi.TmuxPassthrough`. Under tmux the query goes unanswered, the DA1 sentinel answers
   first, and the feature stays off.
5. **Hover is a side effect, never a repaint.** Button-less motion still never repaints the frame
   (`TestMouseMotionNeverRepaints` holds). A motion event only reclassifies the point, and a
   shape is written only when the class changes from the last one written.
6. **Re-assert beside mouse tracking, reset on every release.** The current shape is written again
   wherever `reassertMouse` writes the mouse modes again (after tool results, on resize). The
   reset (`OSC 22 ; ST`, the empty name) is written on every terminal release: on exit, for both
   screen stacks, and on the `settingsExternalEdit` handoff to an external editor. It is also
   written when the setting turns off. A reset is written only after apogee has written a shape.
7. **Presentational only.** Hover produces no Event, and nothing about it reaches the model. The
   engine is untouched, so ADR 0031's Driver-sufficient engine, ADR 0071's Floor guards and
   ADR 0076's Reaction core are not engaged. Hover information routed to a model would be a
   model-facing change and would need its own record and a bench arm.

**Out of scope:** X11 cursor names, shapes beyond `default|text|pointer`, tmux passthrough, any
model-visible hover information, and the text caret (`cursor-shape`, DECSCUSR).
