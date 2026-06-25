---
Status: accepted
---

# The agent loop is an embeddable library, driven by an external bench

## Context

Apogee must be validatable: we need to simulate changes and measure how the agent
behaves against real local models before trusting a Mechanism. The predecessor
`apogee-sim` already owns the instrument for this — RealSandbox, classifier, scorers,
trace archive, and the fork/stepwise counterfactual rig. The open question was *how*
that instrument reaches the agent now that the agent (not a proxy) owns the loop, tool
execution, and conversation state.

## Decision

The eval/simulation harness stays **out of the `apogee` binary** and lives in
**`apogee-sim`** (the bench). The bench reaches the agent by **importing
`github.com/airiclenz/apogee` as a Go module** and driving the agent loop as an
**embeddable library**, in-process — not by shipping the harness inside the binary, and
not over a serialized control protocol.

To make this possible, the loop is designed as an embeddable, steppable component:

- it owns **no process-global state and no implicit global filesystem state** — an agent
  is a value the caller constructs, and *every* state root (Library, sessions, config) is
  injected via `Config`, never assumed at `~/.apogee`;
- **conversation state is a cleanly copyable value** (no live handles, no process globals)
  — a hygiene property Apogee owns; the bench builds forking *on top of* it, but the loop
  itself exposes no fork;
- it exposes **Hook points as Go interfaces**, so the bench can register a temporary
  **experimental hook** to test a behaviour that is not yet a Mechanism, and the loop
  can be **stepped** one Turn at a time;
- session **snapshot/resume** (a real user feature) doubles as the bench's
  snapshot/restore primitive.

The shipped binary links none of the bench's code.

**What Apogee exposes vs. what the bench composes.** Apogee exposes only **snapshot/resume**
(a real user feature) and **clean-library hygiene** (Config-injected state roots, no process
globals, copyable conversation state, an injectable tool registry, Hook-point interfaces).
**Forking is *not* an Apogee feature** — the bench *composes* forking, record/replay, and
counterfactual scoring on its side from those primitives (deep-copy the copyable state, copy
the sandbox directory, drive two branches from byte-identical state). No fork or record/replay
code ships in the binary. (External effects — MCP, network — are non-forkable; the bench
disables them with deterministic stubs for v1 — see
[ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md).)

## Considered options

- **Harness inside the binary** (a `sim`/`bench` subcommand) — rejected: bloats the
  binary against its single-static-binary promise; the bench is a development-time
  instrument end users never run.
- **Drive the real binary over a serialized stdio protocol** (step/snapshot/intervene as
  JSON) — rejected: highest fidelity and language-agnostic, but neither is needed (both
  repos are Go), and it imposes a permanent versioned control surface on the agent —
  exactly the weight the small-binary goal resists.

## Consequences

- The loop must expose a **public** Go package (it cannot be buried in `internal/`).
- The bench measures the *library build* of the loop, not the literal shipped binary;
  fidelity is protected by a `go.mod` version pin.
- "Embeddable, steppable, no process-global state, copyable conversation state" becomes a
  Phase-0 design constraint on the loop — get it wrong and neither forking nor stepping
  is possible later.
- **Bench isolation by default**: because state roots are Config-injected, the bench
  points the Library/sessions at ephemeral directories, so sim runs never read or write the
  developer's production Library — runs stay reproducible and the production Library is
  never flooded by a narrow sim distribution. A deliberate opt-in "reduced-weight bleed" of
  sim observations into production may be added later *if* it proves worthwhile; it is not
  built by default.
- The public API is **not bench-only**: it is a deliberate **third-party embedding
  surface** — other applications can run an Apogee agent in-process. The TUI, the
  optional `apogee headless` CLI, and the bench are all consumers of this one package.
  This raises the bar on API stability and versioning (semver, a guarded public surface)
  and makes the `internal/` vs public boundary a product decision, not an afterthought.
- **Co-development & versioning.** apogee-sim uses a `go.mod replace` → a local apogee path
  during active development (the bench measures the working tree); a pinned version/commit is
  used only for archived A/B evidence. The public API is **v0.x with no stability promise
  through Phase 3, and `v1.0.0` is cut at the end of Phase 3**, once every consumer (TUI,
  bench, optional `headless`) has exercised it. Events and Hook points stay **additively
  extensible** (a new variant is a minor bump). Seed types the bench needs (e.g.
  `OrderingConstraints`) **move into apogee** and the bench imports them — never the reverse.

  > **Amendment 2026-06-25 (P3.16 — `v1.0.0` cut).** Phase 3 is complete and **`v1.0.0` is
  > tagged**. The "v0.x, no stability promise" clause above is now spent: **semver begins.** The
  > frozen v1 public surface is the root `apogee` package (`Agent`/`New`/`Resume`, `Config` and
  > its host delegates `EventSink`/`Approver`/`Asker`/`ExternalEffects`, the four-rung `Mode`
  > ladder, the `Tool`/`ToolRegistry` extension point and its `ReadOnlyTool`/`ExternalEffectTool`
  > markers, the `Event` variants, and the hook points); tools are an open extension point behind
  > the registry (ADR 0002), not root types. Events and hook points remain **additively
  > extensible** — a new Event variant or hook point is a **minor** bump, not a break. Phase-3
  > public-surface additions reviewed at the freeze (§3 D7 of the Phase-3 detail plan): the
  > `Asker` host delegate (struct-typed for additive growth) and the `ModeAllowEdits` constant.
  > The changelog is tracked from this release in [`CHANGELOG.md`](../../CHANGELOG.md).
