---
title: Discovery's 5s timeout plus a 2-failure debounce paints a saturated local server offline and refuses the send
status: done
type: bug
priority: 1
labels: [slow-box]
aliases: [apogee-kws]
created: 2026-09-22T16:03:18Z
closed: 2026-09-23T06:01:33Z
reason: Closed
---

internal/provider/discovery.go:15 discoveryTimeout = 5 * time.Second bounds the /v1/models probe. internal/heartbeat/heartbeat.go:31 Interval = 10s and internal/tui/heartbeat.go:130 offlineFailureThreshold = 2 turn two consecutive probe timeouts into the footeri reading `offline`, and internal/tui/model.go:3317-3318 makes offline the state a send is REFUSED in.

A single-slot llama.cpp/Ollama server mid-prefill on a throttled laptop routinely takes longer than 5s to answer any HTTP request — the ordinary case for this product, not an edge case. The comment at heartbeat.go:125-127 already names the failure ("discovery's own timeout can elapse on a server that is merely saturated") and answers it with the debounce rather than with a longer probe. Neither constant is configurable.

Found by the slow-box deadline sweep, 2026-09-22, alongside the handoff docs/handoffs/2026-09-22 - 00 - ci-and-test-failures-triage.md. The two are one bug, not two.

## Acceptance

A saturated local server that answers slowly is not painted offline and does not refuse a send; the probe budget is sized for slow local hardware (or made configurable).

## Log

- 2026-09-23 Airic Lenz: Verified fixed in the tree at 533ce9af (slow-box-deadlines plan, item 1, commit 1ae96240).

  - internal/provider/discovery.go: discoveryTimeout 5s -> exported DiscoveryTimeout = 30 * time.Second, with the reasoning recorded (the budget is only ever SPENT on a failure, so a generous one is free; a saturated single-slot server answers late, not never). WithDiscoveryTimeout is the override.
  - internal/heartbeat/heartbeat.go: Interval is no longer a stated 10s but DERIVED — 'const Interval = 2 * provider.DiscoveryTimeout' (60s) — so 'a beat is strictly shorter than the interval' holds by construction.
  - internal/tui/heartbeat.go: offlineFailureThreshold stays 2 deliberately, but the debounce window widened with the probe budget to ~2 minutes; the doc comment states that this is the price of not calling a saturated server dead.
  - The send refusal on offline (internal/tui/model.go) is unchanged by design — what changed is that a slow-but-healthy server no longer reaches that state.
  - Docs re-worded off 'ten-second heartbeat' in docs/manual/configuration.md, internal/config/config.go and internal/config/defaults/config.yaml.

  Acceptance met by sizing rather than by a config key, which the acceptance allowed.
