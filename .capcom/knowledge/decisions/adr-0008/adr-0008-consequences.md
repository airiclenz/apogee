---
title: ADR 0008 consequences
---

- The public `Tool` interface documents statelessness-across-Turns as a contract; tool authors
  (including third parties — ADR 0002)
  must honour it. A tool that needs persistence must serialize it into conversation/session
  state, not hold it live.
- External-effect tools route through a **single injectable boundary** so the bench can swap in
  deterministic stubs (and, later, record/replay) without touching tool code.
- A bench task that *requires* live external content becomes always-fail under stubs and falls
  out of the frozen discriminating suite
  (ADR 0009) — v1 validates Mechanisms on the
  network-independent core; external-dependent task validation is out of scope and flagged.
- Disabling external effects keeps the bench's non-determinism confined to LLM sampling, which
  keeps the A/A noise floor — and therefore δ — clean (ADR 0009).
