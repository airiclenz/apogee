---
title: Thinking effort (how hard a model thinks)
---

**Thinking effort** (how hard a model thinks):
The **emit-side dial** on the Model profile's thinking axis — `off | low |
medium | high`, widened by the levels real servers report to `minimal`, `xhigh` and `max` (and
`none`, which the OpenRouter dialect spells for `off`) — carried to the server in whichever
**wire dialect** that endpoint speaks: llama.cpp's `chat_template_kwargs`, OpenRouter's
`reasoning` object, or OpenAI/Groq's top-level `reasoning_effort`. **Absent means send nothing**:
the request stays byte-identical, and a model whose template reads no dial simply ignores a sent
one. Whether the dial exists at all is **detected passively**, from the discovery payloads the
heartbeat already fetches — a `/props` chat template that mentions the kwarg, or a `/v1/models`
`reasoning` object naming the model's own levels — never by probing; a `servers:` entry's
`effort-dialect:` key forces the dialect for a provider that advertises no such tell. What the
next request will actually carry reads in the **footer**, and `/effort` opens a **picker** of
the levels this model reports; on a model with no dial the footer segment and the menu row are
both absent. The dial resolves as its **own half of the thinking axis**: a profile entry that
spells only `effort:` keeps the channel style the layer below carries, so dialling a shipped
model's effort never drops its parsing
(ADR 0058).
A session can overlay the resolved profile with the **`/effort` override** — the human's "keep it
brief" intent, which rides above whatever profile each model switch resolves, is never persisted,
and is dropped only when a switch binds a model whose reported levels exclude it. Effort is
**configuration, not a Reaction** — it holds under Bypass. The deliberate contrast is the
Thinking channel: the channel says what the reasoning stream *is* and how
apogee parses it; effort says how much of it to ask for. See
ADR 0050 and
ADR 0060.
_Avoid_: "reasoning effort" as the term (that is a wire kwarg's spelling, not the concept),
"thinking budget" (llama.cpp's `--reasoning-budget` is a launch flag — launcher territory, not
this per-request dial), "/thinking" for the command (that verb opens the
Thinking pane; `/effort` is this dial's).
