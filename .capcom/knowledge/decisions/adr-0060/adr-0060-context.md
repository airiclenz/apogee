---
title: ADR 0060 context
---

ADR 0050 put the
thinking-effort dial on the Model profile with **one** canonical wire mapping — llama.cpp's
`chat_template_kwargs` (`enable_thinking: false` / `reasoning_effort`) — and a `/effort`
command taking a level word. That shape was correct for the one family whose behaviour was
live-verified (Qwen3.8, 2026-08-15) and deliberately grew no dialect table until a second
family was actually sighted. Two things have since made the single mapping insufficient and
the command's silence unhelpful:

- **The wire is not one wire.** OpenRouter takes a `reasoning: {effort}` object; OpenAI proper
  (o-series, gpt-5) and Groq take a **top-level** `reasoning_effort` field; self-hosted
  vLLM/SGLang/TGI honour `chat_template_kwargs` but expose no `/props`. Against all three,
  apogee's kwargs map is ignored: the human dials `high`, the footer says nothing, and the
  request goes out at the model's own default. Effort was silently dropped.
- **Nothing told the human whether the dial existed at all.** `/effort` was offered on every
  model, answered identically on a model with no dial, and left no readout of what the next
  request would actually carry.

Meanwhile the vocabulary widened underfoot: `minimal` (OpenAI), `xhigh` (Qwen3.8's template
default), `max`, and OpenRouter's `none` all exist in the wild, and ADR 0050's four-value
load-time enum rejects them.

The information needed to close both gaps is **already fetched**: `provider.Discover` reads
`GET /props` and `GET /v1/models` on every heartbeat. llama.cpp's `/props` now returns the
`chat_template` Jinja text; OpenAI-shaped servers that expose a dial return a per-model
`reasoning` object on `/v1/models`. No new request is needed — which matters, because ADR 0050
explicitly rejected an `/apply-template` bind-time probe.

A grill on 2026-08-25 settled the shape; this record is that ratification. It **amends
ADR 0050** (decisions 2, 4 and 5) and is authoritative where the two differ; ADR 0050 stands
elsewhere. The effort sub-axis and its resolution are untouched
(ADR 0058).
