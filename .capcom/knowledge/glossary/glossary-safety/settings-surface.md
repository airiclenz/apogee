---
title: "Settings surface (`/settings`): the full-height pane over the key registry"
---

**Settings surface** (`/settings`):
The **full-height pane over the key registry** — the in-app view of `~/.apogee/config.yaml`. The
**key registry** is one declarative table describing every config key (path, kind, default,
env-var and flag names, key class, confinement interlock, editability, masking, validation hook,
one-line description); both the pane and Resolution's multi-source
precedence read their metadata from it, and a reflection **bijection guard** against `fileConfig`'s yaml tags makes a schema key
without a registry row a test failure — the screen cannot drift from the schema. The pane claims
the **entire transcript row budget** while the frame floor (status line, input box, footer) stays
drawn — a new pane class, `layout.md`'s first surface allowed to take all of it — lists every key
with its resolved value and **Source mark** under white section headings and a fixed two-line
description header, and **persists one key per committed edit**: spliced
into the file comment-preserving, re-parsed and verified against the original apart from the target
path, written atomically; an inserted key lands below its commented example, and **reset deletes
the key's active line** rather than writing today's default.
Every committed edit is also **applied to the running session** (ADR 0037, superseding ADR 0035's
mode-only live apply): the ⏎ that persists routes the key to whatever puts it into effect — an
anytime-safe engine setter, an idle-only validate-then-commit rebind, or the composition root — so
**no key waits for a restart** and no row says "(next launch)". What a row keeps instead is the
**session edit journal**'s ` *` marker: *this surface changed this key this session*, cleared only
by relaunch. The one deferral wording left is a **boundary note** for a key that lands at a
boundary the session crosses anyway ("· applies at next clear", the `context-files:` pair, whose
KV-prefix stability is deliberate); a pane edit **outranks an env/flag override** for the running
session (the row notes that the override wins again at the next start, startup precedence
unchanged), and a persist whose apply then failed says so ("saved — live apply failed: …"). The
ten renderer-owned `ui.*` keys travel as one value, **`domain.UIPrefs`** — the `ui:` block's
schema, homed in `internal/domain` because config and the renderer both name it and import neither
the other (ADR 0043, 2026-09-20 amendment): resolved by config, carried whole on the renderer's
Options in the file's own positive polarity, and edited live through its one parser, `UIPrefs.Set`,
which the registry rows themselves call.
Editing is **hybrid**: simple keys are edited in the pane — a bool toggles, a 3-plus-option key
opens a selection popup, a string or an int opens a real single-line field on its row (cursor keys
and mouse), the inline system prompt a multi-line field (⏎ inserts a newline, ctrl+s commits) —
while every key the pane does not write, bar the confinement pair below, takes an **external edit** —
the six nested structures (`servers`, `system-prompt-models`, `system-prompt-layers`, `mcp-servers`,
`reactions`, `model-profiles`) and the file-only scalars beside them (`sub-agents-server`,
`tools.enabled`, the three `workflow-*` keys, `present.command-on-model-documents`): ⏎ opens the
human's own editor at that key's line, and every changed key is applied through those same two homes (a changed `mcp-servers:`
**reconnects**, validate-then-commit: the new set is dialled first and the old sessions keep serving
on failure; startup connect stays fatal). Which editor is the **editor ladder** — the `editor`
config key, then `$VISUAL`, then `$EDITOR`, then the platform's **OS opener** (`open` / `xdg-open` /
`cmd /c start`) — an explicit setting outranking an ambient one, so a command set for apogee is
never quietly beaten by a variable the row cannot show. How it is started splits on that answer: a
**terminal editor** (the nine names that draw over the tty) takes a **foreground** launch — the TUI
suspends, and its exit still triggers the re-read —
while everything else takes a **detached** launch: nothing is suspended, nothing is waited on, the
pane stays standing and the row says "opened in your editor". The `server` row performs the full
**live switch**, identical to `/server` — and takes no
reset, since its line is the switch's own recording (ADR 0036) and deleting it would leave the
session on a server the file no longer names, so `⌫` is inert there and its hint line says so;
`confine-to-workspace` / `unconfined-hosts` stay display-only with a "use /confine" pointer (that
loosen stays single-homed in **`/confine`**, ADR 0012). Idle-only, and the external edit is
offered between runs only.

What makes an edit apply is no longer the editor's exit but the **config watch**: a poll of
`~/.apogee/config.yaml`'s mtime and size on a ticker, in-process and dependency-free, whose report
is one **wait** the renderer parks (the Heartbeat posture) and re-arms per landed report. Any save
of that file applies live — the pane's own jump, a GUI editor in another window, a `vim` in a second
terminal — through the same re-read/diff/apply the round trip already used, so the two triggers
cannot land a key differently. Two rules bound it: the **last-good rule** (a file that does not
parse is ignored and the previous projection kept, because a poll will read a half-written save; a
run of three consecutive unreadable reports surfaces one transcript note, not repeated until the
file parses again), and **baseline refresh on every apply**, which is what keeps a pane write from
double-applying when the watch sees the file apogee itself just wrote. `server` is the one key a
re-read never reports (ADR 0036 decision 2). It **reconciles** the
standing "apogee never writes your config" claims (seeding never overwrites, Probe prints
paste-ready YAML, `/model` does not rewrite the file): never *unprompted* — a settings-screen edit
is a deliberate user act and names the file and entry it changed, the same fence ADR 0012 applies
to `/confine off --save`. Four keys are written off a gesture outside the pane, each admitted by
name: `server:` (ADR 0036 decision 2), `launch-profile:` under `remember-model:` (ADR 0048),
`ui.task-list-open`, which the transcript's task-list fold records silently — the click or `enter`
that folds every task-list card also writes the flip, only a failed write is mentioned (ADR 0035,
2026-09-14 addendum) — and `ui.tools-open`, which the fold of a large `✦ Tools (N calls)` umbrella
records the same silent way (ADR 0035, 2026-09-17 addendum; a small umbrella's fold is view state
and writes nothing — 2026-09-18 addendum). See
ADR 0035 (the
persistence contract) and
ADR 0037 (the live apply),
as amended by ADR 0041 (the editor ladder and the
config watch, superseding 0037's `$VISUAL`→`$EDITOR`→`vi` ladder and its diff-on-exit trigger).
_Avoid_: "settings menu" / "preferences dialog" (it is a pane inside the frame, not a modal
takeover), "config editor" (it edits declared keys through a verified splice, and hands the file
itself to the external edit for the rest; it is never a text editor of its own), "auto-sync" (no
boot-time key syncing exists — rejected in ADR 0035), "pending edit" / "(next launch)" (nothing is
pending — an edit applies on the ⏎ that persists it; the marker ADR 0035 introduced is abolished,
not narrowed), "`$EDITOR` round-trip" as the name of the whole mechanism (`$EDITOR` is one rung of
four, and a detached launch has no round trip to come back from), "file watcher" in the
`fsnotify`/inotify sense (it polls; there is no OS notification and no daemon).
