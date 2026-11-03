---
title: ADR 0106 consequences
---

- A new `ui.pointer-shapes` setting, bool, default `false`, in the config registry, the defaults
  `config.yaml` and the manual, editable in /settings.
- An in-session terminal capability query in `internal/tui`: the OSC 22 query and its DA1
  sentinel, the answer read from the ultraviolet events that reach `Update` raw
  (`uv.UnknownOscEvent`, `uv.PrimaryDeviceAttributesEvent`), and one verdict kept per session.
- A pointer-shape classifier beside the click chain, answering per point for the prompt, the
  footer, the transcript and each pane.
- View picks `tea.MouseModeAllMotion` while the gate is open and `tea.MouseModeCellMotion`
  otherwise. `handleMouseMotion` writes a shape on change, through `tea.Raw`, and still never
  repaints.
- The shape is re-asserted beside `reassertMouse` and reset on exit (both screen stacks), on the
  external-editor handoff and when the setting turns off.
- `layout.md` and the probe manual document the shapes and the gate.
- No engine change, no Event, no model-visible change, no bench arm.
- Plan: `2026-11-03 - 03 - pointer-shapes-on-hover-plan`; issue `pointer-shapes-on-hover`.
