---
title: ADR 0004 decision
---

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
