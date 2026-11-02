---
title: Any /v1/models reasoning object switches the effort dialect, whatever its shape
status: open
type: bug
priority: 3
created: 2026-11-02T06:57:58Z
---

`modelsResponse.effortSupport` (internal/provider/discovery.go) treats the mere presence of a per-model `reasoning` object as OpenRouter's tell and picks `EffortDialectReasoning`. Splash 1.5 now advertises its own shape there:

```json
"reasoning": {"thinks": true, "efforts": ["low","medium","xhigh"], "default": "medium"}
```

So apogee sends `reasoning: {effort: …}` to Splash, which reads only top-level `reasoning_effort` (and `chat_template_kwargs`) and silently ignores the object: an effort chosen in apogee has no effect (checked 2026-11-02: `reasoning:{effort:"high"}` and `reasoning:{enabled:false}` behave as the default). apogee also misses Splash's `efforts` list and `default`, as it decodes only `supported_efforts` / `default_effort`, and offers the canonical levels instead. Workaround: `effort-dialect:` on the entry. Splash is also being changed to accept the object.

## Why

A field name shared across servers is not proof of OpenRouter's request dialect; a wrong guess drops the user's effort without any error.

## Acceptance

- The OpenRouter dialect is picked only on an OpenRouter-shaped object (e.g. supported_efforts / default_effort present), or another rule documented in the ADR 0060 amendment
- A Splash-shaped object resolves to a dialect Splash honours, with its efforts and default read
- Discovery tests cover both shapes
