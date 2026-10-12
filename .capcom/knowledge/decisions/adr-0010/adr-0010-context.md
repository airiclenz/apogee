---
title: ADR 0010 context
---

Phase 0 surfaced a real architectural tension (TDD §6 #7) and deferred it deliberately to
Phase 1. The public API package **must** be the module root, `github.com/airiclenz/apogee` —
embedders write `apogee.New`, `apogee.Config`, `apogee.Tool` (ADR 0001).
But nearly every subsystem the loop needs — the engine, the provider, the tool suite, the
Mechanism catalogue, the parsers, the platform backends — must reference the public domain
types (`Tool`, `ToolCall`, `Request`, `Event`, `Confiner`, the hook interfaces, …), and the
core must *wire those subsystems together* in `New`.

Go forbids import cycles. With the public types defined **in the root package**, two of the
three obvious layouts cycle:

- A subsystem package (e.g. `internal/tools`) imports root for the public types, and root
  imports that subsystem to seed the built-in set ⇒ **root ↔ subsystem cycle.** This is not
  hypothetical: `NewToolRegistry` / `NewMechanismRegistry` are specified to return registries
  *seeded with the built-in catalogue*, so root would import `internal/tools` and
  `internal/mechanisms` — which import root.
- Moving the loop into `internal/agent` (where the plan §3 layout puts it) fails the same way:
  the loop touches almost the entire public surface, so `internal/agent` would import root
  while root imports `internal/agent` for `New`.

P0.6 sidestepped this for the **throwaway** capstone by putting the loop in the root package
and keeping only a root-type-free provider seam (`internal/agent.Responder`) in `internal/`.
That was an explicit throwaway-thin slice, *not* a layout decision (Phase-0 detail plan §3 "As
built"). The one already-committed wrong-way edge is `internal/platform`, which imports root
for the `Confiner` trio (`Confiner` / `ConfinementCaps` / `ConfinementBox`): harmless today,
but it is exactly the edge that *forces* the cycle the moment the loop must construct a
platform backend (Host for tools, Confiner for the Auto gate).

Two questions had to be answered together — TDD §6 #7 (facade ↔ engine placement / import
direction) and TDD §6.1 (where the `Confiner` interface lives). The repo is early enough
(~2k LOC, mostly a signature sketch plus a throwaway loop) to set the layout *now*, before the
tool and Mechanism catalogues — where the cycle pressure becomes acute — exist.
