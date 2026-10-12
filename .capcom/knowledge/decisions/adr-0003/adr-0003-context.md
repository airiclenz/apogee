---
title: ADR 0003 context
---

apogee-sim runs its Transforms in a **fixed order enforced in code**
(`cot → library → codeinfo → filter → decompose → compress`). The merged Apogee must be
**modular** — adding a Mechanism (or promoting a bench experimental hook to one) should be
easy and should not require editing the loop, because the catalogue is evidence-driven and
expected to churn.
