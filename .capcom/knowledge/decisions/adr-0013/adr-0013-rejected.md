---
title: ADR 0013 considered options
---

- **Thread `Guards` verbatim (share breaker + audit + floor)** — *rejected* (the carried
  finding): a sub-agent's runaway trips the parent's breaker and inherits a pre-tripped one;
  audit trails conflate. The only thing that *should* be shared is the read-only floor.
- **Isolate everything, including a fresh dangerous floor per sub-agent** — *rejected*: a
  per-sub-agent floor is a per-sub-agent place to *re-derive* (and therefore loosen) the floor,
  reopening exactly the hostile-input hole the tighten-only merge closes. The floor must be one
  shared, unloosenable instance.
- **Make `sub_agent` a normal leaf tool with a disposition marker** — *rejected*: a sub-agent's
  blast radius is not a single call; confining/gating it "as a unit" is meaningless and would
  either over-block (gate the whole delegation) or under-block (skip the per-child disposition).
  The recursion point gets the per-child disposition right by construction.
- **No depth bound (rely on the model not to recurse)** — *rejected*: a small model can loop;
  an unbounded tower is a resource footgun. A small fixed ceiling with defence-in-depth is cheap
  and total.
