---
title: ADR 0099 consequences
---

- A second press typed in time always acts, even when a loaded program folds it late; one typed
  after the tick fired only re-arms, as before.
- The e2e `stopRun` helper no longer needs its retry loop (plan item 2 of the same sweep).
- Docs and comments describe the window as "while the gesture is armed" or "before its reset
  tick", never as a wall-clock second measured at the second press.
