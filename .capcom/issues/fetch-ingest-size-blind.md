---
title: Fetch rows hide how much was ingested, and the context gauge lags until the next response
status: done
type: feature
priority: 2
plan: 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan
created: 2026-11-01T10:19:33Z
closed: 2026-11-01T14:08:29Z
reason: Items 3-7 resolved by plan 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan (archived at 639c3bfc)
---

The rows for `web_fetch` (and `http_request`) show only the first line, `HTTP 200 OK`, so the user cannot tell how much each fetch put into the context. The context gauge doesn't move when a tool result is appended either. It only moves after the next upstream response. In the reported session, four fetches returned, the gauge stayed at 36k/160k, and the model then thought silently for ~15 min. The user had no way to tell whether a large ingest was the cause.

Wanted:

1. Each fetch row shows the size ingested, e.g. `HTTP 200 OK · 48 KB · ~12k tok`, and marks it when the response was truncated.
2. The context gauge updates as soon as a tool result is appended, from a local estimate, and the next upstream usage report then corrects it.

Code, traced 2026-11-01:

- Tool: `renderFetchResult` (`internal/tools/web_fetch.go:94`) writes `HTTP <status>`, `Content-Type`, a blank line, and then the body (HTML cleaned to text). It appends `[response truncated at 2097152 bytes]` when capped. The cap is `maxNetworkResponseBytes` = 2 MiB (`internal/tools/network.go:44`). The result carries no size field.
- Row: `internal/tui/toolregistry.go:493-499` sets `detail: firstLineDetail` (:1753) and has no stat hook.
- Gauge: `contextGauge` (`internal/tui/model.go:4579`). `ctxUsed` is set only from upstream `UsageEvent` totals (`internal/tui/fold.go:178-184`). The `~ctxEstimate` fallback is taken only at Turn start (`takeContextEstimate`, model.go:721) and ignored once a real reading exists.
- Helpers already in place: `context.TokenEstimator` (`internal/context/budget.go:210-252`, `EstimateTokens` plus `Calibrate` against reported prompt tokens) and `domain.Budget.EstimateTokens` (`internal/domain/budget.go:31`). `Agent.ContextEstimate` (`internal/agent/contextcost.go:81`) works only while the agent is idle.

Design note: the engine is wire-silent (ADR 0031), so the estimate must reach every Driver through the existing event stream, not as a TUI-only computation. The gauge must also show when it is reading an estimate rather than a measured value (the existing `~` prefix).

## Acceptance

- A `web_fetch` / `http_request` row shows the bytes ingested (and an approximate token count), and says so when the response was truncated at the cap.
- After a tool result is appended, the context gauge moves to an estimate marked as one (`~`) before the next response arrives, and the next usage report replaces it with the measured value.
- The estimate reaches headless/daemon Drivers through the event stream, not only the TUI. A test pins the gauge moving on a tool-result append.
