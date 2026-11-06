---
title: Slow idle-state waits show no busy indicator
status: done
type: feature
priority: 2
plan: 2026-11-06 - 01 - busy-indicator-plan
created: 2026-11-06T20:34:31Z
closed: 2026-11-06T22:08:11Z
reason: "Resolved by plan 2026-11-06 - 01 - busy-indicator-plan (archived at 22114dee): status-line busy indicator for idle waits, animated pop-up loading rows, docs and glossary Note."
---

The status-line spinner (`internal/tui/spinner.go`) runs only while a Turn runs (`stateRunning`); its tick chain is keyed on `worker.gen`. Every other wait leaves the status line blank, so on a slow box apogee looks stuck:

- launcher verbs — `/model <profile>` load, `/unload-model`, `/stop-server` (minutes; footer says `loading <profile>…`, status line blank)
- the first beat after start-up, a `/server` switch or a load (`connecting…` in the footer only)
- a `/sessions` record load (nothing shown)
- a footer git action (one static transcript note)
- a bare `/rename` naming the session through a slow model (one static note)
- the pop-up `loading…` rows (`popupLoadingRow`: `/sessions` browser, workflow item detail) are static text

Wanted: one TUI-wide busy indicator — the user's `ui.spinner` glyph, a phrase and an elapsed clock in the status line for any of these waits, and the spinner glyph on the pop-up loading rows. Pure presentation in the TUI Driver (no engine change). The MCP reconnect freeze is a separate issue (`mcp-reconnect-freezes-tui`): it blocks the Update loop, so nothing can animate.

## Why

A slow machine makes apogee slower, never broken — but a blank status line during a minutes-long model load reads as broken.

## Acceptance

- Each wait listed above shows the spinner glyph, its phrase and a clock in the status line while it is in flight, and nothing once it lands
- The /sessions and workflow-item loading rows animate with the spinner glyph
- Live stars still hold still outside a running Turn; the Turn spinner behaves as before
- layout.md, the manual and a glossary Note describe the busy indicator
