---
Status: accepted
---

# Auto mode requires OS-level confinement — a capability matrix, not a binary

## Context

Apogee has three agent modes: Plan (read-only), Ask-Before (human approves each tool
call), and Auto (runs tool calls without per-call approval). Auto is the one production
scenario that resembles the bench's *unsupervised* runs — there is no human gate — so it
is the one place OS-level confinement actually buys safety. apogee-code shipped no OS
confinement at all (approval was the only gate); apogee-sim's `RealSandbox` proved out the
confinement tech (seatbelt on macOS, namespace network-deny + write-confinement on Linux)
but only for the bench.

We chose to be safe from the start rather than treat confinement as a later optional
enhancement. The refinement this ADR settles: confinement is **not one thing** — a backend
may confine filesystem writes but not network egress (or vice-versa), and different tools
need different confinement — so "is it confined?" is the wrong question to gate on.

## Decision

**Auto mode requires OS-level confinement of tool execution; it is a v1 gate** (for the v1
POSIX targets, macOS + Linux — Windows confinement follows in Phase 5 alongside Windows
support).

Confinement is a **capability matrix, not a binary.** Each `Confiner` backend reports which
restrictions it can actually enforce — at minimum `{fs-write, network-egress}`, extensible.
The gate reads that matrix:

- **Auto requires *both* fs-write *and* network-egress confinement.** A tool running
  unsupervised must be unable to both escape the workspace *and* reach the network.
  Half-confinement is not Auto-eligible.
  - On **Linux**: filesystem confinement is landlock (kernel ≥5.13), but **network
    confinement needs landlock ABI v4 (kernel ≥6.7)**. So **Linux Auto requires kernel
    ≥6.7**; on 5.13–6.6 the box can confine writes but not network ⇒ **Auto is refused,
    degrading to Ask-Before. There is no `--auto-allow-network` escape hatch.**
  - On **macOS**: `sandbox-exec`/seatbelt enforces both fs and network in one profile.

The invariant that makes this safe rather than aspirational, **generalized per-tool**:

> **A tool runs unsupervised only if it can be confined. If confinement of a given tool
> cannot be established, that tool is not run unsupervised — Apogee never executes a tool
> call both unsupervised *and* unconfined.**

The per-tool form has teeth: **MCP tools execute in an external server Apogee cannot
confine**, so **MCP tools gate through Approval even in Auto mode** — Auto silently becomes
Ask-Before *for those tools*, while confinable in-process tools still run unsupervised.

Scope by mode:
- **Auto** — confinement (fs-write **and** network) **required**, evaluated per tool;
  unavailable for a tool ⇒ that tool falls back to Approval, and if the whole box cannot be
  established, Auto is refused.
- **Ask-Before** — confinement **available but optional** (defense-in-depth, opt-in); the
  human is the primary gate.
- **Plan** — not applicable (read-only).

The default confinement box = **workspace-write-only + network default-deny + per-project
allowlist.**

`platform/` gains a **`Confiner`** interface designed in Phase 0 (reporting its capability
matrix). Backends: macOS via seatbelt/`sandbox-exec`, Linux via **landlock**
(`golang.org/x/sys`) — landing by Phase 3 when agent modes ship; Windows (AppContainer /
Job Objects) in Phase 5.

## Considered options

- **Approval + modes only, OS confinement as a later optional enhancement** (the §3a
  default posture) — rejected: leaves Auto mode running unconfined on day one, the exact
  unsupervised-and-unconfined case we want to forbid.
- **Treat confinement as a single boolean** (confined / not) — rejected: a backend that
  confines writes but not network would read as "confined" and let an unsupervised tool
  reach the network. The capability matrix is what makes the fs-*and*-network requirement
  expressible at all.
- **A `--auto-allow-network` escape hatch for old Linux kernels** — rejected: it re-creates
  the unsupervised-and-network-reachable hole the gate exists to close. Old kernel ⇒
  Ask-Before, full stop.

## Consequences

- **Tension with Standing Requirement 2** (single static binary, zero external deps for
  core function), accepted and bounded: the *core loop* and Plan/Ask-Before still run with
  zero external deps; only **Auto** depends on a confinement facility (and on macOS that
  facility is the system `sandbox-exec` binary). When the facility is absent or insufficient
  (old Linux kernel, missing `sandbox-exec`), Auto is simply unavailable — not unconfined.
- **`platform/` scope grows** beyond the plan's "shell + path abstraction": a `Confiner`
  that *reports a capability matrix* is a first-class Phase-0 interface, and confinement
  backends are a v1 deliverable, not a fast-follow.
- **MCP-in-Auto is a cross-package contract**: the MCP client and the mode/confinement logic
  must cooperate so an Auto session still raises Approval for MCP tool calls — not an
  afterthought.
- The bench keeps its own `RealSandbox`; it *may* later drive Auto-mode-confined runs
  through Apogee's public `Confiner` for higher fidelity, but that coupling is not required.
- Confinement design is hard and OS-specific enough to warrant its own dedicated session.
