---
title: Anthropic in-band api_error is not Server-fallback eligible
status: done
type: bug
priority: 2
plan: 2026-11-02 - 01 - fallback-followups-plan
created: 2026-11-02T14:12:35Z
closed: 2026-11-02T16:20:09Z
reason: Resolved by plan 2026-11-02 - 01 - fallback-followups-plan item 1 (archived at 81fc817e)
---

An Anthropic in-band `api_error` (that wire's mid-stream 500) is pinned as not FallbackEligible: it classifies as `FaultInBand` with code 0 and is not `overloaded_error`, so `isTransientFault` (internal/provider/fault.go:73) and `UpstreamFault.FallbackEligible` (internal/provider/fault.go:137) read it as ineligible; the test pins that verdict (internal/provider/stream_test.go:1952). This may fall short of the ratified "HTTP 5xx" trigger (ADR 0105). Consider mapping `api_error` to 500 in `classify` or in `FallbackEligible`. Deferred from plan 2026-11-02 - 00 - midrun-model-fallback-plan.

## Why

A Messages API server failing mid-stream with api_error abandons the Turn instead of falling over to the entry's fallback.

## Acceptance

- An Anthropic in-band api_error is FallbackEligible (or ADR 0105 records why it is not), with the stream_test.go case updated to match
