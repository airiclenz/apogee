---
title: "Parallel agents: the per-server cap on concurrently running sub-agents"
---

**Parallel agents**:
The per-server cap on how many sub-agents the top-level agent may run **concurrently**.
Resolved per `servers:` entry, pin-else-discover-else-floor: an explicit `parallel-agents: N`
is a **pin** discovery never overrides (the `context-window` idiom); absent, the cap is
discovered from the live server (`/props` `total_slots`); no signal means the entry's
**floor** — **4** for a keyed entry (`api-key`/`api-key-cmd`/`api-key-env`, or
`wire: anthropic`: a hosted server serves parallel requests as a matter of course), **1** —
strictly serial — for any other, so `parallel-agents: 1` is how a keyed server is held
serial (ADR 0039 D2 as amended 2026-09-19). It is **structural, not a Reaction** — it only executes calls
the model already made, so it is on under Bypass — but it also feeds the width the engine states
to Reactions (`LoopView.ParallelAgents()`: the width dispatch uses — the **Sub-agent server**'s
cap while a **Delegation target** is latched, else this cap, and **1** on a sub-agent), so an
engine-origin Reaction that synthesizes delegations batches `min(width, remaining)` of them per
Turn by the number dispatch will honour. No shipped Reaction does. More parallel agents
means a **smaller window each**: a llama.cpp `--parallel N` server splits its context into
N slots, and the reported window is the per-slot share. One reply has ONE width even when its
children do not share a **Delegation seat**: all-on-one-seat is bounded by that seat's cap, a
**mixed** reply by `min(session cap, target cap)` — small enough that neither server is
oversubscribed, and never a pool per seat. See
ADR 0039
(mixed width amended 2026-09-01 by
ADR 0069).
_Avoid_: "slots" (the server's own term for its side of the trade), "concurrency limit"
(names the bound, not the thing bounded), "fan-out width" (fan-out is the act; this is the
cap).
