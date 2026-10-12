---
title: "Thinking channel (a model's private reasoning): the reasoning stream a model emits apart from its answer"
---

**Thinking channel** (a model's private reasoning):
The reasoning stream a model emits separately from its user-facing answer — either **delimited**
inline (`<think>…</think>`), **harmony** (gpt-oss's `<|channel|>analysis…<|message|>…`), or split
out by the Upstream into its own wire field — spelled `reasoning_content` by llama.cpp, vLLM and
LM Studio, and `reasoning` by Ollama and OpenRouter. Apogee decodes both spellings as the one
channel, preferring `reasoning_content` wherever it is non-empty. Apogee **strips** inline channels from
visible content and preserves them as reasoning in history; it never sends that readable text back
Upstream. The one exception is the anthropic wire's **signed** reasoning blocks (`thinking` with its
`signature`, `redacted_thinking` with its `data`): Apogee keeps them opaque and verbatim on the
committed assistant message, each with its place among the reply's text and tool calls, so they
survive a session save/resume, and replays them — and the reply's text blocks — in the order and
places the reply had them, only on the anthropic wire and only to the model that produced them —
anywhere else they are dropped silently. On the preserved-thinking models (Fable 5.1, Mythos 5.1,
Opus 5.5, Sonnet 5.5) a further guard applies: a reply's blocks go back only while the prefix
before that reply (`system`, `tools`, every earlier message) digests as it did when the reply was
produced, so a changed system prompt, a capped or noted earlier message, or a resume that saved
without advice drops them client-side
(ADR 0092).
Harmony is a *content-stripping* concern only — a harmony model's tool calls arrive **native**
(the Upstream parses harmony server-side), so there is no harmony tool-call text parser.
_Avoid_: "chain-of-thought" (a prompting technique, not the wire channel), "commentary" (that is
one harmony sub-channel, not the whole concept).
