---
title: "ADR 0083 part 4: The bound entry stays six host-side assembly sites — `Options.StartupEntry` only (2026-09-15, review 09-14 #7)"
---

**Proposed.** "The Upstream this session is on" as one value in `internal/config` — the entry plus
its ranked bindings — computed by one function and handed to the engine, the heartbeat Monitor,
the settings holder and the Firing composer, replacing `ServerEntry`, the flattened `Options`
fields, `upstreamBinding`, the `liveSettings.entry*` fields and the engine's copy, and the ranking
ladder re-run at each arrival site.

**Ratified "IN, reduced"** (owner, 2026-09-15; plan `2026-09-15 - 01`, item 6): `Options.StartupEntry`
replaced the fourteen flattened fields, and that is the whole of it — the three `Resolve*`
ladders are not changed and `liveSettings.entry*` is untouched. The residue the 2026-09-16 review
listed under *Adjudicated earlier* (the per-entry ranking ladder still called at every arrival —
`serverBinder.bind`, `sessionMover.move`, `firingConfig`, `rebindInputs`) and the 2026-09-20 review
measured (six assembly sites in `cmd/apogee`; `liveSettings.firingBinding` in
`cmd/apogee/wire_settings.go` hand-zeroing the parallel cap, the key-source fields, the
description and the forced `effort-dialect:` for an in-session Firing) is accepted as it stands.
The reduction stands because the arrival sites do not agree on purpose: the boot binding, a
`/server` move, a Firing's composed entry and a rebind each rank the top-level keys against a
different observed state (what the beat saw, what the session already resolved), and one function
that "turns the entry into engine-facing facts" would have to take that state as a parameter and
grow the same branches inside. `firingBinding`'s comment names the road not taken — the
Driver-parity alternative of ADR 0031
— as "a decision not taken here", and that is the standing form of it. The engine-side half of the
same review's candidate 6 ("one server binding with one zero rule — engine side") is new and is
not covered by this section.

**Reopens on.** A fifth arrival site that re-spells the ladder, or an entry key added that
needs an edit at every one of the six — the "adding an entry key: 12 edits → 3" leverage the
candidate claimed, once it is measured against a key that actually arrives.

> **Amended 2026-09-30 (review 2026-09-30 #6; plan `2026-09-30 - 02`, *Ratified design calls*).**
> The second trigger fired: `request-extra:` (commit 5e41fe15, 2026-09-24) was an entry key that
> arrived, and it edited seven non-test `cmd/apogee` files (22 files in all), five of them this
> section's assembly sites. **Reopened, narrowly** (owner, 2026-09-30): an entry's *dial facts* —
> endpoint, key, wire, model, request-extra and the forced `effort-dialect:` — are one host-side
> value, `upstreamBinding`, which depend on no observed state. That value builds the Client and the
> heartbeat Monitor, and it fills the four dial fields (endpoint, key, wire, request-extra) of the
> engine's `Config`, `UpstreamSpec` and `DelegationTarget`, and of `probe.Inputs` (which carries
> no request-extra); `DelegationTarget.EffortDialect` comes from the beat. A new Client-side dial
> key is then one host edit — the binding's field and `cmd/apogee/dial.go`. `Config.EffortDialect`
> stays hand-ranked in `firingConfig` and `probeContextConfig`, and the four-argument
> `firingInputs.beat` seam stands, so a key that reaches those needs edits there too. The six
> arrival sites and the three `Resolve*` ranking ladders stand: this section's reason — each site
> ranks against a different observed state — does not reach the dial facts, and it still holds for
> the ranking. One `internal/config` function turning the entry into engine-facing facts and
> replacing the arrival-site ladders — the *Proposed* design above — stays denied; this is a
> narrower reopen, so it is a dated amendment and no superseding ADR.
