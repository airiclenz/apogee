---
title: ADR 0010 decision
---

Adopt a **domain-core / engine / thin-facade** layout governed by one hard rule.

**Invariant: `internal/*` never imports the root `apogee` package. Dependencies flow *down*
toward a foundational domain package; the root imports downward only.**

Three layers:

1. **`internal/domain`** — the **ubiquitous language (CONTEXT.md) rendered
   as Go**: every type, interface, enum, sentinel error, and hook working-value in the public
   surface — `Config`, `Mode`, `Event` + variants, `EventSink`, `Approver`,
   `Tool` / `ToolCall` / `ToolResult`, `ToolRegistry`, the five hook interfaces, `Mechanism` +
   `MechanismDescriptor` + `OrderingConstraints`, `MechanismRegistry`, the
   `Request` / `Response` / `Conversation` working values, `Confiner` +
   `ConfinementCaps` / `ConfinementBox`, `Session`, `LoopView` / `ConversationView`. It also
   owns the **pure logic** intrinsic to those types (the registry's ordering-cycle detection,
   `ConfinementCaps.AutoEligible`, conversation (de)serialization). Depends only on the
   standard library. *(Named `domain`, not `core`, to avoid resurrecting the retired "Apogee
   Core" library term — CONTEXT.md "Retired terms"; this internal package is unrelated to it.)*

2. **The subsystem / adapter packages** — `internal/agent` (the engine: the loop and Turn
   state machine, conversation state, modes, sub-agent orchestration), `internal/provider`,
   `internal/processing`, `internal/tools`, `internal/mechanisms`, `internal/context`,
   `internal/session`, `internal/platform`, `internal/mcp`, `internal/security`,
   `internal/tui`. Each imports `internal/domain` (and sibling ports as needed) and **never**
   root. The engine depends on the other subsystems through `domain` port interfaces, so it is
   unit-testable against fakes — the same access pattern the bench uses through the public API,
   the one the `Responder` seam already prefigures.

   (Amended 2026-09-30: this list is the Phase-1 snapshot. `internal/mechanisms` and the
   `Mechanism` types, registry and `NewMechanismRegistry` forwarder are deleted — the Reaction
   core of ADR 0076 replaced
   them — and some forty packages have joined since; `ls internal/` is the current list. The
   invariant and the three layers stand.)

3. **`apogee` (root)** — a **thin facade**: type aliases (`type Tool = domain.Tool`),
   re-exported consts and sentinel errors (`const ModePlan = domain.ModePlan`;
   `var ErrAutoUnavailable = domain.ErrAutoUnavailable`), and forwarding constructors (`New`,
   `Resume`, `DecodeSession`, `NewToolRegistry`, `NewMechanismRegistry`) that delegate to
   `internal/agent` / `internal/domain`. It imports `internal/domain` and `internal/agent` and
   holds **no engine logic**. A compile-time completeness guard — an external `example_test`
   (package `apogee_test`) that names the full public surface — fails the build if an alias is
   forgotten.

   (Amended 2026-10-02: the root now also imports `internal/config`, `internal/eventjson`,
   `internal/profiles`, `internal/reactions` and `internal/workflow` beside `domain` and `agent` —
   each only for aliases and forwarders (`DefaultSystemPrompt`, the shipped-profile lookup, the
   recipe and workflow-plan types, `RunnerOptions`/`ReactionRunner`). It still holds no engine
   logic.)

**Canonical placement (the lowest-layer rule):** a type lives at the lowest layer that can
define it without importing upward; the root re-exports the public ones.
- `Agent` (the engine handle) + its methods (`Step` / `Run` / `Submit` / `Snapshot` /
  `Close`) and `New` / `Resume`: **`internal/agent`**.
- Everything else public: **`internal/domain`**.
- Aliases + forwarders: **root**.

**Confiner placement (resolves §6.1; ratifies TDD §4.1 #1).** The `Confiner` interface and
`ConfinementCaps` / `ConfinementBox` move into `internal/domain`; the root re-exports them via
aliases — so the interface stays **public** (the host injects it via `Config`) while its
definition sits where both the loop and the backends see it without an upward import.
`internal/platform` stops importing root and imports `internal/domain`. There is **no** public
`apogee/platform` subpackage; the single root facade remains the only public package.

**Options weighed.**
- **(a) Fat root** — keep the loop in root, push only root-type-free seams down — was rejected:
  to avoid the seeding cycle it forces the *entire* tool and Mechanism catalogues into the root
  package, producing exactly the god-package this layout exists to prevent. It wins only on
  short-term churn, which is not a Phase-1 constraint.
- **(c) Full inversion** (loop in `internal/agent`, root as pure aliases) is essentially this
  decision; we additionally factor the *types* out of the engine into `internal/domain` so the
  engine package itself stays small and the domain language has a dependency-free home.
