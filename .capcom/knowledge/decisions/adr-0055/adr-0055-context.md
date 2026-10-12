---
title: ADR 0055 context
---

ADR 0034
deferred one schema question to the daemon's build: "daemon v1 entries name models explicitly or
inherit the configured default" (decision 10) says *that* an entry can choose, not *how*. A
2026-08-22 grill settled it, driven by one force: "choose a model" means two different acts by
server class. On a multi-model endpoint — OpenRouter, a multi-model llama-server — a model name is
a **per-request selection**, free and instant. On a llama-launcher-fronted local server it means
**actuating a load or swap of the single slot**
(ADR 0029) — the
very slot the owner's interactive session may be using. A schedule that silently swaps the local
model out from under a live session is the contention failure ADR 0034 decision 9 already routed to
the Gateway (llama-launcher ADR-0013), not something the daemon may do as a side effect of a YAML
key.

The config already carries the registry that distinguishes the classes: the `servers:` list
(ADR 0028,
ADR 0044) — a plain entry records its
model choice in `model:`, a launcher-fronted one carries `llama-launcher:` and records its choice
in `launch-profile:`, and every entry owns its endpoint, key source, window and per-model wiring.
