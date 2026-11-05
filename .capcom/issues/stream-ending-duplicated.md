---
title: "One reply accumulator ends every stream (architecture review 2026-10-14 #12)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#12), re-verified 2026-11-05 at f7ec72b6. The OpenAI and Anthropic codecs (parseSSE in internal/provider/wire_openai.go and wire_anthropic_stream.go) still each write their own text cap, read-error fault, tool-cap error, malformed count, pending finish / usage and EOF epilogue; only openToolCalls (stream.go) is shared. The review proposes an unexported reply accumulator inside provider that owns caps, pending state, tool calls and every terminal path, leaving the codecs only their dialect, so the two wires cannot drift on EOF again. Its symptom (an OpenAI stream ending without [DONE] dropping a received length finish and its usage) was fixed by item 7 of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; the shape stays inside the one Client (ADR 0078), keeps stop-reason mapping in the codec (ADR 0083 §6) and the idle cut at the body (ADR 0082).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
