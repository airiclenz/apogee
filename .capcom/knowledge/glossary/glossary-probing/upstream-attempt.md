---
title: "Upstream attempt: one HTTP request to an Upstream and the measurement taken of it"
---

**Upstream attempt**:
One HTTP request apogee sends to an Upstream — every one, including the
pre-first-byte retries inside a single stream call — and the **measurement** the provider Client
takes of it: the attempt index, the server's request id when it returns one, **`ttfb`** (send →
first body byte; keepalive comments count), **`ttft`** (send → first model delta of any kind:
reasoning, content or tool call), `last` (send → last model delta), the duration, and an outcome
from a closed vocabulary — `ok`, the fault class, or `cancelled`. It travels the ordinary event
path: a `DeltaAttempt` on the Client's Delta stream, a `domain.UpstreamAttemptEvent` from the loop,
and whatever a Driver subscribes — the TUI's store, headless's `upstream_attempt` line. Attempts
at every delegation depth and in compaction count. An attempt is a *measurement*, never a routing
decision: apogee measures Upstreams and passes routing hints through (a `servers:` entry's opaque
`request-extra:` body overlay) but never routes
(ADR 0085).
_Avoid_: "request" alone (a Turn's request may take several attempts), "retry" for the first
attempt, "TTFT" for the first byte (a keepalive is a byte, not a token — the two clocks are kept
apart on purpose).
