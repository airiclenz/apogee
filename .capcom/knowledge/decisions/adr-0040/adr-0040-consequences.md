---
title: ADR 0040 consequences
---

- **`internal/tui` no longer owns any color literal.** The package-level palette vars are gone;
  `newTheme(scheme.Scheme)` builds every style from the 24 roles (29 today — see
  `adr-0040-amend-0815`), and the spinner's blend stops are
  a `theme` field built per scheme instead of an init-time package var. A grep for `col[A-Z]` in
  `internal/tui` is the standing guard that no new literal creeps back in.
- **A runtime theme switch now exists**, which makes `paintcache.go`'s standing obligation live: the
  block paint cache is cleared on every scheme apply, and the apply ends in a relayout plus a full
  repaint. Any future cache keyed on painted output inherits the same obligation. *(Amended
  2026-09-16, plan `2026-09-16 - 00` item 20: the apply no longer lays out itself — the scheme name
  is an input of the frame key (`paintcache.go`), so Update's repaint tail (`settle`, `model.go`)
  performs the relayout and the full repaint; the cache clear stays the apply's own.)*
- **`internal/scheme` is plain data with no lipgloss and no TUI import**, so schemes resolve and
  validate in the composition root and the renderer is handed a struct — ADR 0031's wire-silence
  shape, and ADR 0011's "no YAML in the renderer" honoured by construction. Any Driver gets color
  schemes from the same door: `Options` carries the resolved `Scheme`, its name, its warnings, and
  three closures (`ListSchemes`, `ResolveScheme`, `ExportScheme`) that nil-degrade. *(Amended
  2026-10-29: since ADR 0054 the three closures are one `SchemeHost` interface — `List`, `Resolve`
  (with an `ok bool` for the per-member degrade), `Export` — carried as `Options.Schemes`
  (`internal/tui/tui.go`) and implemented by `schemeHost` in `cmd/apogee/wire_options.go`; a nil
  host is the unwired degrade.)*
- **The settings registry gains a `kindScheme` row** — `kindServer`'s twin *(amended 2026-10-29: the
  registry now lives in `internal/config/registry.go`, where the pair are `KindScheme` and
  `KindServer`)*: a dynamic closed
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
