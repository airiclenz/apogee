---
title: ADR 0004 consequences
---

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
