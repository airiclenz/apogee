---
title: ADR 0085 consequences
---

- **The config surface gains two keys:** `request-extra:` on a `servers:` entry and a root
  `server-stats:`. Neither is model-facing content a Reaction shapes, so neither is a Reaction and
  neither needs a bench gate (ADR 0076);
  what `request-extra:` sends is the user's own configuration, applied verbatim.
- **The headless protocol gains an `upstream_attempt` event kind** — additive under ADR 0075.
- **A new file under `~/.apogee`** (`server-stats.jsonl`), bounded by the startup trim, carrying
  no content beyond endpoint, model and timings.
- **The vocabulary stays apart:** *Server stats* are wall-clock timings and outcomes per Upstream
  attempt; ADR 0079's *Context cost* is the bytes apogee itself injects. Neither is money.
- **ADR 0024 §6's "binding follows observed reality" clause is superseded** by Decision 3, which
  records the behaviour shipped since b6e51496.
