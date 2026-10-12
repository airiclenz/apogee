---
title: ADR 0041 context
---

ADR 0037 made the settings surface
live: a committed edit validates, persists and applies to the running session at ⏎, and no row ever
says "(next launch)" about its own effect again. For the six genuinely nested keys — `servers`,
`system-prompt-models`, `mcp-servers`, `mechanisms`, `validated-sets`, `model-profile` — its decision
6 routed the edit out to the user's own editor and made **returning from that editor** the
end-of-edit signal: apogee launches `$VISUAL`, then `$EDITOR`, then `vi` (`notepad` on Windows) at
the key's line, waits for the child to exit, re-reads the file, diffs it against the baseline it took
at launch, and applies each changed key. Its Considered options rejected a file watcher by name,
because "an editor's save-in-progress states, a half-written file and an edit the user is not
finished making would all become apply events", and its Consequences said so plainly: *"An edit made
outside apogee still does not reload."*

That contract holds for exactly one kind of program: a terminal editor that owns the terminal until
the human quits it. It is silently, invisibly wrong for every other kind.

A user whose editor is a GUI application — VS Code, Zed, BBEdit, Sublime — or who has no `$EDITOR` at
all and would like the desktop's own `.yaml` application, is not running an editor as a child
process. They are running a **launcher stub**: `open` on darwin, `xdg-open` on linux, `code` without
`-w`. A stub hands the path to an already-running application and returns within milliseconds —
before the editor is on screen, let alone before anybody has typed. The exit-triggered diff therefore
fires immediately, reads bytes nobody has touched, finds nothing changed, reports nothing and applies
nothing. Then the user edits the file, saves it, and apogee never notices. This is worse than a
refusal: the round trip *looks* like it worked. The pane came back, no error landed on the row, and
the config the user just wrote sits on disk unread until the next launch — the deferral ADR 0037
exists to abolish, reintroduced through the one door 0037 opened to route around it.

The prior art names both halves of the fix. llama-launcher's `doEditConfig`
(`internal/launcher/menu.go:448`) is two lines: clear the screen, `exec.Command("open", path).Run()`.
It takes no interest in when the editor exits, and it does not have to, because its menu loop calls
`Reload` (`internal/launcher/config.go:348`) on every redraw and every status probe — an
unconditional re-read whose contract is *"if the file is unreadable or invalid, the receiver is left
unchanged"*. A non-blocking opener is usable precisely when something else is watching the file, and
a poll that re-reads a document a human is mid-save on must keep the last good one. Two things there
are not to be copied: the hardcoded `open`, which is why that implementation is macOS-only, and
having nowhere to tell the user that the file stopped parsing.

So the two objections ADR 0037 raised against a watcher get answered rather than deferred. A
half-written file is a real hazard and is handled mechanically — a settle delay, and a parse failure
that keeps the previous projection. "An edit the user is not finished making" was never a coherent
objection: a save is the user's statement of intent at the moment they made it, the apply is per-key
and idempotent, and the same reasoning would forbid the pane from applying a bool the user might
toggle back. What remains is the honest reading of ADR 0037's own title — a settings edit made in the
user's own editor is a settings edit, and it applies to the running session.

A 2026-08-06 session with the owner settled the shape, question by question. This record is its
ratification; it is implemented by
`docs/plans/2026-08-06 - 04 - editor-key-and-config-watch-plan.md`, and its visual half stays with
`docs/layout/settings-screen-layout.md`.

The constraints it lands inside are the ones 0037 named, unchanged:
ADR 0011 — the renderer is thin, its
Model is value-copied, and no YAML enters it, so the watcher and every re-read live in the
composition root;
ADR 0031 —
the engine is wire-silent and is handed resolved values, so what the watcher produces is the same
per-key apply any Driver already drives;
ADR 0035 — the persistence
contract, which this record leaves entirely alone;
ADR 0012 — the
confinement interlock stays single-homed in `/confine`, so those keys are still skipped by a file
reload;
and ADR 0036
— `server:` is the recorded startup binding, so a reload still never reports it.
