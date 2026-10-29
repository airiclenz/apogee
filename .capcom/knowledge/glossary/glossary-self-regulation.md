---
title: Glossary section for the retired self-regulation layer and the empty-response rule that outlived it
aliases: [self-regulation]
---

**Retired** by ADR 0076
D12, together with the **Mechanism** lab layer it regulated. It named the per-Session runtime
machinery that kept a Mechanism from hurting the model — the operational half of the hard
constraint: **Effectiveness tracking** (per-Mechanism bookkeeping that recorded each time a
Mechanism *acted* and judged the **next** Turn for it three ways — **productive**, **harmful** or
**neutral**, R3), **Adaptive Suppression** (the per-Mechanism withdrawal rule, striking on
consecutive harmful Turns) and the **Turn Budget** (the global one). None of it ships. The
Floor guards need no throttle — each changes only what the model sees after its own
failure, or shapes the request without steering it, which is what makes them engine behaviour at
all — and a bench-armed **Reaction** is measured by the bench rather than policed at runtime.

One rule outlived the machinery and never belonged to it: an **empty final response** is a
**fault**, not a signal. A reply with no visible text and no tool calls is an upstream failure — an
aggregator's in-band error on an HTTP 200, a stream that ended before its first token — so the Turn
faults visibly instead of committing a blank assistant message — once the **empty-response
recovery** Floor guard's retry is spent, or at once when `empty-response-recovery: false` switches
it off. It indicts the Upstream.
See Retired terms.
