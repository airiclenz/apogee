---
title: ADR 0060 consequences
---

- ADR 0050 decisions 2, 4 and 5 carry dated pointers here. Decision 2's "one canonical wire
  mapping" reads as one mapping *per dialect*; decision 4's enum widens; decision 5's command
  becomes a picker and gains the narrow clear-on-switch exception.
- `provider.Discover` reports an `EffortSupport` observation (supported, dialect, levels,
  default) on `ModelInfo`; it rides the Beat to the TUI and the dialect rides the rebind spec
  to the engine — and, since 2026-08-31, the Delegation target to a routed child (ADR 0045).
- `servers:` entries gain `effort-dialect:`; `CONTEXT.md`'s Thinking-effort entry and the
  configuration and commands manual pages state the widened vocabulary, the three dialects, the
  footer readout and the picker.
- The out-of-band naming call carries the observed dialect, read from the composition root's
  live latch, never from the engine (ADR 0022 addendum — it is not a Turn).
