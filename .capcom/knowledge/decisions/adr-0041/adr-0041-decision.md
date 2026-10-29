---
title: ADR 0041 decision
---

**1. A top-level `editor` key names the command.** It sits beside `server` and `llama-launcher` as a
top-level scalar, not under `ui:`: it does not describe how the pane looks, it names a program in the
user's environment, which is the same thing `llama-launcher:` does. (Amended 2026-09-30: there is no
top-level `llama-launcher:` key — ADR 0029's 2026-08-07 amendment moved it onto the `servers:` entry,
and `internal/config/configmigrate.go` refuses the old spelling. `editor` stays top-level.) It is free text split on
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
ADR 0036
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
files (the hot-reload ADR 0040
declined stays declined; re-selecting a scheme already re-reads it), not skills, not MCP manifests,
not workspace context files (session-scoped on purpose —
ADR 0026). *(Amended 2026-10-29: three files are watched today, one `filewatch.Watcher` each fanned
into one wait — `config.yaml`, the Project config (`adr-0041-amend-1010`) and the Project root's
adoption record under `~/.apogee/workspaces`, whose re-read installs the Allow rules it settles to
(ADR 0096 §4), `cmd/apogee/wire_live.go`. The scoping rule — the files the config is resolved from,
never what the config points at — stands.)*

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
while somebody is still typing. *(Amended 2026-10-29: what is counted is consecutive unreadable
RE-READS, not poll ticks — the watcher reports only a settled change (`internal/filewatch`), so a
broken file nobody saves again is not re-read, and a fix lands on the next save rather than the next
tick; `configWatchStallReports` and `foldConfigUnreadable`, `internal/tui/settingswatcher.go`. Three
and once stand.)*

**8. Self-writes must not double-apply, and a watcher apply is an ordinary apply.** The pane's own
write already applies the key live; **every** apply, from either path, refreshes the baseline, so the
watcher sees no change for a write apogee itself just made. A key the watcher applies is journaled
exactly as a pane edit is (ADR 0037 decision 8), lands on the same boundary its class dictates, and
refuses on its own row with the error verbatim when it cannot be applied — but it wears a marker of
its own, ` ~` rather than ` *`, because the two answer a different question about one row: ` *` says
the human changed it here, ` ~` says the file changed it under them. The journal holds one entry per
key, so the LAST source wins the marker — an in-pane commit over a watched key flips the row back to
` *`, and a save over a key edited in the pane flips it to ` ~`. *(Amended 2026-08-28 — this
decision originally gave a watcher apply the pane edit's ` *`; the distinct ` ~` and the
last-source-wins rule are what shipped, `settingsWatchMarker` and `settingsEditMarker`,
`internal/tui/settings.go`. No code changed.)*

**9. Everything else in ADR 0037 stands.** Superseded here are exactly two things: **binding B** —
the `$VISUAL`→`$EDITOR`→`vi` ladder of its decision 6, now a four-rung ladder starting at the config
key and ending at the OS opener — and **the diff-on-exit trigger**, the baseline refreshed at launch
and read when the child exits, which survives only for the foreground path of decision 6. Untouched
and still governing: the `ApplySetting` seam and its per-key dispatcher (whose signature this record
does not change), the two mutator classes of decision 2 and the boundaries they land on, the
boundary/override/failure row notes of decisions 1, 3 and 4, the idle-only `/settings` policy and the
idle-only editor jump, the ` *` / ` ~` marker pair of decision 8, the picker-driven `server` row of decision 5,
the MCP validate-then-commit reconnect of decision 7, and the write fence and persistence contract of
decision 9. ADR 0035's contract is likewise untouched. *(Amended 2026-08-28 — decision 8's marker
is the ` *` / ` ~` pair, not ` *` alone; see the amendment there.)*
