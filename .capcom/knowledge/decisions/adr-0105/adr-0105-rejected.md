---
title: ADR 0105 considered options
---

- **Count failures as the trigger** (N consecutive errors, or M minutes of errors). ADR 0082
  already owns retrying a transient fault. A counter on top would only re-tune that budget. The
  grace window asks the question that matters instead: is the primary back?
- **Switch at the first fault, with no grace.** A local server that restarts in thirty seconds
  would cost the session its model and its prompt cache for nothing.
- **Per-Turn fallback that returns to the primary next Turn.** The model would flap between
  Turns, losing the cache and rebinding the window each time. Under a primary that is down for an
  hour, every Turn would pay the grace again.
- **Switch back automatically when the primary recovers.** That changes the session's model
  under it at a moment nobody chose. `/server` is the door back.
- **Limit `fallback:` to entries serving an equivalent model** (the first option in
  `adr-0085-future`). The case that asked for it is a local model with a cloud fallback. Defining
  the rehome explicitly (window by ADR 0018's fold, model by one beat) covers that case.
- **One top-level `fallback:` list.** The fallback is a fact about an entry. A routed Sub-agents
  seat needs its own list, and a top-level list would make the session's list apply to it.
- **Chain through each fallback's own `fallback:`.** Cycles and unbounded walks, for a case one
  ordered list already covers.
- **Trigger on any failed request** (a 4xx other than 429, an overflow, an empty or capped reply).
  Another server would not fix a bad request or a full window, and the loop already answers
  those. A fallover there would hide the defect behind a model change.
- **Record the fallback as the `server:` startup choice.** The next launch would start on the
  backup because of an outage the owner may never have seen. Resume starts on the configured
  primary.
- **Ship it as a Reaction, bench-gated and off by default.** It acts only after the primary has
  failed and the Turn would be abandoned. There is no healthy run for it to make worse, so the
  ADR 0006 bench gate measures nothing.
- **Let the engine read the `servers:` list.** That closes ADR 0031's wire-silent door. The engine
  gets a resolver and specs instead.
