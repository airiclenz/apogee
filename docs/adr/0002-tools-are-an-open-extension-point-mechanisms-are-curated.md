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

**Note (2026-07-25) — a tool may now attach a typed summary, and omitting it is fully
supported.** `domain.ToolResult` gained one optional field, `Summary domain.ToolSummary`: the
structured half of a tool's outcome, for a *host*, beside the prose `Content` that is for the
*model* (CONTEXT.md, **Tool summary**). The sum is **sealed** the way `domain.Event` is — the
marker method is unexported — so the variant set stays Apogee's and grows additively; external
code, including the root facade's re-exports, can *read* every variant and *add* none.

**This does not narrow the open extension point.** A summary is an extra a tool *may* attach,
never something the `Tool` interface requires: `Summary` nil is the normal case (seven built-ins
attach one; the rest do not), and a host that receives none renders from `Content` exactly as it
did before summaries existed. An embedder's tool therefore behaves identically before and after
this change — it simply cannot mint a new *variant*, which is the same trade `Event` already
makes. That asymmetry is smaller than the Mechanism one above and is accepted for the same
reason: the variants are a rendering vocabulary we are not ready to freeze as third-party
surface, and opening it later is easy.
