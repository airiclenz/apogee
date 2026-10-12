---
title: ADR 0028 context
---

ADR 0024 built the
two layers a user-initiated switch needs and deliberately built no UI over them. Its decision 8 is
explicit: every beat carries the server's whole `/v1/models` offering into TUI state and "nothing
renders it — that is the deliberate data layer for the future `/model` picker and `/server`
switch". Its consequences say the same from the other end: "the picker work is now cheap and
unblocked… `Rebind` deliberately never touches `Endpoint`, and `errMissingEndpoint` stands."

So apogee could follow a model the *server* changed, but a human could not change one. Switching
model meant restarting the server from a launcher beside apogee and waiting ten seconds for the
heartbeat to notice *(Amended 2026-09-30: the cadence is now `heartbeat.Interval`, twice
`provider.DiscoveryTimeout` — 60 s — in `internal/heartbeat/heartbeat.go`)*; switching **server** meant quitting apogee, editing `endpoint:` in
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
layer), and it adds one member to ADR 0011's
idle-only engine-call class.
