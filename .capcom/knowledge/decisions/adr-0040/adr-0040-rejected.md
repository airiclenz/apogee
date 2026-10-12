---
title: ADR 0040 considered options
---

- **Install the built-in schemes into `~/.apogee/schemes/` at first run** (the `config.yaml`
  seed-if-missing precedent) — rejected: it makes every built-in a file the user can silently break
  or leave stale across upgrades, needs a repair story for a deleted or corrupted one, and buys
  nothing that decision 7's explicit export does not. `config.yaml` is seeded because the user must
  have one; nobody must have a copy of `dark.yaml`.
- **Download schemes from a gallery / URL** — rejected outright, in any form. Apogee's colors are
  not worth a network path, an update mechanism, or a trust question, and this is the one line the
  grill drew before any other.
- **A base-palette indirection: named swatches that roles alias** — rejected per decision 3. It
  doubles the vocabulary a scheme author has to learn, makes "what color is an error" a two-hop
  lookup, and its one real benefit (change a tone once, see it everywhere) is a property of writing
  the same hex twice in a file you are already editing.
- **Fewer, coarser roles** (a foreground / background / accent triple) — rejected: apogee's colors
  already carry meaning the user can see — four autonomy modes, add versus delete, skill versus file
  reference — and collapsing them would make a scheme unable to express the distinctions ADR 0027
  and the mode ladder depend on.
- **Painting the full screen background from `surface`** — rejected per decision 9: apogee does not
  own the terminal's background, a full-bleed repaint fights the user's own transparency and
  wallpaper, and it would make every un-themed region (a subprocess's output, a pager) read as a
  hole in the screen.
- **Auto-detecting a light or dark terminal** (`lipgloss.AdaptiveColor`, OSC 11 queries) — rejected
  for this record: detection is unreliable across terminals and multiplexers, and its failure mode
  is an unreadable screen the user cannot override. An explicit `ui.color-scheme` cannot be wrong
  about what the user wants. Nothing here forecloses adding detection later as a *default* for the
  key.
- **A strict parse that rejects a defective file** — rejected per decision 8. The failure lands at
  startup, on the surface the user needs in order to fix it, and "your colors are wrong" is never
  worth "apogee will not start".
- **Warning on a missing key** — rejected: partial files are the documented, intended usage
  (decision 3), so a two-key scheme would emit twenty-two warnings for doing exactly the right
  thing.
- **Overwriting on export, or exporting user files too** — rejected: an export that overwrites can
  destroy work someone has been doing all evening, and a user file is already the editable copy
  export exists to produce.
- **Theming glyphs, markers, or layout** — out of scope by construction: this record is about color.
  The spinner *styles* stay `ui.spinner`'s business and the block markers stay `layout.md`'s.
- **Hot-reloading a scheme file edited on disk** — rejected here, and left to the config-watch work
  (`docs/plans/2026-08-06 - 04 - editor-key-and-config-watch-plan.md`). Re-selecting a scheme
  re-reads its file, which already closes the edit loop with an unambiguous end-of-edit signal —
  the same reasoning ADR 0037 used to reject a file watcher.
- **Keeping the palette vars and giving them a runtime setter** — rejected: package-level mutable
  color vars are shared state under a value-copied Model (ADR 0011), and the init-time
  `spinnerStops` var proved the point by being unable to follow a switch at all. The scheme flows in
  through `newTheme` and the stops became per-theme state.
- **Making the mode/error/surface colors reachable only through styles** — not possible: the prompt
  textarea is Bubble Tea's widget and takes raw colors, not styles, so the `theme` struct carries a
  few raw color fields and threads `surface` into the editor constructors. That is why closing the
  "palette leaks" was part of the work rather than a tidy-up after it.
