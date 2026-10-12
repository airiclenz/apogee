---
title: ADR 0014 consequences
---

- **A principle now constrains every future Mechanism:** a Mechanism steers the primary call;
  it never makes its own Upstream call and never spawns sub-agents outside dispatch.
  Compaction's internal call remains a structural privilege, not a precedent.
- **The Mechanism descriptor grows `Requires`** (CONTEXT.md updated): config validation gains
  an enable-time dependency check, and the bench evaluates required stacks as a unit —
  `guided_decomposition` is benched with `tool_result_cap`, never alone.
- **"Deferred Response Action" is widened** (CONTEXT.md updated): the deferred thing is a
  *decision* — a correction, or carried work such as the remaining-items queue.
- **CONTEXT.md gains the fifth context-and-history operation**: capping and truncation *cut*,
  Compaction *summarises*, guided decomposition *avoids* — "decompose" joins the
  must-not-conflate list.
- **Ships default-off, bench-gated** like every Mechanism (D1): it flips on only when the
  ADR 0009 non-inferiority gate passes for the decomposition + capping stack.
- **Parked separately** (ISSUES.md, 2026-07-05): mid-Exchange auto-compaction at quiescent Turn
  boundaries under pressure — a structural-reducer contract change with its own blast radius;
  if it ever lands, it would loosen (not remove) this Mechanism's `Requires` coupling.
