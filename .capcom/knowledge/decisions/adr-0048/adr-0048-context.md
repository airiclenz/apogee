---
title: ADR 0048 context
---

The server a session starts on is remembered — every `/server` switch splice-writes `server: <name>`
so the next launch comes up where the last one ended
(ADR 0036
decision 2). The **model** running on that server was not. On the machines this project targets that
is the more expensive half of the pair: a workstation with one GPU serves one small model at a time,
the human picks it deliberately, and every restart asked them to pick it again — a `/model` overlay on
a plain multi-model server, a Launch profile load and a thirty-second health wait on a
launcher-fronted one. The choice was already expressed at the keyboard; nothing in the system wrote
it down.

The two server classes keep that choice in different vocabularies, which is why one key could not
serve both. A **plain** multi-model server (llama.cpp with several models, LM Studio, a remote
provider) holds many models at once and the choice is a **wire model id** — and the entry already has
a home for it: `model:` is read at start-up and bound, as a HINT rather than a pin
(ADR 0024 decision 6),
which is exactly the strength a remembered choice wants. A **launcher-fronted** entry (one carrying
`llama-launcher:`) holds one model at a time and the choice is a **Launch profile** — a name in
llama-launcher's own config, a launch-side recipe rather than a wire id, and on that class `model:`
is a deliberately empty discovery hint. Nothing in `servers:` could name a profile.

The tempting shortcut — write the choice into the launcher's config, where the profiles already live
— is closed by ADR 0029
decision 4: the launcher's config is the single profile store and apogee never defines, writes or
caches a profile in it. That rule is not merely a layering preference. The launcher's YAML is a
**library of presets** a human curates and copies between machines; which one *this* apogee session
happened to load last is **session state** belonging to one client of several. Merging them makes one
file mean two things, and the first casualty is the human's own recipe list.
