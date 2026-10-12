---
title: ADR 0013 consequences
---

- **`security.Guards` gains `ForSubAgent()`** (fresh breaker + fresh audit, shared read-only
  `Dangerous`); the misleading `Guards` doc comment is corrected to describe the live-state
  aliasing honestly. A sub-agent breaker trip provably does **not** trip the parent's, and a
  sub-agent cannot loosen the dangerous floor (both tested).
- **`Agent` gains a `depth` field**; `base()` becomes a method stamping `Depth` so every event
  an `Agent` emits carries its nesting level. Top-level events stay `Depth == 0`; sub-agent
  events are `Depth == 1`. **P3.14** turns this from *tolerate* into *render* (the TUI frames a
  `Depth > 0` block); it needs only the `Depth` on events, which this ADR establishes.
- **The default tool set gains `sub_agent`** (`DefaultTools` is now ~19 built-ins; 2026-07-23:
  21 with the host-gated `ask_user`/`present_document` — ADR 0019). It is
  offered in every mode **including Plan** (it is the recursion point, bounded one level down —
  a Plan sub-agent inherits Plan, so its children are read-only), and is **withheld at the depth
  bound**.
- **Acceptance held under `-race`:** a Plan-parent sub-agent cannot write (inherits Plan); a
  subset-narrowed sub-agent cannot call a tool the parent has but the subset omits; an Auto
  sub-agent confines a child subprocess, runs a child Apogee write path-safety-bounded, and
  still Approval-gates child MCP/external (the per-call disposition, one level down); nested
  events arrive at `Depth == 1`; a child tool panic recovers at the parent boundary (ADR 0007)
  without killing the parent Exchange; a cancel during a sub-agent rolls the whole parent Turn
  back; the depth bound holds via both the withheld tool and the defensive refusal.
- **Forward-compat preserved:** the snapshot schema's "suspended sub-agent" slot stays
  reserved-but-empty, so nested stepping drops in later without a schema break.
