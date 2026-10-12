---
title: Tool-call-only replies stamp ttft at flush, not first fragment
status: done
type: bug
priority: 3
aliases: [apogee-tool-call-ttft-at-flush]
created: 2026-09-24T04:48:54Z
closed: 2026-09-24T05:59:37Z
reason: Fixed in a87172b5; race run pending on a TSan-capable host
---

A reply that carries only tool calls gets its attempt ttft when the codec flushes the assembled DeltaToolCall at stream end, not at the first tool-call fragment, so ttft ~= last for such attempts and the tok/s summary (output tokens / (last - ttft)) gets a near-zero denominator. The recorder marks on DeltaToolCall (internal/provider/attempt.go:264); the codecs buffer fragments inside parseSSE (internal/provider/wire_openai.go:216, internal/provider/wire_anthropic_stream.go:65), so fixing it needs a parseSSE signature change (e.g. a first-fragment callback). Item 8's serverstats summary must tolerate last-ttft ~ 0 meanwhile. Deferred from plan docs/plans/archived/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md items 5/8.
