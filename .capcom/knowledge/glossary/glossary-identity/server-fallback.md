---
title: "Server fallback: the entry a failed seat moves to mid-Turn"
---

**Server fallback**:
An Upstream a `servers:` entry names in its ordered `fallback:` list, and the move onto it when
the entry's server fails mid-Turn. The trigger is a fault that would abandon the Turn today: a
transport failure, a timeout or idle cut, HTTP 5xx, 429 or 529, or a cut stream after the
re-stream budget is spent. apogee then probes the primary for up to the entry's `fallback-after:`
(default `2m`). If the primary recovers, the request is resent there. Otherwise the **seat** (the
session's own Upstream, or the routed Sub-agents seat) rehomes to the first live fallback. One beat
binds that server's model, and the failed request is resent, so the Turn continues. The move is
one-way and per seat: every agent on the seat moves, one `upstream_switched` Event reports it, and
only `/server` or `/sub-agents-server` moves back. A fallback's own `fallback:` list is ignored, and
with every fallback down the Turn is abandoned as before. An entry without `fallback:` behaves
exactly as before. Not to be confused with a **Delegation seat** falling back: that one sends a
routed spawn to the session's server because the Sub-agent server is unusable *at spawn*, and the
result says so in one note line (`SeatFallbackNote`). A Server fallback moves a running seat onto
an entry its config names. Ratified 2026-11-02 (ADR 0105).
_Avoid_: "failover pool", "routing" (a fallover follows a list, after a failure; apogee does not
route, ADR 0085), "retry" (retrying the same server is ADR 0082's budget).
