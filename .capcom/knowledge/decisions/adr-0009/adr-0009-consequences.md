---
title: ADR 0009 consequences
---

- The bench grows real statistical machinery it did not have: paired per-task analysis, A/A
  calibration, FDR across the family, a CI-based disposition. The predecessor's pooled-runs
  Fisher script is retired as the *decision rule* (it may survive as a quick eyeball).
- The frozen, pre-registered, discriminating suite is a deliverable in its own right — building
  it (and growing it until the A/A band is tight) is the main cost, replacing "just run more."
- Bench external-effect handling (deterministic stubs,
  ADR 0008) is load-bearing *for
  this ADR*: live external flakiness would widen the A/A noise floor and pollute δ.
- This rule governs Phase 4 end-to-end; the Mechanism catalogue is whatever survives it.
