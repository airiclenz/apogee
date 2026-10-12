---
title: ADR 0040 decision
---

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
