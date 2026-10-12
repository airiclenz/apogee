---
title: ADR 0057 consequences
---

- ADR 0044 decision 4 is superseded by decision 5 above (reconciled in place with a dated
  pointer); its deferred 'inherit' spelling closes as obsolete.
- `RebindSpec`'s and `SetProfile`'s doc comments change again (the roster joins the per-model
  bindings); ADR 0024 §4's "what stands across a rebind" reading follows.
- CONTEXT.md's Model-profile entry widens to three axes and axis-wise resolution.
- The bench arms in `docs/design/tool-surface-findings.md` gain their landing zone: an arm that
  concludes "remove X for the small class" ships as a default-roster or profile change instead
  of a global removal.
