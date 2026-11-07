---
title: "Busy indicator: the spinner, word and clock an idle session shows while it waits"
refs: [internal/tui/busy.go, layout.md]
---

**Busy indicator**:
The **spinner, word and clock an idle session shows while it waits on something slow**, so a slow
box never looks stuck. A Turn narrates itself on the running row; outside one the session can still
be waiting — a launcher verb in flight, a footer git action, a `/sessions` record load, a bare
`/rename`'s naming call, a wired heartbeat that has not bound a model yet, an MCP server's dial
running off the loop — and while it is idle the
status line carries that wait in the running row's shape, `⠶⠶ loading qwen3… · 01:04s`. The **word
is the one apogee already shows for the wait** (`loading <profile>…`, `git <verb>…`, `loading
session…`, `naming this session…`, `connecting…`, `reconnecting MCP…`), one wait at a time, the most specific first; a
changed word is a new wait whose clock starts again. A pop-up still reading what it lists — the
`/sessions` browser, the `/workflows` item view — is a wait too, but **its own `loading…` row turns
the glyph** and the status line stays blank. The wait is **derived at call time from the latches that
already record it**, never a flag of its own (`internal/tui/busy.go`, `busyWait`). It draws whatever
`ui.spinner`, `ui.spinner-color` and `ui.glyphs` select — no style or setting of its own — and its tick
**never flips a live star**: stars blink only while a Turn runs. Pure TUI presentation: the engine and
the other Drivers know nothing of it (`layout.md`, "The status line's spinner").
_Avoid_: "loading screen" or "progress bar" (it measures nothing — the clock says how long, not how
far), "the spinner" alone for the idle form (the running row's spinner is the Turn's; the busy
indicator is the same glyph narrating a wait).
