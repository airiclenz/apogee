---
title: Firings never re-latch the Sub-agents target after a fallover
status: open
type: bug
priority: 3
plan: 2026-11-02 - 02 - turn-total-and-open-bugs-plan
created: 2026-11-02T14:46:59Z
---

resolveFiringRouting (cmd/apogee/wire_firing.go) builds the Delegation target once and newFiringSeatFallback lands with no re-latch hook, so in headless, daemon and session-raised Firings every routed child spawned after a Sub-agents-seat fallover dials the dead primary, retries, faults, then silently reads the cached switch. The TUI's delegationWiring.fallover pushes the fallback's target. Found in item 11 NOTES of plan 2026-11-02 - 00 - midrun-model-fallback-plan.

## Why

Each later routed spawn pays retries against a dead server (up to the dial timeout per attempt on a black-holed host) and its stream shows failures with no upstream_switched line.

## Acceptance

- After a Sub-agents-seat fallover in a Firing, a later routed spawn sends nothing to the primary, pinned by a headless test

## Log

- 2026-11-02 Airic Lenz: Plan 2026-11-02 - 01 - fallback-followups-plan item 8 (archived at 81fc817e) landed the Firing re-latch: a later routed spawn dials the fallback for its turns. Still open: the Firing's delegation namer (newFiringNamer) keeps the primary's routed binding, so a later unnamed child still asks the dead primary for its name.
