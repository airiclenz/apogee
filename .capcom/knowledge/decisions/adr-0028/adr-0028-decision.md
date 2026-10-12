---
title: ADR 0028 decision
---

**1. The pickers are UI over the prepared layers — never a third way to bind.** A picked model
becomes a `rebindIntent` fed to the same `applyRebind` orchestration a heartbeat-observed change
takes: the same seam call, the same fail-once refusal, the same restated start-up box, the same
`rebindNote` wording (`model changed: A → B`), the same notices, the same unknown-window honesty.
ADR 0024's "cold start, late seed and mid-session switch are ONE code path" is extended to the
switch **the human asks for**, by construction rather than by discipline — no second set of strings
exists that could drift from the first.

The pick is also **recorded as the last observation**, exactly as an observed change is recorded,
so the next beat reporting the picked model measures as "nothing new" rather than as a fresh change
to bind. ADR 0024's rule that a beat is measured against the last *observation* and never against
the current *binding* is what makes that one assignment sufficient.

**2. The discovery hint is a property of the BINDING, and follows it.** `heartbeat.Monitor` gains
`SetModel`, and the composition root's rebind closure re-states the hint on **every** committed
rebind — heartbeat-driven ones included, where it is a no-op restatement. The hint answers "which
of the models you serve do I mean", and after a rebind the honest answer is the model the session
actually runs, not the one config named at launch.

This is deliberately the principled fix rather than the defensive one. Suppressing the flap TUI-side
(remembering "the user picked B, ignore A") would have left discovery *itself* answering a stale
question, and would have needed a second rule for the pre-existing case where the heartbeat, not a
human, moved the binding away from the config'd model.

**3. A server switch UNBINDS the model; the new server's first beat completes the switch.**
`Agent.SwitchUpstream(UpstreamSpec{Endpoint, APIKey})` binds a fresh `provider.NewClient`, moves
`cfg.Endpoint`/`cfg.APIKey`, and sets `cfg.Model = ""`. *(Amended 2026-08-13 — see the Amendment
section below: the spec grew the two token bounds the new entry PINS, which are config facts rather
than discovered ones.)* It guesses **nothing** about the new
server: its model, window, system-prompt template and validated Mechanism set are facts only that
server can report *(Amended 2026-09-30: the Mechanism registry and Validated sets are gone —
ADR 0071,
ADR 0076 A9 — so there is no
set left to re-resolve)*, so the heartbeat discovers them and the ordinary `Rebind` applies them — one
code path with the cold start again, one Interval later at worst, and immediately in practice
because the fold fires the first beat at once. `errNoModelBound` guards `Submit` in the gap exactly
as it does before a session's first bind, and `blockedUpstream` refuses the three Exchange-opening
paths with the new endpoint named.

A **new client rather than a mutated one** is `provider.Client`'s own documented contract
("`SetModel` deliberately does NOT cover the endpoint — switching servers means a new Client"), and
it is also what moves the endpoint and the key atomically rather than through two independent
mutations. `SwitchUpstream` joins Rebind in the idle-only class (`ErrInputPending` mid-Exchange),
so no request is ever re-pointed at a different server underneath itself, and the boundary the host
crosses to call it **is** the synchronization for the loop's un-mutexed `cfg` reads (ADR 0024
decision 4, unchanged).

What stands across a switch: the conversation and Turn counters, the autonomy mode, session
approvals, the confinement flag, the resolved tools, and the model profile — none of them describe
a server. The catalogued Mechanism registry also stands, still armed for the model that went away,
until the follow-up Rebind rebuilds it; it is unreachable meanwhile, because no request can open
while nothing is bound. What resets is exactly what Rebind resets, for Rebind's own reasons: the
token estimator and the compaction saturation latch. `ctxUsed` survives, as it survives a rebind.

**4. The Monitor is per-server and is swapped WHOLE, behind unchanged seams.** A Monitor holds the
endpoint and key it was built with, so a switch replaces it rather than mutating it — the same
contract one level up from the client's. `tui.Options.Heartbeat` and `Options.Rebind` keep their
signatures; the composition root introduces a small mutex-guarded **holder** owning the current
Monitor, wires `Options.Heartbeat` to `holder.Beat`, and swaps what is behind it. The mutex guards
the pointer alone: the observation runs outside it, because a beat against a hung server would
otherwise stall the Update goroutine's next swap for seconds. A swap mid-beat is therefore possible
and safe — the in-flight beat lands carrying the retired generation, which ADR 0024's generation
guard already makes inert.

One new seam is added, and only one: `Options.SwitchServer(name) (ServerSwitchResult, error)`. The
TUI owns **when**, the binary owns everything the switch touches — resolving the name, the engine
call, the Monitor swap, restamping the session host's model to `""` so a save in the unbound gap
cannot claim the old model — and returns the display facts, including the pinned window, so the
renderer keeps needing no knowledge of the `context-window:` pin (ADR 0024 decision 6). Both new
members nil-degrade: no `SwitchServer` and no choices is one situation, answered with one line.

**5. `servers:` is a file-only config LIST, the startup endpoint is synthesized as a row, and the
switch is SESSION-scoped.**
*(Amended 2026-08-05 by ADR 0036:
the list is now the **single** definition of what servers exist — the top-level
`endpoint:`/`api-key:`/`host-alias:`/`model:` quadruple this decision sits beside is retired, so
there is no startup endpoint left to synthesize except an ephemeral `--endpoint`/`APOGEE_ENDPOINT`
override, and only that case still prepends a row. The **session-scoped half is superseded**: a
switch to a configured entry now records `server: <name>` in `config.yaml`, so the next launch
starts where the last session ended, and the "`server:` startup key" this ADR rejected below is
ratified there. What stands unchanged: the entry shape, `name`'s three jobs, keys being per-server
and file-only, and the invariant that the server the session started on is always offered.)*
Each entry carries a required `name` and `endpoint` plus optional
`api-key` and `model`. The `name` does three jobs with one field, mirroring `host-alias:`: it
labels the picker row, it is the `/server` argument, and it becomes the footer's host alias once
the session is on it. Keys are per-server and file-only, because `APOGEE_API_KEY` is a single value
that belongs to the **startup** server. The `model` is that server's discovery hint — `model:`,
per server.

The composition root always offers the endpoint the session started on, synthesizing a row for it
(name = the resolved `hostAlias`, key = the resolved startup key, hint = the config'd `model:`)
whenever no configured entry already names that endpoint, so switching away is reversible without
config surgery. And a switch changes the **running session only**: nothing is written back to
`config.yaml` and the next launch starts at `endpoint:` again — the `/confine off`-without-`--save`
posture. Session records keep storing the model alone; resuming a session does not restore a
switched endpoint.

**6. The post-switch seed is ANNOUNCED, where the launch seed is quiet.** First contact at launch
prints nothing, because the start-up box a few rows above already states the facts. After a
mid-session switch that box is deep in scrollback and the human just acted, so the bind **is** the
confirmation: the display state carries a `switched` flag, set by the switch fold and never
cleared, and the first-contact capture reads `!everOnline && failures == 0 && !switched`. Launch
stays quiet; every post-switch seed says `connected: <model>`.

The switch fold replaces the whole heartbeat state rather than patching it, which is what makes it
need no unwinding: a fresh generation retires the old chain, the offering empties with the server
that advertised it, and the offline debounce returns to its cold-start posture — the honest one,
because nothing has been observed of *this* server yet, so its first failed beat is believed at
once rather than debounced against evidence about a different machine.

**7. Two verbs, one overlay, idle-only, and every degrade is an honest note.** `/model` and
`/server` are ordinary rows in the one command table
(ADR 0027), both `takesArgs` and neither
`whileRunning`: the merged menu tags them `— idle only`, the dropdown **completes** them rather
than running them, and mid-run acceptance earns the standing note.
*(Amended 2026-08-13: the dropdown now **runs** both verbs bare at accept — each row carries
`runsBareAtAccept` (`internal/tui/command.go:94`), because the bare form only opens the picker and
mutates nothing until that picker's own accept, where a `/model ` nobody meant to finish would just
stand in the box. The idle-only tag and the mid-run standing note are unchanged.)*
*(Amended 2026-09-14: mid-run acceptance no longer earns a note — the line is **queued** and runs
at the next idle, ADR 0025 decision
10 as amended that day; the tag reads `— runs at idle`.)*
Idle-only is not incidental —
both end in idle-only engine calls, and a user-initiated switch racing the deferred `pendingRebind`
path would make "latest wins" ambiguous between the human and the server.

Bare opens a modal single-select overlay (the `/sessions` browser's simpler sibling, painted
through the shared popup module); one argument switches directly on an exact match; more is a usage
note. One state struct serves both verbs, distinguished by a **kind enum** rather than a callback
field, so the value-copied Model keeps holding plain values only (ADR 0011). Rows are **derived at
render time** — a beat landing under an open `/model` picker refreshes the offering in place,
selection clamped — and the row matching the current binding carries a `· current` suffix.
*(Amended 2026-07-29 by ADR 0029's
D3 amendment: `/model` no longer **offers** the row the session is already on, so that mark is
`/server`'s alone. The rest of this paragraph stands, the argument form's `already bound to …`
included.)* Every
way the verb cannot work is a sentence and no overlay: heartbeat unwired, upstream offline, nothing
advertised yet, `Rebind` nil (a display-frozen heartbeat), nothing configured to switch to. And
choosing what the session is already on is **answered** (`already bound to …`, `already on …`),
where `rebindNote`'s silent-when-nothing-moved contract is about the observations nobody asked for.
