---
title: ADR 0055 decision
---

**1. A schedule entry binds to a server by NAME.** The optional `run: server:` key names a
`servers:` entry from `config.yaml`; the schedule inherits everything that entry states — endpoint,
key source, context window, the per-model Mechanism set. A name no entry answers to is refused when
the schedules file is validated. Absent, the entry binds to the same startup default a fresh
session or headless run on this host gets.

**2. `run: model:` is a per-request selection and legal only where that is what it means.** On an
entry carrying no `llama-launcher:` it selects the model per request. On a launcher-fronted entry
it is refused at validation: there the key would be a request to actuate, and the daemon does not
actuate.

**3. The daemon never actuates the launcher.** No profile load, no startup restore, no unload —
a Firing sends to whatever is serving. Nothing (or the wrong model) serving means the Firing fails,
visibly, in its saved session record; the next tick behaves normally. The fix for "route my
schedule to the right local model" is the Gateway, by config, when it exists — not daemon-side
supply management.

**4. `schedules.yaml` never carries a raw endpoint or key.** Server identity lives in one file;
the schedules file only points.
