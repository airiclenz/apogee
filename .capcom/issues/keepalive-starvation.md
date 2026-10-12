---
title: SSE keepalive comments keep a starved stream alive past stream-idle-timeout
status: done
type: feature
priority: 3
aliases: [apogee-keepalive-starvation]
created: 2026-09-23T20:22:37Z
closed: 2026-09-30T16:31:00Z
reason: "Layer-2 data says no. ~/.apogee/server-stats.jsonl, 287 attempts 2026-09-25..09-30: OpenRouter never starved (ttft-ttfb max 14 s, p90 0). One no-delta stall: local apollo-II, 882 s, ended in_band (09-25). Local prefill gaps that finished ok reach p90 170 s, max 1038 s, so any no-model-progress timeout would need to exceed ~17 min and would not have saved that case. Semantics stay per ADR 0082/0085; reopen if new data shows a recurring cost."
---

idleBody.Read (internal/provider/stream.go) resets the stream-idle-timeout on any bytes, so a provider sending ': OPENROUTER PROCESSING' comments indefinitely never trips the timeout. ADR 0082 deliberately counts any bytes as liveness. Owner call 2026-09-23 (grilling for plan 'docs/plans/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md'): leave semantics unchanged; design a separate no-model-progress timeout only if the Layer-2 ttfb/ttft data shows starvation is a real cost.
