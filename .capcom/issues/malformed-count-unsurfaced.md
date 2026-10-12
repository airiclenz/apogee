---
title: Malformed-chunk count is invisible on a successful reply
status: done
type: bug
priority: 3
plan: 2026-10-08 - 01 - firing-window-and-silent-drops-plan
aliases: [apogee-malformed-count-unsurfaced]
created: 2026-10-08T10:12:54Z
closed: 2026-10-08T14:51:31Z
reason: "Resolved by plan '2026-10-08 - 01 - firing-window-and-silent-drops-plan' (items 4, 5, 7; archived at ec0827f2): MalformedChunksEvent note shown and recorded by every Driver; stubllm record warns on undecodable stream events."
---

Plan 2026-09-14 - 03 item 3 made the OpenAI and Anthropic stream decoders count chunks that fail strict json.Unmarshal and carry the count on Delta.MalformedChunks (internal/provider/wire_openai.go:297, wire_anthropic_stream.go:92). Only the fault paths render it (malformedChunksNote, malformedOnlyErrFmt). On a DeltaDone that carries text or tool calls, nothing outside internal/provider reads the field: no event, no session-log entry, no notice. A reply that silently lost text to a dropped chunk therefore looks clean. That is the apogee-l8s shape: text cut mid-sentence before a tool call, with the turn otherwise succeeding. Related: the recorder has the same silent drop (internal/stubllm/record.go:455, continue on unmarshal failure), so a stubllm record fixture cannot show a malformed chunk either. Found 2026-10-08 while preparing the l8s capture. It is a gap left by the plan's acceptance (which pinned only the count on DeltaDone), not a regression.
