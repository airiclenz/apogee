---
title: ADR 0078 context
---

apogee has spoken exactly one wire since the proxy was shelved: OpenAI chat-completions —
`POST /v1/chat/completions`, `Authorization: Bearer`, the OpenAI SSE stream, `GET /v1/models` and
llama.cpp's `/props` for discovery (`internal/provider/wire.go`, `client.go`, `discovery.go`). Every
Upstream apogee has been run against — llama.cpp, Ollama, LM Studio, vLLM, OpenRouter, OpenAI
proper, Groq — reads that shape, which is why the `servers:` list (ADR 0036)
never had to say *which* protocol an entry speaks.

The 2026-09-06 contender gap assessment (gap 4, bead `apogee-6fp`, accepted by the owner
2026-09-07 as wave 2) named the cost of that: the Anthropic Messages API — the wire Claude is
served on, and one Ollama has also spoken since January 2026 — is unreachable, while the
contenders speak it natively (pi speaks four wire formats; Crush and goose ship typed providers per
vendor). A user with an Anthropic key, or a Messages-speaking proxy, has no entry to write.

Three facts bounded the shape:

- **The engine is wire-silent** (ADR 0031).
  `internal/agent` builds a `provider.Request` and reads a `provider.Response`; it neither knows
  nor may learn what bytes carried them. A second wire is therefore a provider-package concern
  from end to end, and the Bypass invariant needs no bench gate — no model-facing text changes.
- **The wire is a property of the endpoint, not the model.** The same reasoning that put
  `effort-dialect:` on the `servers:` entry rather than in a model profile
  (ADR 0060 decision 3, after
  ADR 0050 decision 2)
  applies with more force here: a URL speaks one protocol whatever it serves.
- **The Messages wire already spells the effort dial.** ADR 0060's three dialects exist because the
  chat-completions wire has three ways to say "think harder"; the Messages API has one,
  `output_config.effort`, so a dialect word on an anthropic entry names a shape the request never
  travels in.
