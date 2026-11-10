---
title: A local server still loading its model fails the turn after about 0.6 s of 503s
status: open
type: bug
priority: 2
labels: [slow-box]
created: 2026-11-10T19:35:12Z
---

Source: docs/reviews/code-audit-2026-11-10.md ("Provider gives up on a loading local server after about 0.6 s"). Verified 2026-11-10 at a5c4f2db.

**Where:** `internal/provider/client.go:73-74` (`defaultMaxRetries = 2`, `defaultRetryBaseDelay = 200ms`), `send` at `:577-594`; `statusDelta` in `internal/provider/stream.go:351-375`; the re-stream branch in `internal/agent/loop.go:633`.

**What:** `send` retries a 5xx with no `Retry-After` twice (200 ms, 400 ms) and returns the third response. `statusDelta` turns it into a `DeltaError` with `Retryable:false` ("already retried"), so the loop's re-stream budget is never spent on it. The fallback resolver steps in only with a configured `fallback:` list (not the default). llama-server answers every request with `503 "Loading model"` and no `Retry-After` for the whole load.

**Why it matters:** a prompt sent just after starting llama-server, or swapping its model, fails with "upstream HTTP 503" after ~0.6 s. On a Pi-class box a load takes tens of seconds to minutes. The server is healthy and only slow: a slow-box bug. llama-swap and Ollama hold the request during a load and are not affected.

**Suggested fix:** give a 503 without `Retry-After` its own patience inside `send`: longer backoff (e.g. 1 s doubling to a few seconds) bounded by a generous backstop sized for a cold model load on slow hardware (minutes), cancellable via ctx, like the existing `maxRetryAfter` cap. Marking the final 503 `Retryable` alone adds only ~7 s via the loop's 1+2+4 s ladder — not enough. Keep the short ladder for other 5xx so a real 500 surfaces quickly.

## Acceptance

- A scripted upstream that answers 503 with no Retry-After for longer than the old 0.6 s window and then 200 completes the turn with no ErrorEvent.
- A 503 that outlasts the backstop still surfaces as a fault, and cancelling during the wait ends it at once.
- A 500 (or another non-503 5xx) keeps the existing short retry schedule.
