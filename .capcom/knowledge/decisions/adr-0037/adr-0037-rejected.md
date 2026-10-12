---
title: ADR 0037 considered options
---

- **Keep ADR 0035 decision 9 as-is** (`mode`-only live apply, honest "(next launch)" markers) —
  rejected: the marker was honest about a *limitation*, and 0035 said so. Once each key has a seam, the
  marker stops describing a constraint and starts describing a refusal, and it was never uniformly true
  anyway: several keys it marked are read per-request or per-hook-fire, so "next launch" overstated the
  wait as often as it stated it.
- **Batch the edits and apply on close** — rejected, and for the reason ADR 0035 rejected batching the
  *writes*: it invents a dirty-state model (what does esc mean, what happens on a crash, what does a
  second `/settings` show) and it separates the moment of the act from the moment of the effect, which
  is the one thing a settings screen must not do.
- **In-pane form editors for the six nested structures** — rejected, as ADR 0035 decision 3 rejected
  them: ordering, deletion, nested validation and multi-line text are a design problem of their own, and
  a half-built form over a file the user hand-edits is worse than the file. The `$EDITOR` jump buys the
  whole capability for a fraction of the surface.
- **Keeping the read-only "edit in `config.yaml`" pointer** — rejected: it is a dead end inside a screen
  whose entire claim is that this is where configuration lives, and it makes the user do by hand the one
  step apogee can do well — noticing what changed and applying it.
- **A file watcher, so any external edit reloads live** — rejected: an editor's save-in-progress states,
  a half-written file and an edit the user is not finished making would all become apply events. The
  reload is triggered by *returning from the editor apogee opened*, which is an unambiguous end-of-edit
  signal.
- **Skipping live apply on rows overridden by env or flag** — rejected per decision 4.
- **Deferring MCP reconnect to a later plan** — rejected explicitly by the owner: a surface that can
  edit `mcp-servers` and cannot honour the edit rebuilds, for the most consequential key on the screen,
  exactly the dead end this record exists to remove.
- **Reconnecting by tearing the old sessions down first** — rejected: a failed connect would leave the
  session with no MCP tools at all, having destroyed a working set to reach a broken one. Validate-then-
  commit is the same shape `Rebind` and `SetProfile` use.
- **A tool-registry mutation path per feature** (one for web search, another for MCP) — rejected: two
  doors into the same field is two places for the idle-only refusal and the sub-agent inheritance rule
  to drift. One `SwapTools`.
- **A free-text `server` row** — rejected per decision 5.
- **Letting the pane re-read `config.yaml` itself after the editor returns** — rejected: ADR 0011 keeps
  YAML out of the renderer. The root re-reads, diffs, applies and returns per-key display values; the
  pane renders what it is handed.
- **Applying `context-files.*` immediately by rebuilding the prompt prefix mid-conversation** —
  rejected: ADR 0026's session scope is a cache-stability decision, not an oversight, and the next
  boundary is one `/clear` away. The row says so.
- **Offering the `$EDITOR` jump mid-run** — rejected per decision 6.
- **Live-applying confinement keys after an `$EDITOR` round-trip** — rejected per decision 6 and
  ADR 0012's acknowledgement interlock; the reload skips them without comment rather than pretending a
  file edit is that act.
- **Keeping `RestartRequired` in the registry for some future key** — rejected: unused metadata is an
  invitation, and a key that genuinely could not apply live would have to amend this record anyway —
  at which point reintroducing the flag is cheaper than having carried it.
