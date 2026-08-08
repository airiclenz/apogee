---
Status: accepted
---

# Every settings edit applies to the running session

> **Superseded in part by [ADR 0041](0041-the-config-file-is-watched.md) (2026-08-06).** Two
> mechanisms of decision 6 are replaced. **Binding B** — the `$VISUAL` → `$EDITOR` → `vi` ladder —
> becomes a four-rung ladder that starts at a new top-level `editor` config key and ends at the OS
> default opener (`open` / `xdg-open` / `cmd /c start ""`), so unset no longer means `vi`. And **the
> diff-on-exit trigger** — the baseline taken at launch, re-read when the child process exits — is
> replaced by a **watcher over `config.yaml`** that runs for the whole session: a launcher stub such
> as `open` returns before the editor is even on screen, so an exit-triggered diff reads unchanged
> bytes and concludes the user edited nothing. Terminal editors keep the blocking, TUI-suspending
> path and still apply on exit; everything else launches detached and the watcher supplies the apply.
> With it, the rejected option "a file watcher, so any external edit reloads live" and the
> consequence "an edit made outside apogee still does not reload" are reversed. **Everything else
> here stands** — the `ApplySetting` seam and its dispatcher, the two mutator classes and their
> boundaries, the idle-only mutators and the idle-only editor jump, the row notes, the ` *` marker,
> and the rule that a key which cannot be applied refuses on its own row.

> **Note 2026-08-07 — the `llama-launcher` row left this surface.** ADR 0029's global
> `llama-launcher:` key retired when the launcher moved onto the `servers:` entry it fronts (that
> record's amendment of the same date), and the row went with it: no registry row, no formatter, no
> `ApplySetting` case. Nothing here is superseded — the row was an ordinary anytime-safe member of
> decision 2's first class and left as one — but the live-apply story for the launcher is now a
> different mechanism entirely. Enablement follows the session's server: startup entry selection, a
> `/server` switch and a bind install the entry's launcher path, so the thing that used to be a key
> you edited on this screen is a consequence of a move you make in the picker decision 5 put here.
> The setting itself now lives inside the `servers:` block, which is a decision-6 `$EDITOR` key —
> where a per-entry field belongs.

## Context

[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) built the settings
surface and drew two lines around it that were true when it was written and are not design. Its
decision 9 made `mode` **the only live apply**: every other committed edit rendered a
`→ value (next launch)` marker, a row already overridden by an env var or flag said so *instead of*
applying, and "the pane never rebinds". Its decision 3 kept v1 to **simple keys only**: bools, ints,
strings and enums are editable, while `servers`, `mcp-servers`, `mechanisms`, `validated-sets`,
`system-prompt-models`, `model-profile`, `context-files.names`, `system-prompt-text` and
`system-prompt-file` rendered read-only behind an **"edit in `config.yaml`"** pointer.

Both lines were drawn for the same reason: the engine had one live mutator (`SetMode`) and the pane
had one editor (an append/pop text buffer). Neither limitation was argued from what a settings
screen *should* be — 0035 says so itself, calling the marker "the visible cost of decision 9" and
the pointer "honest about where that editing happens today". What that honesty ships is a screen
listing forty keys that, for most of them, tells you to restart or to go somewhere else — and the
surface met its first user the day it landed. The owner's 2026-08-06 layout note put it plainly: *"settings must take effect immediately … I do not
want `→ new value (next launch)`"* — and the row should show the new value in place, marked ` *`.

What changed underneath is that the seams exist or are one small mutator away. `SetMode` and
`SetConfineToWorkspace` established the **anytime-safe** class (each behind its own mutex, read live
by the worker); `Rebind` established the **idle-only validate-then-commit** class, and the TUI
already defers a rebind that arrives mid-run through `pendingRebind` and applies it at the next
quiescent boundary ([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md),
[ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)). `/server` already performs a whole live
rehome and records the choice ([ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md),
[ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)).
The question left is not *can a running session absorb a settings change* but *which boundary each
change lands on, and what the row says while it waits*.

A 2026-08-06 grill settled that, question by question, with the owner. This record is its
ratification; it is implemented by `docs/plans/2026-08-06 - 00 - settings-live-apply-plan.md` and its
visual half by `docs/layout/settings-screen-layout.md`.

The constraints it lands inside:
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
— every mutator must be a Driver-agnostic engine seam, and the engine stays wire-silent (it is
handed resolved values; it never reads a config file);
[ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) — the renderer is thin, its
Model is copied by value, and no YAML enters it;
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) — the
confinement loosen stays single-homed in `/confine`;
[ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md) and
[ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) — prompt-prefix
data is session-scoped on purpose, because a prefix that changes mid-session throws away the KV
cache;
and ADR 0035's own decision 2, whose persistence contract this record leaves entirely alone.

## Decision

**1. A committed edit applies to the running session, at ⏎, one key at a time.** The order is
**validate → persist → apply**: the pane commits an edit exactly as ADR 0035 decision 2 specifies,
and then — same keystroke, same row — the value takes effect. Nothing is batched, and closing the
pane is dismissal, not a save: there is no dirty state, no "apply on close", no second moment at
which something happens. An apply that fails *after* a successful persist does not unwind the write
— the file already expresses the user's intent and the next launch will honour it — and the row
carries the failure verbatim: `saved — live apply failed: <error>`. Re-committing the row retries the
apply.

This **supersedes ADR 0035 decision 9**. The `mode` idiom that decision described as the exception is
now the rule, and the `(next launch)` marker it introduced is deleted rather than narrowed.

**2. Every new mutator lands in one of two classes, and there is no third.** A live seam is either
**anytime-safe** (the `SetMode` shape: one mutex, one field, consumed at a boundary the loop crosses
constantly — a hook fire, a compaction check, a request render) or **idle-only validate-then-commit**
(the `Rebind` shape: refuses while an Exchange is in flight, validates fully before swapping, and
rides the TUI's existing `pendingRebind` deferral so a mid-run edit lands at the next quiescent
boundary). Every tool-registry change — web search appearing or disappearing, an MCP reconnect —
rides **one** generic idle-only `SwapTools`; a second registry-mutation path may not appear.

The seams are engine methods taking resolved values, not config paths: the composition root reads
files, resolves precedence and validates; the engine is handed a struct. That is ADR 0031's
wire-silence and its benchable-all-the-way-up invariant doing their job — a bench or daemon Driver
gets live reconfiguration from the same doors, and the TUI's `Options.ApplySetting` seam nil-degrades
to persist-only for a Driver that wants no live apply at all.

**3. Keys whose effect is a prefix land at the next natural boundary, and the row says which.**
`system-prompt-text` / `system-prompt-file` / `system-prompt-models` re-resolve and ride the rebind,
so they land at the next engine-idle moment — in practice the next turn, with no note, because
nothing observable happens in between. `context-files.enable` / `context-files.names` are picked up
by the next `/clear` or session start and the row says `· applies at next clear`: the per-session
freeze is ADR 0026's deliberate KV-prefix stability, not an implementation gap, and re-reading
workspace files under a live conversation would invalidate the whole prefix for a change the user did
not ask to have applied retroactively.

A boundary note is the *only* deferral wording the surface has. **No row ever says "(next launch)"
about its own effect again.**

**4. A pane edit outranks an env or flag override for the running session.** The edit applies live
and persists; the row then notes that the override wins again next time — `· APOGEE_MODE outranks at
next launch`. Startup precedence (flag > env > file > default) is untouched.

This reverses ADR 0035 decision 9's second half, which had the overridden row report the override
*instead of* acting. The pane is where the user is expressing an intent **now**, and a launch-time
override is a statement about how this process started, not a veto over what it does next. Refusing
the edit would make the row a museum label; applying it and naming the override is both true
sentences at once.

**5. The `server` row IS `/server`, driven from a picker.** ⏎ on `server` opens a selection popup
over the configured entries with the current one marked; choosing calls the same `SwitchServer` seam
`/server` calls — mover, heartbeat rebind, and the `server:` recording that ADR 0036 decision 2 made
the persist for this key — and the switch's progress surfaces in the transcript exactly as `/server`'s
does. The pane adds no rendering and no second implementation of the rehome; a free-text buffer is
refused outright, because the valid values are a closed set the file already enumerates and a typo
would write the stale pointer ADR 0036 decision 4 has to recover from.

This is where ADR 0035's "live model/server switching stays with `/model` and `/server`" is honoured
rather than broken: the row is a **second entrance to the same seam**, not a second, less-informed
flow. Popups are the affordance for three-or-more-option keys generally; **bools keep the instant ⏎
toggle**, because a two-item popup is a keystroke tax.

**6. Structured editing is hybrid: three keys come in-pane, six go out to `$EDITOR`.** This **widens
ADR 0035 decision 3**, whose read-only pointer is deleted everywhere.

- **In-pane:** `system-prompt-file` (a validated string row), `context-files.names` (a comma-separated
  list row) and `system-prompt-text` (a multiline editor entered with ⏎, where ⏎ inserts a newline,
  **ctrl+s saves and esc discards** — stated on the hint line, because an editor whose commit key is
  not the obvious one must say so).
- **Out to `$EDITOR`:** the six genuinely nested structures — `servers`, `system-prompt-models`,
  `mcp-servers`, `mechanisms`, `validated-sets`, `model-profile` — open the user's own editor at the
  key's line (`$VISUAL`, then `$EDITOR`, then `vi`; `notepad` on Windows), with a `+<line>` argument
  passed only to editors known to accept one. On return apogee re-reads the file, and **only then**:
  a parse or validation failure applies nothing, reports the error on the row, and never rewrites the
  file; a clean parse is diffed key-by-key against the previous config and each changed key is applied
  through the same dispatcher an in-pane edit uses, with per-key notes and errors landing on their own
  rows.

The list/map editor ADR 0035 declined is still declined — this record does not build one. It routes
around it: the user already has a text editor they trust, the file is already the documented shape
they hand-edit, and the only thing apogee was failing to do was *notice*. Two bounds keep that honest.
The jump is offered **only while no run is in flight** (otherwise the row says to wait), because
suspending the TUI under a landing stream and queueing applies behind a run are two problems bought
for nothing — a run is finite. And **GlobalOnly confinement keys are skipped silently** on reload even
when the round-trip changed them: ADR 0012's acknowledgement interlock is a *flow*, and a file edit is
not that flow, so `/confine` remains the only door that can loosen the fence for a running session.

**7. MCP reconnect validates before it commits, and keeps the old connections on failure.** A changed
`mcp-servers` set connects the **new** servers first; on success the tool registry is rebuilt (host
tools plus the newly folded MCP tools) and swapped through `SwapTools`, and only then are the old
sessions closed. On any failure — a server that will not connect, or an engine too busy to accept the
swap — the previous sessions keep serving, nothing is swapped, and the row reports
`reconnect failed: <error> — previous connections kept`. **Startup connect failure stays fatal**, and
that asymmetry is deliberate: at startup there is no working state to preserve, mid-session there is.

**8. The ` *` marker means "changed through the settings surface this session".** It is set by an
in-pane edit, by a reset (which renders `default *`), and by every key an `$EDITOR` round-trip
changed; it is cleared only by relaunch. It replaces the entire pending-arrow apparatus —
`→ value (next launch)`, `saved (next launch)`, and the registry's `RestartRequired` flag with it,
because after this record **no key is restart-gated**. The remaining row annotations are exactly three:
the boundary note of decision 3, the override note of decision 4, and the failure note of decision 1.
A masked key follows the same rule and stays masked.

**9. ADR 0035's persistence contract is untouched, and so is the write fence.** One key per deliberate
edit, guided by parsed node positions, spliced textually, re-parsed and compared against the original
apart from the target path, written atomically; an inserted key lands below its commented example; a
reset deletes the line rather than freezing today's default. The two new value shapes — a flow sequence
for the string list, a block scalar for the multiline text — extend the *renderer* inside that
contract, and are verified by the same whole-file compare.

Nothing here lets apogee write on its own initiative. The three promoted keys join the authorized write
set as ordinary deliberate edits, under the same fence and the same obligation to name the file and
entry they changed. The `$EDITOR` round-trip is not an apogee write at all — it is the user's own hand
on their own file — and the reload path is read-only by construction: it applies or it reports, and it
never rewrites what it just read.

## Considered options

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

## Consequences

- **ADR 0035 decision 9 is superseded and decision 3 is widened; everything else in that record
  stands** — decision 2's persistence contract above all, along with the registry-as-source-of-truth
  bijection guard, the insert-below-example rule, reset-by-deletion, the idle-only `/settings` policy,
  the masked `api-key`, and decision 8's confinement single-homing. Its consequence line about "most
  edits are honest about applying at next launch" no longer describes the surface.
- **The engine grows a live-mutability surface**, and `Agent`'s call-class doc comment is the authority
  for it: every new setter names its class (anytime-safe or idle-only) and its consumption boundary in
  its own godoc. The anytime-safe set grows past `SetMode` / `SetConfineToWorkspace` — each new member
  guarded by its own mutex, none of them sharing one.
- **Every Driver gains live reconfiguration, not just the TUI.** The seams are engine methods over
  resolved values, so a bench, a daemon or a future remote Driver reconfigures a running agent through
  the same doors — ADR 0031's tripwire holds, and a capability welded into the pane would now fail
  visibly at those call sites.
- **ADR 0035's authorized write set gains three keys' worth of shape** (`system-prompt-file`,
  `context-files.names`, `system-prompt-text`), all of them ordinary deliberate edits through the same
  verified splice. The set's membership rule is unchanged: nothing apogee decides on its own initiative
  may join it.
- **`layout.md` and `docs/layout/settings-screen-layout.md` own the pane's chrome** — the two-line
  fixed-height description header, white section headers with spacer lines, the highlighted edit row,
  the single-line and multiline editor states with their hint lines, the picker, and the
  `⏎ opens $EDITOR` affordance. This record fixes the behaviour; the grammar is documented where the
  pane's grammar lives.
- **`CONTEXT.md`'s Settings surface term changes shape**: "`mode` is the only live apply" and the
  "(next launch)" marker leave it; live apply on commit, the session edit journal behind ` *`, and the
  `$EDITOR` round-trip enter it.
- **Changing `present.port` or `present.host` closes the docs listener** if it is already bound, and the
  next presentation binds on the new address. URLs already handed out stop resolving — inherent to a
  port change, and the alternative (serving the old address until restart) is the "(next launch)" this
  record removes.
- **An edit made outside apogee still does not reload.** Only the round-trip apogee itself launched
  triggers a reload, per the rejected file watcher; the file remains the user's document, read at the
  moments apogee has a reason to read it.
- This is a user-visible feature wave, and it **warrants a minor bump** when the next version is cut.
  That call is the owner's, and no item of the implementing plan touches a version identifier.
