---
title: Rehomed routed child takes the session's root window pins
status: done
type: bug
priority: 3
plan: 2026-11-02 - 01 - fallback-followups-plan
created: 2026-11-02T14:12:36Z
closed: 2026-11-02T16:20:10Z
reason: Resolved by plan 2026-11-02 - 01 - fallback-followups-plan item 4 (archived at 81fc817e)
---

When a routed child is rehomed mid-Turn, its binding is resolved by `fallbackLander.arrive`/`entryUpstream` with `rootPinsOf(opts)` (cmd/apogee/fallback.go:276). The session's top-level `context-window:`/`working-window:`/`response-reserve:` pins therefore apply under the fallback entry's own pins. `resolveDelegationTarget` deliberately never applies those root bounds to a routed target, so the in-flight child's window can differ from what later spawns on the fallback get (cmd/apogee/delegation.go:313 seatFallbackFor, cmd/apogee/wire_firing.go:760 newFiringSeatFallback). Deferred from plan 2026-11-02 - 00 - midrun-model-fallback-plan (item 11).

## Why

An in-flight routed sub-agent and a later spawn on the same fallback see different windows.

## Acceptance

- A routed child rehomed onto a fallback is bound without the root window pins, as resolveDelegationTarget binds a routed target, pinned by a test
