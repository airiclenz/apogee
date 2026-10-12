---
title: Capture a deepseek tool-call stream with stubllm record to bisect the pre-tool-call text truncation
status: done
type: task
priority: 3
aliases: [apogee-l8s]
created: 2026-09-14T20:28:25Z
closed: 2026-10-10T07:12:13Z
reason: "Not apogee: the 2026-10-08 capture (115 completions via OpenRouter, deepseek-v4-flash) reproduced no truncation; all 26 text-before-tool-call turns ended on a sentence boundary and reached apogee's token events byte-identical, with 0 undecodable SSE events. Provider routing is the leading suspect for the 08-24/08-27 window. Dropped chunks are now counted and surfaced (plan 2026-09-14 - 03), so a harness-side cut would be visible. If cuts recur, open a new bead with a provider-pinned capture."
---

Session-mining open question 1 / item 23b: 27 of 37 coordinator turns on deepseek-v4-flash end mid-sentence before a tool call; bucket 6 saw 40/45 cut on 08-24 and 08-27, 0/44 on 08-15 and 09-08 (window-shaped). Scout finding: internal/provider/stream.go yields Content before folding tool_calls in the same chunk (shared-chunk hypothesis wrong); the only silent harness path is the strict-unmarshal drop of a malformed chunk (continue at the decode site). Plan 2026-09-14 - 03 makes dropped chunks counted and surfaced; this bead is the capture + bisect. No code fix until a capture exists.

## Log

- 2026-09-14 beads: spec: docs/reviews/archived/session-mining-2026-09-14.md
- 2026-10-08 beads: capture, no truncation reproduced. Chain: apogee headless --recipe audit internal/stubllm/ (plan mode, throwaway clone) -> stubllm record -> a scratch raw-byte tee -> https://openrouter.ai/api, model ~deepseek/deepseek-v4-flash-latest (served as deepseek/deepseek-v4-flash-0731), 12:14-13:04 CEST, 115 completions. OpenRouter routed across providers: Sail Research 107, OpenInference 5, Reka/StreamLake/Wafer 1 each. Raw SSE had 0 undecodable data events and 26 turns with text followed by tool calls, all ending on a sentence boundary. Every one of those 26 texts appears byte-identical in apogee's token events, so the harness passed the text intact. Two OpenInference streams ended on an in-band 502 ('model entered a repetition loop'). Read: the cut is not apogee's on this window. Provider routing is the leading suspect for the window shape (a different OpenRouter provider on 08-24/08-27). Next capture, if any: pin a provider via OpenRouter provider routing, or re-run when cuts recur. The capture stayed in the session scratchpad and was not committed.
