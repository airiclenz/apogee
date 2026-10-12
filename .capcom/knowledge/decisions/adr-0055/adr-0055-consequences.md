---
title: ADR 0055 consequences
---

- The daemon reads `config.yaml` once, at startup; `schedules.yaml` is its only live-reload
  surface (ADR 0034 decision 3). Rebinding servers after a config edit is a daemon restart —
  supervised, that is one `systemctl --user restart apogee-daemon`.
- Schedules-file validation needs config facts (entry names, entry class) and host facts
  (Auto eligibility); the validator takes them as injected inputs, so it stays a pure function.
- A local nightly schedule yields results only when the right model is serving — an accepted v1
  cost, stated in the seeded template's comments; the Gateway is the settled fix.
