---
title: ADR 0004 considered options
---

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
