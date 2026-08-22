---
Status: accepted
---

# Daemon Schedules bind to named servers and never actuate a model load

## Context

[ADR 0034](0034-the-daemon-is-an-in-repo-subcommand-over-a-declarative-trigger-action-file.md)
deferred one schema question to the daemon's build: "daemon v1 entries name models explicitly or
inherit the configured default" (decision 10) says *that* an entry can choose, not *how*. A
2026-08-22 grill settled it, driven by one force: "choose a model" means two different acts by
server class. On a multi-model endpoint — OpenRouter, a multi-model llama-server — a model name is
a **per-request selection**, free and instant. On a llama-launcher-fronted local server it means
**actuating a load or swap of the single slot**
([ADR 0029](0029-the-launcher-actuates-local-servers-and-the-beat-completes-every-move.md)) — the
very slot the owner's interactive session may be using. A schedule that silently swaps the local
model out from under a live session is the contention failure ADR 0034 decision 9 already routed to
the Gateway (llama-launcher ADR-0013), not something the daemon may do as a side effect of a YAML
key.

The config already carries the registry that distinguishes the classes: the `servers:` list
([ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md),
[ADR 0044](0044-model-profiles-are-per-model-and-mostly-shipped.md)) — a plain entry records its
model choice in `model:`, a launcher-fronted one carries `llama-launcher:` and records its choice
in `launch-profile:`, and every entry owns its endpoint, key source, window and per-model wiring.

## Decision

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

## Considered options

- **Raw `endpoint:`/key fields in `schedules.yaml`** — rejected: it duplicates the key-source
  machinery, validation and migration story of the `servers:` list into a second hand-edited file,
  and puts secrets where none need to be.
- **Daemon-side actuation (load the named profile before firing)** — rejected: it swaps the single
  local slot out from under the interactive session, and it rebuilds the supply side of the slot
  broker inside the daemon — the exact split ADR 0034 decision 9 settled the other way.
- **Allow `model:` everywhere and let the server reject it** — rejected: single-model servers
  ignore or mangle an unknown model name inconsistently, so the misunderstanding would surface as
  a confusing Firing failure at 3am instead of a named validation error at edit time.

## Consequences

- The daemon reads `config.yaml` once, at startup; `schedules.yaml` is its only live-reload
  surface (ADR 0034 decision 3). Rebinding servers after a config edit is a daemon restart —
  supervised, that is one `systemctl --user restart apogee-daemon`.
- Schedules-file validation needs config facts (entry names, entry class) and host facts
  (Auto eligibility); the validator takes them as injected inputs, so it stays a pure function.
- A local nightly schedule yields results only when the right model is serving — an accepted v1
  cost, stated in the seeded template's comments; the Gateway is the settled fix.
