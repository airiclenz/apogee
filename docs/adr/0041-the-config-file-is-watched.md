---
Status: accepted
---

# The config file is watched

## Context

[ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md) made the settings surface
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
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) — the renderer is thin, its
Model is value-copied, and no YAML enters it, so the watcher and every re-read live in the
composition root;
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md) —
the engine is wire-silent and is handed resolved values, so what the watcher produces is the same
per-key apply any Driver already drives;
[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) — the persistence
contract, which this record leaves entirely alone;
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) — the
confinement interlock stays single-homed in `/confine`, so those keys are still skipped by a file
reload;
and [ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)
— `server:` is the recorded startup binding, so a reload still never reports it.

## Decision

**1. A top-level `editor` key names the command.** It sits beside `server` and `llama-launcher` as a
top-level scalar, not under `ui:`: it does not describe how the pane looks, it names a program in the
user's environment, which is the same thing `llama-launcher:` does. It is free text split on
whitespace, so `editor: code -w` resolves to `["code", "-w"]` and `emacsclient -nw` keeps its flag —
the same shape a `$EDITOR` carrying flags has always had. It is an ordinary editable string row with
no validator (a command line is not a vocabulary apogee can check without running it — the posture
`present.command` already takes) and it persists through ADR 0035's contract like every other scalar,
spliced below its commented example.

The key carries **no live-apply seam of its own**. It is read at editor-launch time from the file's
own projection, so setting it in the pane takes effect on the very next ⏎ that opens an editor, with
nothing to dispatch and nothing to journal beyond the write itself.

**2. Precedence: config beats environment.** Resolution is `editor` → `$VISUAL` → `$EDITOR` → the OS
default opener. This is not an inversion of startup precedence (flag > env > file > default), because
`$VISUAL` and `$EDITOR` are not apogee's env override for this key — there is no `APOGEE_EDITOR` and
none is wanted. They are the *system's ambient default*, the rung below an explicit apogee setting.
An explicit setting outranking an ambient one is also what keeps the pane honest: the `editor` row
shows the command that will really run, and a row reading `code -w` while an inherited `$VISUAL=vi`
quietly won would be a lie on the screen.

**3. The completion signal is a file watcher, not process exit.** Apogee polls `config.yaml` for the
whole session — `os.Stat` on a ticker, a change in **either** mtime or size counted as a change, a
short settle delay coalescing an editor's write/truncate/rename burst into one report — and applies
whatever changed. One goroutine, no third-party dependency, no `fsnotify`, no inotify, no daemon, no
out-of-process anything.

This supersedes ADR 0037's diff-on-exit trigger for detached launches and the rejection of a watcher
that stood behind it. It also supersedes an earlier call made in the same 2026-08-06 session — that
apogee would append wait flags to known GUI editors (`code` → `code -w`) — which is **not** to be
implemented: with a watcher, wait flags buy nothing, and a table of every editor's blocking flag is a
guess apogee cannot win for an arbitrary program someone types into the key.

**4. Unset means the OS default opener** — `open` on darwin, `xdg-open` on linux, `cmd /c start ""`
on windows, and `xdg-open` for anything else. This is a deliberate behaviour change: unset used to
mean `vi`. The desktop already knows which application opens a `.yaml`, and it knows better than
apogee does; the old fallback answered "the editor of 1976" to everyone who had simply never set
`$EDITOR`, including the people least likely to know how to leave it. It is the posture
[ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)
decision 3 took toward an unset `server:` — defer to the user's own answer rather than guess a value
they never gave.

When the resolved program does not exist, the failure **names all three ways to set an editor** — the
`editor` key, `$VISUAL`/`$EDITOR`, and the opener it tried — rather than repeating Go's
`executable file not found in $PATH`, because on a bare container or a Linux box without
`xdg-utils` that error names a program the user never chose.

**5. The watcher runs for the whole session, and every external change applies.** Not only the edit
apogee launched: a GUI editor left open in another window, a `vim ~/.apogee/config.yaml` in a second
terminal, a dotfile manager rewriting a key. The file is the user's document and saving it is the act;
who opened it is not apogee's business. Watching is scoped to **`config.yaml` alone** — not scheme
files (the hot-reload [ADR 0040](0040-color-schemes-are-embedded-roles-with-user-shadowing.md)
declined stays declined; re-selecting a scheme already re-reads it), not skills, not MCP manifests,
not workspace context files (session-scoped on purpose —
[ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)).

**6. Terminal editors keep the blocking, TUI-suspending path; everything else is detached.** The nine
names already known to take a `+<line>` jump — `vi`, `vim`, `nvim`, `nano`, `pico`, `emacs`, `micro`,
`hx`, `kak` — suspend the TUI, run in the foreground, and still apply on exit exactly as today. Every
other program is started detached: it does not take the terminal, it does not inherit the TUI's
stdin/stdout, the pane stays up, and the watcher supplies the apply. This is not a preference between
two styles — a terminal editor drawn over a live alt-screen TUI is broken, and a GUI editor held
open by `tea.ExecProcess` blanks the user's session for as long as they are editing.

Both paths end in the **same** apply, through the same per-key dispatcher: one apply path, two
triggers. The `+<line>` argument stays bound to that same set of nine and is never handed to an OS
opener, which would take it for a filename.

**7. A file that does not parse is ignored, and the last good config is kept.** A watcher will
inevitably read a file mid-save. A projection that fails to parse or validate is dropped, the
previous baseline stands, nothing is applied, and the next tick retries — llama-launcher's rule, for
llama-launcher's reason. Unlike llama-launcher, apogee has somewhere to say so: a failure that
persists across **three consecutive ticks** surfaces **once** as a transcript note, and is not
repeated until the file parses again. Three, because a single tick landing inside a write is normal
and self-correcting; once, because a note per tick would be an error scrolling past every second
while somebody is still typing.

**8. Self-writes must not double-apply, and a watcher apply is an ordinary apply.** The pane's own
write already applies the key live; **every** apply, from either path, refreshes the baseline, so the
watcher sees no change for a write apogee itself just made. A key the watcher applies journals its
` *` marker exactly as a pane edit does (ADR 0037 decision 8), lands on the same boundary its class
dictates, and refuses on its own row with the error verbatim when it cannot be applied.

**9. Everything else in ADR 0037 stands.** Superseded here are exactly two things: **binding B** —
the `$VISUAL`→`$EDITOR`→`vi` ladder of its decision 6, now a four-rung ladder starting at the config
key and ending at the OS opener — and **the diff-on-exit trigger**, the baseline refreshed at launch
and read when the child exits, which survives only for the foreground path of decision 6. Untouched
and still governing: the `ApplySetting` seam and its per-key dispatcher (whose signature this record
does not change), the two mutator classes of decision 2 and the boundaries they land on, the
boundary/override/failure row notes of decisions 1, 3 and 4, the idle-only `/settings` policy and the
idle-only editor jump, the ` *` marker of decision 8, the picker-driven `server` row of decision 5,
the MCP validate-then-commit reconnect of decision 7, and the write fence and persistence contract of
decision 9. ADR 0035's contract is likewise untouched.

## Considered options

- **Auto-append wait flags to known GUI editors** (`code` → `code -w`, `subl -w`, `open -W`) — the
  call this session made first, and superseded per decision 3. It needs a table of every editor
  anyone might name, each entry wrong for somebody (`open -W` waits on an application the user may
  already have running), and it cannot help the user who names a program apogee has never heard of.
  The watcher needs to know nothing about any of them.
- **Keep the exit trigger and document "configure a blocking editor"** — rejected: it makes the
  failure the user's fault for owning a GUI editor, and no documentation can rescue the default
  opener path, which is a stub by construction.
- **`fsnotify` / inotify** — rejected: a third-party dependency and four sets of per-OS event
  semantics for a file that changes a handful of times a session. Editors that save by writing a
  temp file and renaming over the target defeat naive watches anyway, so the poll would have to
  exist as the fallback regardless; a one-second `os.Stat` is cheaper than the code that would
  choose between them.
- **mtime alone as the change witness** — rejected: a one-key edit is exactly the case that produces
  a same-second rewrite of identical length on a coarse-timestamp filesystem. Size is a second free
  witness.
- **Hashing the file every tick** for an exact answer — rejected: it turns an idle apogee into a
  process that reads and digests a document every second forever, to catch a rewrite that lands in
  the same second *and* at the same length. That residual miss is bounded — the next save reports
  it, and the apply is per-key and idempotent.
- **Watching every file apogee reads** (schemes, skills, MCP manifests, context files) — rejected per
  decision 5: each has its own reload semantics and its own reasons against, and one of them (context
  files) is deliberately frozen per session by ADR 0026. One file, one contract.
- **Re-reading and re-applying unconditionally on every tick**, as llama-launcher's `Reload` does —
  rejected: llama-launcher's reload is a struct copy with no side effects, and apogee's apply is not.
  An unconditional re-apply would journal ` *` markers, rebind a docs listener and reconnect MCP
  servers on a file nobody had touched.
- **Prompting instead of applying** — "config.yaml changed; press r to reload" — rejected: that is
  the `(next launch)` deferral in a different costume, and ADR 0037 decision 1 already deleted the
  model where the act and its effect are two separate moments.
- **Suspending the watcher while a run is in flight** — rejected: every apply already lands on the
  boundary its key's class dictates (anytime-safe immediately, idle-only at the next quiescent
  boundary), so a second gate protects nothing. The editor *jump* stays idle-only for the reason
  ADR 0037 gave — suspending the TUI under a landing stream is a different problem.
- **Launching every editor detached, terminal ones included** — rejected: a terminal editor started
  detached draws into the terminal the TUI is painting, and a full-screen one fights it for the
  alternate screen. Two paths exist because two kinds of editor exist.
- **Namespacing the key as `ui.editor`** — rejected per decision 1: it is not a rendering setting, and
  a namespace of one is a namespace nobody finds.
- **Keeping `vi` as the unset fallback** — rejected per decision 4.
- **Letting `$VISUAL` outrank the config key** — rejected per decision 2: an ambient environment
  variable is not a statement about apogee, and the pane row must name the command that will run.

## Consequences

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
  starts the watcher gets the same live reconfiguration).
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
