---
title: ADR 0088 consequences
---

- ADR 0013 §5(b) is superseded. §5(a) (no snapshot mid-child) and §5(c) (resume only before or after
  a delegation) stand: a child is still atomic to the *snapshot*, just no longer to the *cancel*.
- `apogee-2un` closes with the plan that implements this record.
- CONTEXT.md **Exchange** loses its "a cancel inside a delegation pool still rolls the whole parent
  Turn back first" clause. **Retained delegation** loses "restored by a cancelled Turn's rollback".
  **Stop (a delegation)** now names cancel as a stop of every running child.
