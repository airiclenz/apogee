---
title: ADR 0045 consequences
---

- `ServerEntry` grows `sub-agents:`, `bypass:`, `mechanisms:`, and `context-window:`;
  `ValidateServers` grows the two-flags and posture-on-unflagged refusals. (Amended 2026-09-30:
  `sub-agents:`, `mechanisms:` and both refusals are gone, per ADR 0066 and ADR 0076.)
- The engine gains the Delegation-target latch and its setter; `newChildAgent` consults it
  for upstream, window, profile, posture, and the **effort dialect** instead of inheriting all
  five from the parent. *Amended 2026-08-31 — this record originally said "all four", before
  the dialect existed; that enumeration is superseded, not merely extended.*
  ADR 0060 decision 3
  makes the thinking-effort wire dialect a property of the SERVER, and a routed child is on
  another server, so a child that inherited the ORCHESTRATOR's dialect sent its compaction
  summariser's "no reasoning" request in a shape the routed server does not read: the fold
  spent its whole output cap on reasoning and faulted at every Turn boundary. A target that
  NAMES no dialect — the entry pins none and the server advertises none — still leaves the
  child on the parent's, so the fix adds a rung rather than moving the floor.
- CONTEXT.md's Sub-agent claim "its context window is not reduced" becomes conditional on
  routing — the child works against the TARGET's window once routed.
- The per-model-profiles plan (`docs/plans/2026-08-11 - 05`) must execute before this
  lands; the Delegation target consumes its `profiles.Resolve`.
- A session `/server`-switched ONTO the flagged entry is briefly observed by two monitors —
  harmless (same facts), noted so the wiring may dedupe but need not.
