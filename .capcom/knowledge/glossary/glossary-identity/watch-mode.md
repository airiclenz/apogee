---
title: "Watch mode: the TUI's read-only live view of a session another apogee runs"
---

**Watch mode**:
The TUI's read-only view of a **Live session** another apogee is running, opened from `/sessions`
(⏎ on a `● live` row) or `--resume <id>`. It paints the record's first `entries` transcript
entries, then follows the session's **Event journal**: streamed text, tool cards, read-only **Run
views**, and thinking in the **Thinking pane** only. It is not a **Driver**: it drives no loop,
holds an inert Engine, never loads, holds, saves or deletes a record and never takes a lock.
Everything it reads is untrusted and passes the TUI's escape stripping. The prompt box reads
`watching <title> · read-only · esc back`; esc returns to the session the watcher left, or to a
fresh one after `--resume`. When the owner ends, a banner says so and the view stays read-only; in
the watcher's own home ⏎ then resumes through the normal door, and a **Watched home**'s session
is never resumable. See ADR 0103.
_Avoid_: "attach" (nothing is shared with the owner and nothing flows back), "spectate",
"mirror".
