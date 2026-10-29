---
title: "Daemon: the always-on Driver that holds durable Schedules"
---

**Daemon**:
The always-on **Driver** — `apogee daemon`, an in-repo subcommand (same binary as
the TUI) composing the same scheduler and runner libraries, but holding **durable** Schedules
read from a declarative `~/.apogee/daemon/schedules.yaml`: validated and atomically swapped on
change, an invalid file keeps the old set running. Each entry is a **trigger+action envelope**
— `on:` (v1: a cycle) + `run:` (a prompt, or since 2026-09-28 a recipe's workflow as
`run: workflow: {recipe, inputs}` — exactly one — with mode, workspace, an optional named server
binding and an optional per-request `model:`) — so a webhook trigger arrives later as a new `on:`
key, never a schema break, as workflow actions already did. A daemon Schedule binds to a `servers:`
entry by name, may select a model only
where that is a per-request act, and its Firings **never actuate a local model load**
(ADR 0055). Its Firings take the
**Firing** posture unchanged and save into the shared sessions store, so the TUI's `/sessions`
browser is the results window; the two processes share libraries and stores, never IPC. Runs
foreground under the OS's own supervisor (`apogee daemon install` generates the unit),
single-instanced by a lock file. See
ADR 0034.
_Avoid_: "server" (the engine is wire-silent and v1 has no listener; the future webhook
surface is this Driver's composition), "the scheduler" (that names the library both Drivers
share, not this Driver).
