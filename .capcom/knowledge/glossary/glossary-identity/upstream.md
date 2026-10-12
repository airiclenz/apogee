---
title: Upstream
---

**Upstream**:
The LLM server that runs the model — Ollama, llama.cpp, LM Studio, vLLM, or any endpoint
speaking one of the two **Wires** (below) apogee has a codec for: the OpenAI chat-completions surface
(the default) or the Anthropic Messages API. Apogee reaches the Upstream directly through
its `internal/provider/` package; there is no intervening proxy. An `endpoint` host ending in `.local` that
the system resolver cannot find is resolved by apogee itself — one in-process IPv4 mDNS query
(`internal/mdns`), tried only after the system lookup fails — so a release build, which has no cgo
and so no route to the OS's mDNS responder, still reaches a server named by its mDNS name
(ADR 0091). A session is not married to one: the
Heartbeat's Rebind half moves it to another configured server
mid-session (`/server`), unbound until that server's first Beat says what it serves.
The Upstreams apogee knows are exactly the entries of config's **`servers:` list — the single
definition of what servers exist** (one entry = a `name`, an `endpoint`, an optional `api-key`, an
optional `model` discovery hint, an optional `wire`). The `name` is also the **host alias** the footer shows, so no
standalone `host-alias:` key exists. A session starts on the entry the `server:` key names — the
last one a `/server` switch chose, recorded automatically — and asks with the picker when that key
is unset or names an entry that is gone; a raw `--endpoint`/`APOGEE_ENDPOINT` override builds an
unlisted, unpersisted entry for one run
(ADR 0036).
_Avoid_: "the model server", "the backend".
