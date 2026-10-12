---
title: "Color scheme: the palette apogee draws with, as a file of semantic roles"
---

**Color scheme**:
The **palette apogee draws with, as a file of semantic roles** — `ui.color-scheme` names it, and
the name resolves to `~/.apogee/schemes/<name>.yaml` first and an **embedded built-in** second, so
a user file **shadows** a shipped scheme of the same name rather than replacing it (ADR 0040). A
scheme file is YAML with **one key per role — 29 of them**, named for meaning rather than place
(`error`, `code`, `tool-header`, `file-ref`, `skill`, `muted` / `muted-bright`, the four `mode-*`,
the four `spinner-*`, …) — and **every key is optional**: a missing one inherits the built-in
**`dark`** default, which is the palette apogee has always drawn with apart from three roles
retuned for legibility while the scheme system was being built (`code`, `tool-header` and
`tool-marker`), and remains the default scheme. Two schemes ship, `dark` and a `light` one *for
light terminals*; both are compiled into
the binary with `go:embed` and are **never installed on disk and never downloaded** —
`/color-scheme export <name>` is the only way one reaches the user's disk, verbatim comments and
all, and it refuses to overwrite. Loading is **forgiving**: a bad hex, an unknown key, an
unreadable file or an unknown name each cost a default plus a **warning as a transcript ephemeral
note**, never the run. The scheme in force is switched live — a picker row on the
Settings surface and `/color-scheme <name>`, both rebuilding the `theme`
and clearing the block paint cache — and schemes recolor **only what apogee already colors**: no
full-screen background is painted and no glyph, marker or layout is themed.
_Avoid_: "theme" (that is the internal struct of built lipgloss styles the scheme is compiled
into), "dark mode" / "light mode" (nothing is auto-detected — the key is the user's declaration
about their own terminal), "color palette" for the file (a palette is a bag of colors; a scheme
assigns one to each named role).
