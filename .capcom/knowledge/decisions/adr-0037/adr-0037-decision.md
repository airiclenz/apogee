---
title: ADR 0037 decision
---

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
gets live reconfiguration from the same doors, and the TUI's `SettingsHost.Apply` seam degrades
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
next
launch`. Startup precedence (flag > env > file > default) is untouched. (Amended 2026-10-29: ADR 0096 decision 7 splits "file" into project over global — flag > env > project > global > default — and a save to the global file under a value the Project config sets applies nothing new, and the row says `saved
to global; the project config outranks it` (`settingsOutranksNote`, `internal/tui/settings.go`).)

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
  (Amended 2026-09-30: the list has moved on. `mechanisms` and `validated-sets` are deleted
  (ADR 0076 A9) and the global `model-profile`
  retired in favour of `model-profiles`; the structured rows today are `servers`, `system-prompt-models`,
  `system-prompt-layers`, `unconfined-hosts`, `mcp-servers`, `dangerous-rules`, `allow`,
  `sessions.watch-homes`, `reactions` and `model-profiles` (the three between `mcp-servers` and
  `reactions` joined in October 2026) —
  the `KindStructured` rows of `internal/config/registry.go`. ADR 0041
  replaced the editor ladder.)

The list/map editor ADR 0035 declined is still declined — this record does not build one. It routes
around it: the user already has a text editor they trust, the file is already the documented shape
they hand-edit, and the only thing apogee was failing to do was *notice*. Two bounds keep that honest.
The jump is offered **only while no run is in flight** (otherwise the row says to wait), because
suspending the TUI under a landing stream and queueing applies behind a run are two problems bought
for nothing — a run is finite. And **the Interlocked confinement keys are skipped silently** on
reload (the registry flag was called `GlobalOnly` until 2026-10-10, when ADR 0096 gave that name to
its project-layer key class; `reloadNeverReports`, `cmd/apogee/settingsedit.go`) even
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
