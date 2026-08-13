---
Status: accepted
---

# A server switch rehomes the session, and the first beat completes it

## Context

[ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md) built the
two layers a user-initiated switch needs and deliberately built no UI over them. Its decision 8 is
explicit: every beat carries the server's whole `/v1/models` offering into TUI state and "nothing
renders it — that is the deliberate data layer for the future `/model` picker and `/server`
switch". Its consequences say the same from the other end: "the picker work is now cheap and
unblocked… `Rebind` deliberately never touches `Endpoint`, and `errMissingEndpoint` stands."

So apogee could follow a model the *server* changed, but a human could not change one. Switching
model meant restarting the server from a launcher beside apogee and waiting ten seconds for the
heartbeat to notice; switching **server** meant quitting apogee, editing `endpoint:` in
`config.yaml`, and starting a new session — losing the conversation, the approvals and the mode to
a change that describes none of them. `TODO.md`'s "[P1] Server / model switching" had been parked
on exactly that gap since before the heartbeat existed.

One defect was latent in the prepared layers and had to be fixed before any picker could ship. The
Monitor is constructed once with the config'd `model:` as its **discovery hint**, and nothing ever
moved that hint. On a multi-model server the hint is what resolves which model a beat reports — so
a session that bound B while the hint still said A would have the very next beat resolve A, measure
it as a change, and rebind **back to A** within one Interval. That flap is not a picker bug: it was
already reachable through a heartbeat-observed rebind away from the config'd model, on any server
that later served both ids.

This ADR records the design settled on 2026-07-28 and implemented by
`docs/plans/2026-07-28 - 05 - model-server-picker-plan.md`. It supersedes nothing. It extends
ADR 0024's decisions 3 (one code path), 5 (two seams, split by job) and 8 (the prepared data
layer), and it adds one member to [ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)'s
idle-only engine-call class.

## Decision

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
server can report, so the heartbeat discovers them and the ordinary `Rebind` applies them — one
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
*(Amended 2026-08-05 by [ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md):
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
([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md)), both `takesArgs` and neither
`whileRunning`: the merged menu tags them `— idle only`, the dropdown **completes** them rather
than running them, and mid-run acceptance earns the standing note. Idle-only is not incidental —
both end in idle-only engine calls, and a user-initiated switch racing the deferred `pendingRebind`
path would make "latest wins" ambiguous between the human and the server.

Bare opens a modal single-select overlay (the `/sessions` browser's simpler sibling, painted
through the shared popup module); one argument switches directly on an exact match; more is a usage
note. One state struct serves both verbs, distinguished by a **kind enum** rather than a callback
field, so the value-copied Model keeps holding plain values only (ADR 0011). Rows are **derived at
render time** — a beat landing under an open `/model` picker refreshes the offering in place,
selection clamped — and the row matching the current binding carries a `· current` suffix.
*(Amended 2026-07-29 by [ADR 0029](0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)'s
D3 amendment: `/model` no longer **offers** the row the session is already on, so that mark is
`/server`'s alone. The rest of this paragraph stands, the argument form's `already bound to …`
included.)* Every
way the verb cannot work is a sentence and no overlay: heartbeat unwired, upstream offline, nothing
advertised yet, `Rebind` nil (a display-frozen heartbeat), nothing configured to switch to. And
choosing what the session is already on is **answered** (`already bound to …`, `already on …`),
where `rebindNote`'s silent-when-nothing-moved contract is about the observations nobody asked for.

## Considered options

- **Let `SwitchUpstream` mutate the existing client's endpoint** — rejected, and it is the
  provider's own documented line rather than a preference: `Client.SetModel` covers the model
  precisely because the endpoint is not a mutable property of a bound client. Mutating endpoint and
  key separately would also open a window in which a request could carry one server's URL and
  another's bearer token.
- **Keep the old binding "until corrected" after a switch** — rejected as dishonest, and it is the
  same rejection ADR 0024 made for "refresh the display only": for up to one Interval the footer,
  the start-up box and the Budget would describe a server the wire is no longer pointed at, and the
  gap is exactly when a human is watching. Unbound says the true thing, and `connecting…` is a
  state the cold start already teaches.
- **Tear down and reconstruct the Agent for a new endpoint** — rejected. The conversation, the Turn
  counters, session approvals, the mode and the confinement state all survive a switch *by design*;
  reconstruction would destroy them to change one URL, and it is the same posture Rebind already
  takes for a model.
- **Swap the `Options` func values TUI-side** (build a new Monitor and rebind closure in the
  renderer) — rejected: it would put upstream lifecycle — client construction, key handling,
  Monitor ownership — inside the thin renderer, which ADR 0011 and ADR 0010 keep deliberately
  ignorant of `internal/provider`. The holder keeps the seams' signatures and moves the ownership
  nowhere.
- **Suppress the flap-back TUI-side** instead of moving the discovery hint — rejected: it treats
  the symptom, leaves discovery answering a question the session has moved past, and needs a second
  rule for the heartbeat-driven case where no human picked anything.
- **A `--save` form for `/server`, and a `server:` startup key naming an entry** — rejected for
  now, not on principle. Session-scoped is the smaller, reversible claim and matches
  `/confine off`; both are additive later, and neither is needed to make the switch useful.
  *(Amended 2026-08-05 by [ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md):
  the `server:` key is ratified and a switch records into it automatically; the `--save` form stays
  rejected — a human who picked a server has already stated the intent a flag would ask them to
  repeat, and `/confine off`'s asymmetry is about loosening a safety fence, not about which machine
  serves the session.)*
- **Persist the switched endpoint in the session record** so `--resume` returns to it — rejected
  with the same reasoning: a session record describes a conversation, and which machine served it
  is not part of what is being resumed.
- **Make `/server` require a reachable current server** (the offline degrade `/model` takes) —
  rejected: switching away is the single most useful thing to do *while* the current server is
  unreachable. Where the session can go is config, not an observation.
- **A callback field on the picker state instead of a kind enum** — rejected mechanically: the
  Model is copied by value on every `Update` (ADR 0011), and the state stays plain values so that
  rule needs no new exception.
- **Keep first contact quiet after a switch too** (one rule, no `switched` flag) — rejected: the
  quiet seed's whole justification is a start-up box the human can still see. After a switch they
  cannot, and they asked — an unacknowledged act is worse than one redundant line.

## Consequences

- **The public Go surface gains `Agent.SwitchUpstream(UpstreamSpec)` and the `apogee.UpstreamSpec`
  alias** — additive (ADR 0010), so the next cut is a **minor** bump. An embedder who never calls
  it is byte-identical to before.
- **`tui.Options` gains `Servers` and `SwitchServer`**, both degrading to a note. A host that wires
  neither gets exactly today's renderer, with `/server` explaining itself.
- **`config.yaml` gains one file-only key, `servers:`.** Absent, nothing changes: the session is
  offered the one server it started on and no behaviour differs from before this work.
- **A latent flap is fixed, not merely avoided.** Because the hint now follows every commit, a
  session that the *heartbeat* moved off the config'd model no longer risks being yanked back to it
  by a server that later serves both ids. That defect predates the pickers and was never reported.
- **`TODO.md`'s "[P1] Server / model switching" loses two of its three halves.** The picker UI and
  the endpoint switch close here; what remains of the entry is **local llama.cpp start/stop** (to be
  rebuilt over `heartbeat.Beat`, as ADR 0024 already recorded) and the switchable **model-profile**
  abstraction, ~~which stays deliberately global — `model-profile` is not per-model, and neither a
  rebind nor a switch touches it~~ *(superseded 2026-08-11 by
  [ADR 0044](0044-model-profiles-are-per-model-and-mostly-shipped.md): the profile is resolved per
  model from the `model-profiles:` pattern map and rides every rebind)*.
- **A switch costs one Interval of "connecting…" at worst and usually none** — the fold returns the
  first beat as a command rather than waiting for the next tick. Against a dead new server the same
  fold means offline is reported immediately, which is the cold-start rule doing its job.
- **CONTEXT.md's *Heartbeat* entry grows the switch** rather than gaining a new noun. "Rehome" is
  not introduced: the operation is `SwitchUpstream`, and Upstream, Heartbeat, Beat and Rebind carry
  the rest.

## Amendment (2026-08-13) — a switch carries the new entry's two token bounds

Decision 3 spells the spec `UpstreamSpec{Endpoint, APIKey}` and says a switch guesses nothing about
the new server because "its model, window, system-prompt template and validated Mechanism set are
facts only that server can report". Two of those stopped being discovery-only after this record was
written: [ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md) decision 3
put a `context-window:` pin on the `servers:` entry, and
[ADR 0046](0046-the-engine-bounds-every-reply-with-an-output-cap.md) decision 2 put a
`max-output-tokens:` cap beside it. A pin is not a guess and not an observation — it is the
operator's statement about that slot, and for a cloud endpoint that advertises no window it is the
only statement there is.

So `UpstreamSpec` grows `MaxContextTokens` and `MaxOutputTokens`, resolved WHOLE by the composition
root — the entry's own window pin over the top-level `context-window:` key, which survives every
move (`config.ResolveContextWindow`) — and applied by `SwitchUpstream` beside the endpoint and the
key. The engine stays wire-silent either way ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
it is handed two numbers, exactly as `RebindSpec` hands it the resolved window for a model change.

The rest of decision 3 stands: the model is still unbound, the system-prompt template and the
Mechanism set are still the follow-up `Rebind`'s to re-resolve, and an entry that pins neither bound
sends two zeroes — the same "unknown until the first beat" the unbound model is in, and never the
retired server's numbers.

What this closes: a `/server` move left the engine capping replies from the new server at the OLD
server's ceiling (or at one derived from the old server's window), and the entry's window pin
reached a moved session at no point at all — the rebind seconds later bound the new server's
observation over it. The pin is now held beside the top-level key at the composition root and is
what the rebinds after a move resolve against, so the footer's gauge, the Budget and the ceiling on
the wire all describe the server the session is actually on.

**Follow-up (2026-08-13) — the BIND resolves the same pin.** The sentence this amendment first
closed with said a first bind was unchanged and still read only the top-level key. That was the
other half of the same defect: a session that STARTS on a pinned entry — the determined startup, a
pre-bound session's first pick, a headless run — budgeted against the top-level window until its
first beat, because the entry's `context-window:` was never flattened onto `config.Options` the way
`max-output-tokens:` already was. So it is (`StartupContextWindow`), it rides the `ServerEntry` the
bind step takes, and `serverBinder.bind` resolves it over the top-level key through the same
`config.ResolveContextWindow` the move calls — into the `Config` the Agent is CONSTRUCTED from,
because a bind has no engine to push at yet and the pin has to bound the session's first Turn. The
latch the move writes is seeded from the same flattened field, so the first beat's rebind re-resolves
against the entry rather than binding that server's observation over its pin. A live `servers:` edit
re-derives the latch from the re-read list, matched back by name, exactly as the fan-out cap's own
pin is re-derived (`parallelAgentsCap.relist`) — the ADR 0037 rule that a key moved in the pane is in
force in the running session. Nothing about the ranks changed: the entry's pin outranks the top-level
key, and neither pinned is still 0.
