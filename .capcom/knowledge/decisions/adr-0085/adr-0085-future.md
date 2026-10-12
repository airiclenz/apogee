---
title: "ADR 0085 future work: fallback pools"
---

Explicit **fallback pools** (the brainstorm's Layer 3) remain acceptable and are not built here:
a server entry would name `fallback:` entries, and when the per-Turn re-stream budget of
ADR 0082 is
exhausted the Turn would continue on the next one. It needs its own ADR, written against three
settled decisions: ADR 0028
binds a session to one server and model, so a pool must be limited to entries serving an
equivalent model or define mid-Turn rehoming explicitly (context window, effort dialect,
prompt-cache loss); ADR 0047 rejects a
fallback chain across key sources — not across servers, which is no conflict but must be cited;
and ADR 0082's budget is the trigger. A pool would sit behind the `provider.Responder` seam, so
the engine's wire-silence (ADR 0031) is untouched. The measurement recorded here is the evidence
that decides whether it is worth building: pools pay off when stalls, not slow providers, are the
cost.
