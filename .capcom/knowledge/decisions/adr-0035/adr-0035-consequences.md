---
title: ADR 0035 consequences
---

- ADR 0012's authorized-write set is now "`/confine off --save`, plus registry-declared editable
  scalars edited in the settings surface" — every member of it still a distinct affirmative user
  act that names the file and entry it changed. Nothing apogee decides on its own initiative may
  join that set without amending this record.
- `CONTEXT.md` gains **Settings surface** as a canonical term, and it carries the reconciliation:
  the standing "apogee never writes your config" claims mean never **unprompted**. A
  settings-screen edit is user-initiated and names its entry; probe still prints rather than
  writes (ADR 0021 untouched); seeding still never overwrites (ADR 0023 §8 untouched).
- The key registry becomes the single home for config **source metadata**, and the bijection guard
  makes "the schema grew a key" a test failure rather than a screen that quietly omits it. It is
  also the groundwork the deferred table-driven-resolution rewrite will build on.
- `layout.md` gains a **full-height pane class** with its own budget rule, joining the frame's
  give-way order — the first surface allowed to take the transcript's entire budget.
- Most edits are honest about applying at **next launch**. That marker is the visible cost of
  decision 9, and it is the correct cost: the alternative is a screen that appears to change a
  running session and does not.
- The seeded template stays the documentation of record for every key (the registry's descriptions
  are condensed from it), so template comments and registry rows must be kept in step by whoever
  adds a key — the guard pins the *paths*, not the prose.
- `ISSUES.md`'s settings-screen entry closes: the grilling it asked for is this record.
