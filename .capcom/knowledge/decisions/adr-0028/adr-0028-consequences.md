---
title: ADR 0028 consequences
---

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
  ADR 0044: the profile is resolved per
  model from the `model-profiles:` pattern map and rides every rebind)*.
- **A switch costs one Interval of "connecting…" at worst and usually none** — the fold returns the
  first beat as a command rather than waiting for the next tick. Against a dead new server the same
  fold means offline is reported immediately, which is the cold-start rule doing its job.
- **CONTEXT.md's *Heartbeat* entry grows the switch** rather than gaining a new noun. "Rehome" is
  not introduced: the operation is `SwitchUpstream`, and Upstream, Heartbeat, Beat and Rebind carry
  the rest.
