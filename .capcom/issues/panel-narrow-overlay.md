---
title: Side panel opens as an overlay on a window too narrow to dock it
status: open
type: feature
priority: 2
deps: [panel-keyboard-navigation]
created: 2026-11-08T21:33:39Z
---

Below its gate (137 columns in `auto`, 117 in `on`) the panel is not drawn and bare `/panel`
only says why. ADR 0111 decision 7: `→` in navigation mode, or bare `/panel`, opens the
panel as an overlay over the transcript's right edge, at the panel's width clamped to the
window, in navigation mode with the cursor in the panel region. `←`, `esc` or a click outside
closes it; clicks inside act as on the docked panel. The overlay never re-wraps the
transcript and cannot be resized. Under `off`, `→` does nothing.

Design: [ADR 0111](.capcom/knowledge/decisions/adr-0111.md).

## Acceptance

- At 100 columns with `ui.side-panel.show: auto`, `alt+↑` then `→` draws the panel over the transcript's right edge; the transcript's wrapped lines are byte-identical before and after.
- Bare `/panel` at that width opens the overlay with the cursor on its first stop, and still prints its report.
- `←`, `esc` and a click outside each close it.
- With `show: off`, `→` leaves the screen unchanged.
