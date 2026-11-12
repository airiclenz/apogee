---
title: Session log notes keep stderr hard line breaks instead of wrapping to width
status: open
type: bug
priority: 3
plan: 2026-11-12 - 01 - session-log-wrap-and-tool-grouping-plan.md
created: 2026-11-12T18:44:29Z
---

An expanded **Session log** note keeps the hard line breaks of its stderr form, so it reads oddly in the transcript. Seen on macOS (seatbelt) in Auto mode, 2026-11-12:

```
✦ Session log (2 notes) ▼
  · apogee: auto mode fences terminal commands, but seatbelt cannot fence:
      connect(2) AF_UNIX — a same-user socket service (tmux; systemd-run, docker if not masked) can run commands
    outside the fence
      mach-lookup — a same-user launchd service (launchctl submit, osascript) can run commands outside the fence
  · glm-5.3-flash always reasons — compaction and titles spend part of their output cap on it
```

## Cause

- `probe.ResidualNotice` (`internal/probe/confinement.go`) builds the residual notice for stderr: a header line, then one `"  <token> — <consequence>"` line per residual, joined with `\n`. `cmd/apogee/wire_boot.go` passes that same text to `w.announce(tui.Notice{Text: notice})`.
- `renderSessionLog` (`internal/tui/render.go`) keeps a note's embedded newlines ("A note of several lines keeps them, hung under its first line's text"). So the hard breaks and the two-space indents stay; then `hangingWrap` wraps the long residual line, and its soft-wrap continuation hangs at the bullet's text column, left of the hard-indented line it continues ("outside the fence" above).
- The `apogee:` prefix is a stderr convention and is noise inside the log.

## Wanted

Each note in the Session log is one continuous paragraph of text. Only `hangingWrap` breaks it, at the transcript's current width, and it re-wraps when the window is resized. No hard breaks or hand-made indents come from the notice text.

Design points:

- Where the transcript form is made: a separate transcript wording from `probe` (keep stderr byte-identical — headless, daemon and the pre-alt-screen stderr path carry it verbatim, `announce`), or a normaliser in the TUI that folds a multi-line notice into one paragraph. Prefer giving the transcript its own wording over guessing at whitespace.
- A residual notice with several residuals: one note per residual (`seatbelt cannot fence connect(2) AF_UNIX — …`), or one note with the residuals joined in one sentence. One note per residual reads better and keeps each note one line at wide widths.
- Check every other notice that reaches the Session log for embedded `\n` (context files, degraded/residual confinement, mandatory reasoning, rebind notes) and give each the same one-paragraph form.
- Update the layout spec (`layout.md` "The start-up box", `docs/layout/tool-layout.md`) where it says a note of several lines keeps them.

## Acceptance

- The expanded Session log on a seatbelt host in Auto shows each note as one paragraph wrapped only at the window width; no line starts with a hard indent, and no wrapped continuation sits left of the text it continues.
- Resizing the window re-wraps the notes.
- The stderr text of the residual notice (headless, daemon, start-up) is unchanged.
- A driven TUI test pins a residual note at two widths (wide: no wrap; narrow: soft wrap only, hung at the note's text column).
