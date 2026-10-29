---
title: ADR 0041 consequences
---

- **ADR 0037 keeps its title and loses two mechanisms.** A "Superseded in part by 0041" blockquote
  sits under its title, in the shape 0037 itself used over ADR 0035; its `Status` stays `accepted`,
  because everything but binding B and the exit trigger is still the governing record for this
  surface. Its Considered-options rejection of a file watcher and its consequence "An edit made
  outside apogee still does not reload" are the two lines this record reverses outright.
- **`cmd/apogee` grows a watcher, and nothing else grows.** A dependency-free polling type over one
  path, started for the life of the TUI in the composition root and stopped at teardown, reporting
  only "the file changed". Parsing, projection, the diff and the per-key apply stay where they
  already are, reusing the existing baseline diff and dispatcher unchanged — ADR 0011 holds (no YAML
  reaches the renderer) and ADR 0031 holds (the engine is handed resolved values, so any Driver that
  starts the watcher gets the same live reconfiguration). *(Amended 2026-10-29: the polling type
  moved out of `cmd/apogee` into `internal/filewatch` on 2026-08-22 (`7410670d`) so the daemon's
  `schedules.yaml` reload shares it (ADR 0043); one `Watcher` is still one path, and the composition
  root now holds up to three — `config.yaml`, the Project config (`adr-0041-amend-1010`) and the
  Project root's adoption record (ADR 0096 §4) — fanned into one wait, `cmd/apogee/wire_live.go`.
  The package still reads no YAML and applies nothing.)*
- **The settings registry gains one editable string key with no validator, and a dispatcher case that
  deliberately moves nothing.** `editor` is read off a fresh projection of the file at the moment an
  external edit starts, so the write itself is the whole apply and there is no seam to drive. The case
  is named anyway because the dispatcher's default arm is a refusal, and refusing a key already in
  force would report "editor cannot be applied to the running session" about a change that had taken
  effect. It is therefore not dead code: deleting it as such reinstates that false report, and
  `TestApplySettingAcceptsTheEditorKey` is the guard that says so. The registry's own bijection guard
  forces the row, the file's commented example documents it, and the row is blank when unset.
- **A visible behaviour change on upgrade.** A user with no `$VISUAL`/`$EDITOR` who pressed ⏎ on
  `servers` landed in `vi`; they now land in whatever their desktop opens `.yaml` with, and the pane
  stays on screen while they edit. README, `layout.md` and `docs/layout/settings-screen-layout.md`
  carry the wording; the spec owns the row's note.
- **`config.yaml` becomes a live surface.** Anything that writes it changes the running session — a
  script, a dotfile manager, a second apogee instance persisting a pane edit. That is the intended
  reading of ADR 0037's title, and it is why the last-good rule and the per-key refusal are
  load-bearing rather than defensive. No locking is introduced: the file stays last-writer-wins, as
  it has always been.
- **Scheme files still do not hot-reload** (ADR 0040), even though `ui.color-scheme` itself now
  applies when it is changed in the file: the watched thing is the config document, not everything
  the config document points at.
- **`CONTEXT.md` gains the watcher** in the Settings-surface neighbourhood, and its "$EDITOR
  round-trip" phrasing narrows to the foreground path.
- This is user-visible — a new config key, a changed default editor, external edits applying live —
  and **warrants a minor bump** when the next version is cut. That call is the owner's, and no item
  of the implementing plan touches a version identifier.
