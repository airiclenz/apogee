---
title: "Wire: the request/response protocol family a server speaks"
---

**Wire**:
The request/response protocol family a server speaks — the codec the provider Client encodes a
request in and decodes the stream with. Two exist: `openai`, the chat-completions wire
(`/v1/chat/completions`, `Authorization: Bearer`, the OpenAI stream) every Upstream spoke before
the key existed and the default; and `anthropic`, the Messages wire (`/v1/messages`, `x-api-key`,
Anthropic's event stream, `GET /v1/models` for discovery with no `/props` behind it). A wire is
chosen per **`servers:` entry** by its `wire:` key, is a property of the endpoint rather than of
the model served there, and implies the effort dialect on the anthropic
side (`output_config.effort`), so `wire: anthropic` and `effort-dialect:` do not compose. It is
one codec inside the one `provider.Client`, never a second client and never a second engine path —
the engine stays wire-silent
(ADR 0078).
_Avoid_: "dialect" for this (the **wire dialect** / `effort-dialect:` is how the thinking-effort
dial is spelled ON a wire, a narrower thing — ADR 0060); "provider" as the family name (the
`internal/provider/` package is the client that speaks every wire; `openai` is a wire, not a vendor).
