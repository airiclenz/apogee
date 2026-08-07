---
Status: accepted
---

# Color schemes are embedded role palettes with user-file shadowing

## Context

Apogee's palette was twenty-odd hex literals in package-level vars at the top of
`internal/tui/theme.go` — `colWhite`, `colFaint`, `colDiffAdd`, `colModeAuto`, `colSpinner1..4` —
read directly by `newTheme` and, in a handful of places, by call sites that had no style to reach
for (the prompt textarea's four background slots, the gauge fill's track, the popup's interior, the
mode word's color). Every tone in the product was therefore a compile-time constant, and the whole
palette was tuned for one terminal: a dark one. On a light terminal apogee paints `#8a8a8a` detail
text and `#ffffff` user text onto whatever the terminal's own background is, and both disappear.

The owner's ask was a **user-configurable color scheme**, and a 2026-08-07 grill settled its shape
question by question. This record is that grill's ratification; it is implemented by
`docs/plans/2026-08-07 - 02 - color-schemes-plan.md`.

Three existing decisions bound the design before it started.
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) — the renderer is thin, no
YAML enters it, and its Model is copied by value on every `Update`, so whatever a scheme becomes
inside the TUI has to be copy-safe.
[ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md) — a settings edit applies to
the *running* session at ⏎, so "restart to see your colors" was never an available answer; and
[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md)'s persistence contract
(one key per deliberate edit, spliced and verified) is how the choice gets written down.
[ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md) — the prompt's `/skill` and `@file`
tokens are told apart **by color**, which makes two specific roles load-bearing rather than
decorative in any scheme that ships.

And one implementation fact bounded it too: `internal/tui/paintcache.go` caches painted blocks and
its own comment named the obligation — the cache has to be cleared when the theme changes — while
noting that no runtime theme change existed. Making one exist is most of the work.

## Decision

**1. Built-in schemes are embedded in the binary.** `go:embed schemes/*.yaml` in `internal/scheme`.
They are never written to disk, there is no boot-time presence check to install or repair them, and
nothing in this feature ever touches a network — not to fetch a scheme, not to list one, ever. A
built-in reaches the user's disk only when the user asks for a copy (decision 7).

**2. Scheme files are YAML**, parsed with `gopkg.in/yaml.v3` — the same decoder and the same file
shape as `config.yaml`, because a user who edits one already knows how to edit the other. Shipped
files carry a comment on every key saying what it colors, and those comments are part of the
artifact: an exported scheme is documentation the user edits in place.

**3. The schema is one key per semantic color role — 24 roles — and every key is optional.** Roles
are named for what they mean, not for where they are drawn (`error`, `code`, `mode-auto`,
`file-ref`, `muted`, `muted-bright`, `spinner-1`…`spinner-4`), and the file groups them in commented
sections: base tones, semantic, autonomy modes, prompt tokens, chrome accents, misc, spinner. A
missing key silently inherits the built-in `dark` value for that role — partial files are the
intended way to write one, so absence is not a defect and earns no warning. The `Scheme` struct's
yaml tags are the single definition of the vocabulary; the role list is derived from them by
reflection rather than restated.

There is deliberately **no base-palette indirection** — no `colors:` block of named swatches that
roles then point at. A role *is* a color.

**4. Two schemes ship: `dark` and `light`.** `dark` is exactly today's palette, value for value, and
remains the default, so this whole record is a no-op for a user who does nothing. `light` is new.

**5. The user-facing name is "color scheme", everywhere.** Config key `ui.color-scheme` (file-only,
in the `ui:` block, like its neighbours); user folder `~/.apogee/schemes/` holding `*.yaml`; command
`/color-scheme`. Not "theme" — `theme` is already the internal struct of built lipgloss styles, and
the two would have been confused in every conversation about either.

**6. A user file shadows a built-in of the same name.** Resolution order is `~/.apogee/schemes/<name>.yaml`
first, then the embedded set. A user who wants to adjust `dark` writes `dark.yaml` and keeps the
name they already type; the built-in is overridden, never replaced, and deleting the file restores
it. `Discover` lists built-ins and user files merged, sorted and deduplicated — a shadowed name
appears once — and opens no file, so a picker can list schemes on every draw and a defective file
costs nothing until someone selects it.

**7. `/color-scheme export <name>` hands the user an editable copy; switching happens elsewhere.**
Export writes the embedded bytes **verbatim** — comments and all — to `~/.apogee/schemes/<name>.yaml`,
creating the folder at `0o700` and the file at `0o600`, and **refuses to overwrite an existing
file** (create-exclusive, so the refusal cannot go stale between the check and the write). Only
built-ins are exportable: the point is to obtain a starting copy, and a user file already is one.

Switching is two doors onto one seam: a picker row on the settings screen — built-ins plus
discovered user files, current one marked — applying live on ADR 0037's `settingsApplyLocal` path,
and `/color-scheme <name>` in the transcript, which applies through that same path *and* persists
through the same ADR 0035 write the pane uses. Bare `/color-scheme` lists what this session can
switch to.

**8. Loading is forgiving, and says what it forgave.** A bad hex value or an unknown key costs that
one key, which keeps its default, plus a warning naming the file and the key; a file that will not
open or will not parse at all, and a scheme name that resolves to nothing, cost the default scheme
plus a warning. No path returns an error, and every path returns a usable palette. A defective
scheme file never crashes apogee, never blocks startup, and never leaves the screen unstyled. For
the same reason `ui.color-scheme` validates only as a name — non-empty, no path separators — and
existence is deliberately **not** checked at config-load time, because a scheme that has been
deleted is a warning, not a broken config file.

**9. Schemes recolor only what apogee already colors.** No full-screen background painting: the
terminal's background stays the terminal's, and a scheme cannot repaint the parts of the screen
apogee leaves plain. `light` is documented as "for light terminals" and is the user's declaration
about their terminal, not a claim apogee makes about it. In every shipped scheme, `skill` and
`file-ref` must stay visually distinguishable (ADR 0027) — a cross-scheme test iterates every
built-in and enforces it, alongside the four `mode-*` values being pairwise distinct and `muted` /
`muted-bright` holding their contrast step apart.

**10. The `light` scheme is GitHub-light equivalents**, role by role: same hue meaning, GitHub's own
values where a role has an equivalent (`#cf222e` for error and diff-del, `#1a7f37` for diff-add,
`#0969da` for allow-edits, `#1f2328` for user text), and hue-preserving darkened values where it
does not (the mode-plan cyan, the file-ref green, the spinner stops), because a tone tuned for
contrast against black is invisible against white.

**11. Warnings surface as a chat-transcript note.** `addEphemeralNote` styling — a dim one-liner
that is not persisted into the session record — at boot for the configured scheme and after a live
switch for the chosen one; while the settings pane is open, the row additionally carries
`applied with N warnings` in its own note slot. A color problem is reported where the user is
looking, at the moment it happened, and does not become permanent scrollback.

## Considered options

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

## Consequences

- **`internal/tui` no longer owns any color literal.** The package-level palette vars are gone;
  `newTheme(scheme.Scheme)` builds every style from the 24 roles, and the spinner's blend stops are
  a `theme` field built per scheme instead of an init-time package var. A grep for `col[A-Z]` in
  `internal/tui` is the standing guard that no new literal creeps back in.
- **A runtime theme switch now exists**, which makes `paintcache.go`'s standing obligation live: the
  block paint cache is cleared on every scheme apply, and the apply ends in a relayout plus a full
  repaint. Any future cache keyed on painted output inherits the same obligation.
- **`internal/scheme` is plain data with no lipgloss and no TUI import**, so schemes resolve and
  validate in the composition root and the renderer is handed a struct — ADR 0031's wire-silence
  shape, and ADR 0011's "no YAML in the renderer" honoured by construction. Any Driver gets color
  schemes from the same door: `Options` carries the resolved `Scheme`, its name, its warnings, and
  three closures (`ListSchemes`, `ResolveScheme`, `ExportScheme`) that nil-degrade.
- **The settings registry gains a `kindScheme` row** — `kindServer`'s twin: a dynamic closed
  vocabulary that writes like a string and renders as a picker. The registry's key bijection forces
  the row, and the pane's vocabulary for it is supplied at runtime rather than pinned in the
  registry.
- **`~/.apogee` gains a `schemes/` folder**, created on first export and otherwise absent. Its
  absence is normal and never an error.
- **ADR 0027's skill/file-ref distinction is now enforced by test**, across every shipped scheme,
  rather than resting on two hex values a person picked. A future built-in cannot ship without it.
- **The dark scheme is pixel-identical to the palette that preceded this record**, and the existing
  render tests are the proof: the same values flow through a new pipe.
- **A user scheme is now a supported artifact**, which means the role vocabulary is a compatibility
  surface: renaming a role breaks every user file that names it, and adding one is safe (absent keys
  inherit). New roles are additive; a rename needs an amendment to this record.
- **This is a user-visible feature** — a new config key, a new command, a new folder under
  `~/.apogee` — and **warrants a minor bump** when the next version is cut. That call is the
  owner's, and no item of the implementing plan touches a version identifier.
