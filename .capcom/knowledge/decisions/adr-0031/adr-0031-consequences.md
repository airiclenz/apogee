---
title: ADR 0031 consequences
---

- Future design sessions cite this ADR as a tiebreaker; door-closing proposals must
  supersede it explicitly.
- The first mechanical consequence of invariants 1–4 is the deferred **`apogee
  headless`** runner: once it exists, any capability that only works through the TUI
  visibly breaks the headless path instead of silently accreting — the architectural
  analogue of the Builder guard test. No schedule is implied; sequencing lives in plans.
- `CONTEXT.md` gains **Driver** as a canonical term.
