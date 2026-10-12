---
title: ADR 0058 considered and rejected
---

Both weighed and declined by the repo owner on 2026-08-24:

- **Keep the axis atomic and announce the drop** — a notice on every `effort:`-only entry
  saying the shipped style was wiped. Turns a silent trap into a narrated one, but the user's
  only remedy is to copy the shipped `style:` into their own entry, which is exactly the 'inherit'
  spelling ADR 0057 closed as obsolete for the other axes.
- **Document the idiom without fixing it** — teach `effort:` entries to always carry `style:`.
  Leaves the most common config edit one omission away from broken parsing, and contradicts
  ADR 0050's reason for putting effort on the profile at all.
