---
title: ADR 0045 deferred (dated 2026-08-11, not denied)
---

- **Model-chosen routing** — a `sub_agent` tool parameter letting the parent model pick a
  server/model per delegation. Needs this static routing to exist first, plus prompt-surface
  bench evidence. **No longer deferred — ratified 2026-09-01 by
  ADR 0069**, in the narrower shape this
  item's "server/model" wording anticipated: the choice is between two **Delegation seats** (the
  session's own server, or the Sub-agent server) through a depth-0 `run_on` parameter gated by a
  root `sub-agents-choice:` key, never a `servers:` entry name and never a model. Decisions 3-7
  below are what the `sub-agents-server` seat still means, unchanged; §4's fallback is what an
  unusable ask still degrades to, with one note line added to the result that asked.
- **Launcher actuation for the Sub-agent server** — llama-launcher auto-loading the grunt
  model when delegations start (ADR 0029's latch, second server). Works today when the owner
  pre-loads; actuation is its own grill.
