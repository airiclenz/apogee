---
title: ADR 0013 context
---

ADR 0005 fixed the *policy* — a
sub-agent's privileges are always ≤ its parent's. The Phase-3 plan's **D2** sketched the
*shape*; P3.13 is where sub-agents are actually spawned, so the remaining calls have to be
settled in code:

- **Where does a sub-agent live in the dispatch flow?** A sub-agent is the embeddable `Agent`
  (ADR 0001) one
  level down. It is exposed to the model as a tool (`sub_agent`), but it is unlike every other
  tool: it is the **recursion point**, not a leaf — its "execution" is a whole nested loop,
  each of whose child tool calls must itself get the full per-call blast-radius disposition
  ([D5](../../../../docs/plans/archived/phase-3-detail-plan.md) / ADR 0012).
- **What does a sub-agent inherit, and how is "≤ parent" made structural** rather than a thing
  a careless port can drop (apogee-code's `SubAgentOrchestrator` constructed the loop with *no*
  approval manager and *no* mode — the exact hole ADR 0005 closed)?
- **The carried `/code-review` finding:** `security.Guards` bundles the dangerous-action floor,
  the circuit-breaker, and the audit log. Breaker and audit hold **mutable** pointer-backed
  state. A naïve "thread Guards verbatim into the sub-agent" therefore makes the sub-agent and
  the parent **share one breaker and one audit trail** through the aliased pointers — a
  sub-agent's runaway tool-loop would trip the *parent's* breaker, and a fresh delegated task
  would start pre-tripped by the parent's earlier failures. Decide **share-vs-isolate** and
  implement it.
- **What stops an unbounded tower of sub-agents** (each level is a full nested loop)?
- **How does stepping/cancel/snapshot interact** with a loop running inside another loop's
  tool dispatch?
