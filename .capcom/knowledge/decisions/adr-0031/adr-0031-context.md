---
title: ADR 0031 context
---

Unified-memory scaling (Apple M-class; M7 Ultra rumoured up to ~1.5 TB) is on track to
make on-prem inference viable for small and medium companies around ~2027. The owner's
strategic bet (grilled 2026-08-03) is that Apogee should be able to grow into a **local
AI automation platform** — scheduled and webhook-triggered runs, a thin workflow layer,
enterprise data access, governance; a local-first analogue of cloud automation
platforms. The bet is the *trend*, not the exact specs; it survives the rumours being
off by 2×.

Nothing needs building yet. The risk this ADR addresses is different: doors close
through small local decisions — a capability reachable only by keypress, an approval
welded to a live prompt, a "harmless" wire endpoint on the engine — not through the
absence of a grand design. Meanwhile the load-bearing prerequisite already exists:
ADR 0001
makes the loop an embeddable library with injected state roots, and two **Drivers**
(TUI, bench) already prove the pattern a future daemon would join.
