---
title: "Delegation target: the latched spec a sub-agent spawn reads to build its own Upstream"
---

**Delegation target**:
The engine-side latched spec a sub-agent spawn reads to build its own Upstream: the
Sub-agent server's endpoint and key plus its *observed* facts — model, per-slot window,
Parallel-agents cap, model profile, **effort dialect** — kept fresh by a second heartbeat monitor
and pinned by the entry's `model:` / `context-window:` / `effort-dialect:` where set. A fact the
target names is what the routed child gets; a fact it names *not at all* falls back to the
parent's (the dialect, like the window). Mutex-read at spawn (never idle-gated:
beats land mid-Exchange; each spawn snapshots). An **unusable** target (no beat yet, server
down, no model) is not an error: the spawn **falls back** to the parent's Upstream *and*
parent posture, with one notice per routing state change. Ratified 2026-08-11 (ADR 0045).
_Avoid_: "child upstream" (the target outlives any one child), "sub-agent endpoint" (it
carries far more than an address).
