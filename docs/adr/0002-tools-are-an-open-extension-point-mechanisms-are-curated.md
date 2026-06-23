---
Status: accepted
---

# Tools are an open extension point; the Mechanism catalogue is curated

## Context

The public Go API is a third-party embedding surface (see
[ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)).
That forces a boundary question: what can embedders *add* — custom tools, custom
Mechanisms, both, neither?

## Decision

**Tools are open**: the `Tool` interface and registry are part of the public surface, and
embedders may register their own tools. An application embedding Apogee will routinely
need app-specific tools; it is cheap and low-risk.

**The Mechanism catalogue is curated**: the hook-point interfaces are public (the bench
must register experimental hooks, so embedders technically can too), but the built-in set
of Mechanisms is owned by Apogee. A third-party hook runs, but it does **not** join
Adaptive Suppression / effectiveness tracking unless it supplies a Mechanism descriptor,
and that path carries **no stability promise** for v1.

The asymmetry is deliberate: Mechanisms participate in cross-cutting self-regulation
(gating, suppression, the descriptor incompatibility graph) whose coherence we are not
ready to expose as a stable contract. Opening it later is easy; retracting an open
contract is not.
