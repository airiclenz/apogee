---
title: "Heartbeat (and its Beat): the continuous monitor of the Upstream"
---

**Heartbeat** (and its **Beat**):
The continuous **monitor** of the Upstream: on a cadence **derived from the
discovery probe's own budget** — `heartbeat.Interval` is twice `provider.DiscoveryTimeout`, about a
minute at today's values, which is what keeps a beat strictly shorter than the interval — apogee asks
the server which model it is serving, in which context window, and what else it advertises, and
reports the answer as one **Beat**. Deliberately the opposite pole of Probe
on every axis — a probe diagnoses **once, on demand**, and prints a report a *human* reads; the
heartbeat runs **unasked for the life of a session** and its output is *consumed* (the footer's
model and window, the gauge's denominator, the offline state, and the rebind that follows an
observed change). A Beat is **never an error**: an unreachable server sets `Reachable: false` and
says why, because that is a finding about the server, not a failure of the observation. It is also
what **starts** a session — the first beat fires immediately and completes discovery late, so
apogee paints before the server has answered and can be started **before** its server exists.
**Rebind** is the heartbeat's apply half: `Agent.Rebind` swaps *all* the per-model bindings
together — wire model id, System prompt template, context window, and the
Model profile with the tool roster its third axis spells — at a **quiescent boundary** (idle, or deferred to the
end of the running Exchange), never mid-Exchange. A configured
`context-window:` is a **pin** the heartbeat never overrides; a `servers:` entry's `model`
is a **trusted** id, never substituted: whenever it is set it is the active model verbatim, and an
advertised entry supplies only its context window (an id the server does not list runs as
configured, with no window known). Only an empty `model` falls back to the first model the server
advertises — and the resolved id **follows the binding**, restated on every commit, so discovery
keeps resolving the model the session actually runs rather than the one config named at launch.
The same two halves are what the human's own switches are made of, never a third path to bind.
**`/model`** picks among the models the beat reported and drives *Rebind by hand* — same seam, same
words, and the pick is recorded as the last observation so the next beat measures it as nothing
new. **`/server`** moves the whole Upstream: `Agent.SwitchUpstream` binds a
fresh provider client at the new endpoint and leaves the session with **no model bound**, the
per-server heartbeat Monitor is swapped whole behind the unchanged seam, and the new server's
**first Beat completes the move** through that same Rebind — one code path with the cold start. A
switch guesses nothing about the new server and destroys nothing about the session: the
conversation, Turn counters, mode, approvals and confinement all stand. What a switch *does* carry
is what the new entry **pins** about its own server — its `context-window:` and its
`max-output-tokens:` reply ceiling (and likewise its `response-reserve:`, `working-window:`,
`stream-idle-timeout:`, `request-extra:` and `price:`), all facts about the slot rather than about
the session, and a
pin is not a guess: the entry's window outranks the global `context-window:` key while the session
sits there, and an entry pinning neither leaves the window to that global key and the ceiling to the
engine's own derivation. And the launcher: moving onto an entry that names a llama-launcher config turns the
Launch profile verbs on for as long as the session sits there, and moving
onto an entry without one turns them off again. The servers it can reach
are the `servers:` list (plus the unlisted one an `--endpoint` override started the session on),
and a switch **onto a listed entry records the choice**: `server: <name>` is spliced into
`config.yaml` through the Settings surface's writer, so the next launch
starts where the last session ended, while a move onto an unlisted endpoint — a
Launch profile load, an override row — has no name and records nothing. With
`server:` unset (first boot) or naming an entry that is gone, the session starts **pre-bound** —
no engine constructed, `errMissingEndpoint` never risked — and the picker opens by itself to
complete the startup; an empty list opens the Settings surface with its "edit `config.yaml` and
restart" pointer instead. See
ADR 0024,
ADR 0028 and
ADR 0036.
_Avoid_: "health check" (it observes what is served, not merely that something answers), "poller"
(says nothing about what is observed, and the chain re-arms from the landed beat, not off a clock),
"probe" for this (the other noun, the other job), "reconnect" for a Rebind (the connection never
moved — the bindings did), "rehome" as a noun for a server switch (the operation is
`SwitchUpstream`; these nouns carry the rest).
