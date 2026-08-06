# Handoff: debug the TUI ghosting on Windows

**Goal:** find and fix why the apogee TUI leaves ghost artifacts on Windows — stale
fragments of previous frames stay on screen, text gets eaten mid-line, and only a
terminal resize (which forces a full repaint) cleans it up. This session runs **on the
Windows machine itself** so the bug can be reproduced live.

**Context:** reported 2026-08-06 from a session on the Mac. A first fix attempt —
bumping the renderer stack — did **not** help, so the cheap explanations are ruled out
and live debugging is the next step. macOS is unaffected.

## The symptom (what two screenshots showed)

1. **Full-screen ghosting:** fragments of earlier frames (half-lines like
   "d to update it and sync the submodule", stray gray highlight blocks) stay painted,
   mostly on the right side of rows. New content overlaps old content on the same row.
2. **Tight repro on the status line:** while a turn runs, the line should read
   `<spinner> Thinking · 9s`. The default "snake" spinner is **two braille cells** wide.
   On Windows, exactly the first two letters are eaten (`.. inking · 9s`) and ghost
   spinner dots appear **one row above**. It ghosts within seconds of starting a turn.
3. **Resizing the terminal repaints everything correctly.** That is the confirming
   clue: the diff renderer's model of the screen drifts away from the terminal's real
   state; a resize forces a full redraw that resyncs them.

## What is already ruled out

- **Stale renderer.** The repo pinned an ultraviolet snapshot from 2026-05-25, which
  predates upstream July-2026 fixes with exactly matching descriptions ("reduce cursor
  drift from wide glyphs", "flush trailing empty cells in renderLine", "correct wide
  character rendering across terminals"). Commit `8cdbbf8` bumped
  `charm.land/bubbletea/v2` v2.0.7 → v2.0.8 and ultraviolet to the 2026-08-03 snapshot.
  A fresh Windows build from that commit **still ghosts**. Full test suite and
  windows/amd64 cross-build were green after the bump.
- **App-internal layout-vs-paint width drift.** The TUI already routes every width
  measurement through one authority that follows the painter's method
  (`internal/tui/width.go`, `widthAuthority`). Its coupling was re-verified against
  v2.0.8: bubbletea's mode-2027 switch is byte-identical to v2.0.7, and ultraviolet's
  buffer still defaults to `ansi.WcWidth`.

## Working diagnosis

Cursor drift between ultraviolet's screen model and the Windows terminal's real cursor,
most likely from a **character-width disagreement** in the negotiation between the
terminal and the painter:

- At startup bubbletea queries **mode 2027** (grapheme-cluster measurement). If the
  terminal answers "set/reset/permanently set", the painter switches from `WcWidth` to
  `GraphemeWidth` (`bubbletea/v2 tea.go` around line 786; mirrored by
  `widthAuthority.observe`, `internal/tui/width.go:73`).
- **Windows Terminal 1.22+** answers mode 2027 *and* has its own user setting
  `compatibility.textMeasurement` (`console` | `wcswidth` | `graphemes`, default
  `graphemes`). If what WT reports and how it actually measures disagree with what the
  painter adopted, every ambiguous-width glyph (braille spinner cells, `·`, `✦`, `…`)
  drifts the cursor by a column, and the diff renderer then paints partial updates at
  wrong positions — exactly the observed artifacts.

## Debug plan for this session

Work the cheap A/B tests first; each result narrows the cause a lot.

1. **Record the environment.** Windows Terminal version (Settings → About), the
   `compatibility.textMeasurement` value in WT's `settings.json` (absent = default),
   font, and Windows build. Also try apogee in a plain `cmd.exe` (conhost) window —
   does it ghost there too?
2. **Spinner A/B.** Set `ui.spinner = classic` in the apogee config (single braille
   cell; key defined in `cmd/apogee/registry.go:217`). If the status line stops eating
   letters, glyph width is confirmed as the trigger.
3. **Terminal measurement A/B.** In WT `settings.json` set
   `"compatibility": {"textMeasurement": "wcswidth"}`, fully restart WT, rerun. Then
   try `"graphemes"` explicitly. Note which mode ghosts and which does not.
4. **Instrument the width negotiation.** `widthAuthority.observe`
   (`internal/tui/width.go:73`) already receives the terminal's mode-2027 answer — log
   it (or surface it in the footer temporarily) and record which method the painter
   ended up on. Compare against WT's `textMeasurement`. A mismatch here is the bug's
   signature.
5. **Find the first drifting glyph.** The status line is the fastest repro. If needed,
   binary-search by stripping glyphs (`·` U+00B7, `✦` U+2726, `…` U+2026, braille
   U+28xx) from a minimal bubbletea program to isolate which one WT measures
   differently from the painter.
6. **Fix.** Depending on findings: a targeted workaround in apogee (e.g. avoid the
   offending glyph class on Windows, or pin the painter's method), and/or a minimal
   repro reported upstream to `charmbracelet/ultraviolet`. Prefer the upstream report
   plus a small local workaround over forking renderer behavior.

## Key files and references

- `internal/tui/width.go` — the width authority; the long header comment explains the
  layout-vs-paint width problem and the mode-2027 switch. Start here.
- `internal/tui/spinner.go` — snake spinner = two braille cells (`snakeGlyph`);
  `classic` = one cell. `ui.spinner` / `ui.spinner-color` in `cmd/apogee/registry.go`.
- `internal/tui/tui.go:854-891` — alt-screen claim and `tea.NewProgram` (no renderer
  options; the default ultraviolet cell renderer is in use).
- Commit `8cdbbf8` — the dependency bump that did not fix it.
- Build on Windows with `makeWin.bat build` (repo root; needs Go 1.26+).
- Upstream: <https://github.com/charmbracelet/ultraviolet/commits/main> (July 2026
  renderer fixes), <https://github.com/microsoft/terminal/discussions/17809> (WT 1.22
  grapheme clusters + `textMeasurement`),
  <https://mitchellh.com/writing/grapheme-clusters-in-terminals> (mode 2027 explainer).

## Suggested skills

- `coding-standards` — before modifying any Go code.
- `run` — to launch the TUI and observe the repro once a change is in place.
- `handoff` — write a follow-up handoff if the session ends before the fix lands.
