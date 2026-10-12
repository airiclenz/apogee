---
title: ADR 0041 considered options
---

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
