---
title: TUI fallover skips the launcher follow and the fallback profile's rebind settle
status: open
type: bug
priority: 2
plan: 2026-11-02 - 01 - fallback-followups-plan
created: 2026-11-02T14:46:59Z
---

A TUI Server fallback (serverHost.FollowFallback → sessionMover.fallover → arrive) never runs launcherPath.follow and never runs rootWiring.rebind: tui.Model.followFallback nils pendingRebind. So the fallback model keeps the primary profile's tools: roster, an effort override the fallback does not offer is not cleared, profile notices are dropped, and /model, /stop-server and /unload-model keep acting on the primary's llama-launcher. Firings with a Driver-built tool set (firingSeatTools non-nil) keep the primary roster too. /server does all of this. Found in item 10 NOTES of plan 2026-11-02 - 00 - midrun-model-fallback-plan.

## Why

ADR 0105 D5: one beat on the fallback binds its model, window and per-model facts; the profile roster is a per-model fact (ADR 0057).

## Acceptance

- After a TUI fallover the launcher path follows the fallback entry and, at the next Turn boundary, the fallback profile's roster, effort clear and notices apply, pinned by tests
- A Firing with a Driver-built tool set offers the fallback profile's roster after a fallover, pinned by a test
