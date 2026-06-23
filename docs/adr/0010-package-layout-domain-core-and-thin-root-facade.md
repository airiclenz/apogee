---
Status: accepted
---

# Package layout: a domain core, an engine, and a thin root facade

## Context

Phase 0 surfaced a real architectural tension (TDD §6 #7) and deferred it deliberately to
Phase 1. The public API package **must** be the module root, `github.com/airiclenz/apogee` —
embedders write `apogee.New`, `apogee.Config`, `apogee.Tool` ([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)).
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

## Decision

Adopt a **domain-core / engine / thin-facade** layout governed by one hard rule.

**Invariant: `internal/*` never imports the root `apogee` package. Dependencies flow *down*
toward a foundational domain package; the root imports downward only.**

Three layers:

1. **`internal/domain`** — the **ubiquitous language ([CONTEXT.md](../../CONTEXT.md)) rendered
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

3. **`apogee` (root)** — a **thin facade**: type aliases (`type Tool = domain.Tool`),
   re-exported consts and sentinel errors (`const ModePlan = domain.ModePlan`;
   `var ErrAutoUnavailable = domain.ErrAutoUnavailable`), and forwarding constructors (`New`,
   `Resume`, `DecodeSession`, `NewToolRegistry`, `NewMechanismRegistry`) that delegate to
   `internal/agent` / `internal/domain`. It imports `internal/domain` and `internal/agent` and
   holds **no engine logic**. A compile-time completeness guard — an external `example_test`
   (package `apogee_test`) that names the full public surface — fails the build if an alias is
   forgotten.

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

## Consequences

- **P1.0 is the first Phase-1 task and a precondition of every other body.** The throwaway
  P0.6 internals in the root package (`loop.go`, `conversation.go`, `registry.go`) move into
  `internal/{agent,domain}`; the public methods in `apogee.go` become aliases / forwarders.
  P1.0 is a **pure move** — the existing 12 tests must still pass — landing with verify
  (`gofmt` / `vet` / `build` / `test -race` + the 6-target cross-build) green before any real
  provider/loop work.
- `internal/agent.Responder` (the provider seam) moves to its real home, `internal/provider`,
  beside the HTTP client that will implement it. The wire types
  (`provider.Request` / `RawResponse` / `Message`) stay provider-local and root-type-free; the
  loop translates `domain` conversation state ↔ wire shape at that boundary. The doc-only
  `internal/agent` placeholder is superseded by the real engine package.
- The plan §3 layout (loop under `internal/agent/{loop,subagent,modes}`) is **confirmed**, not
  changed — P0.6's root-package loop was the deviation. The only structural addition is
  `internal/domain` beneath everything, plus the explicit "internal never imports root"
  invariant.
- **New cost:** the root facade carries mechanical re-export boilerplate (~30 consts, the
  sentinels, ~6 constructors). One-time, drift-guarded by the `example_test` completeness
  check — the price of an engine that lives in its own testable package.
- **CONTEXT.md gains a code home.** `internal/domain` is the glossary as Go: adding a domain
  term is adding a `domain` type and (if public) one root alias — a two-line mechanical
  extension that keeps the public surface and the language in lockstep.
- The bench's isolation and fake-driven access pattern (ADR 0001) becomes the **native** shape
  of the system: the engine consumes `domain` ports, so driving it against a fake
  provider / tool / Mechanism is ordinary, not special-cased.
- **Stability (ADR 0001 §18).** The public surface *is* the set of root aliases. Variants stay
  additively extensible: a new `Event` variant is a new `domain` type + a new root alias (a
  minor bump), with the sealing (an unexported method on the `domain` interface) preserved
  through the alias.
</content>
</invoke>
