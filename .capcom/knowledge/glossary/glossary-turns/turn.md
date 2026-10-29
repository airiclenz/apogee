---
title: "Turn: one iteration of the loop around a single primary Upstream call"
---

**Turn**:
One iteration of the loop — a single *primary* Upstream call and the work that follows it
(parse → dispatch tools → apply Reactions). Compaction's summarisation call is *internal*
to a Turn, not a Turn of its own. The unit of self-regulation and of bench measurement. The
Turn's lifecycle — its opening, its one permitted overflow fold, and its six exits (complete,
Exchange-complete, abandoned, cancelled, settled, step-capped; settled since 2026-09-27, ADR 0088: a
cancel during tool dispatch keeps the Turn and its finished tool results, while cancelled, which
rolls the Turn back, is left to a cancel while the reply streams) — is owned by the loop's
`turnLifecycle` module
(`internal/agent/turn.go`), the way the Exchange anchors on `ExchangeView`. A Turn also rides out a
**transient** Upstream fault itself — an in-band 5xx/429, a connection cut mid-reply, a stream
`stream-idle-timeout` (default **10m**, `0` = off; bytes of any kind are activity) cut for silence
— by re-sending the same request up to a per-Turn **re-stream budget** (`re-stream-budget`,
default **3**, `0` = never; the same at every depth, and shared by the compaction summary on a
counter of its own) with a hold-off that doubles before each re-send (1 s, 2 s, 4 s), silently when
it lands, so only the fault that finds the budget spent faults the Turn
(ADR 0082);
this is not the Reaction correction re-stream, which repairs a tool call within the same Turn.
