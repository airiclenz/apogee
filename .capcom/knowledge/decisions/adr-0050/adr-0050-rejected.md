---
title: ADR 0050 considered and rejected
---

- **Two keys (`enabled:` + `effort:`)**: mirrors the two kwargs but makes `enabled: false,
  effort: high` spellable, which then needs a rule.
- **A top-level `thinking-effort:` profile axis**: the channel and its dial are one concept in
  two directions; the profile already drives both directions at the seams.
- **A per-family dialect field or table now**: config surface for families whose kwarg
  behaviour nobody has verified; decision 2's per-sighting rule instead.
- **Raw `chat-template-kwargs:` maps in the profile**: maximum generality, but exposes wire
  detail in config and bypasses validation; revisit only if a verified family cannot be
  expressed semantically.
- **Probing `/apply-template` at bind time**: llama.cpp-only, a request per bind, and needs a
  fallback story for servers without the endpoint.
- **`/thinking` as the command name**: collides with the deferred display feature's natural
  name.
- **Override cleared on model switch**: treats a human intent as a model fact and costs
  re-typing it after every switch; a knobless model ignores the kwarg anyway.
- **`logit_bias` on `</think>`** (graduated per-token hack) and **llama.cpp
  `--reasoning-budget`** (a launch flag, 0/-1 only — llama-launcher territory): denied.
