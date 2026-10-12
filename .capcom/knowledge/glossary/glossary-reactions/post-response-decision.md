---
title: Post-response decision
---

**Post-response decision**:
**Retired** by ADR 0076
D12 — say **`Outcome`**, the one shape every seam Reaction returns. The three actions it named
survive as fields of it: **retry** (`Retry` — re-call the Upstream now, **in place**: the
correction rides `Inject` onto the in-flight request and re-streams **within the same Turn**, R1),
**intercept** (`Edited`, or any move of the working value's revision — alter the response before
the loop acts on it) and **defer** (`Defer` — a correction, or carried work, scheduled into the
*next* request). Corrections deliver by **retry-in-place**: the loop owns the stream and can reset
it (`StreamResetEvent`), so — unlike the proxy-era predecessor, which had already streamed the
response downstream and could only defer — a streaming response is **not** forced to defer.
_Avoid_: "interceptor" (intercept is one action an Outcome reports, not a thing that acts).
