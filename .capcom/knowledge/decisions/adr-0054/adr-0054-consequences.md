---
title: ADR 0054 consequences
---

- The seam stops growing one field per capability. A new act on an existing family is a method on an
  interface that already exists, and its contract is stated beside its siblings.
- A renderer test can say "the settings seam" and hand over one value. Whole-`Options` construction
  is still how a pane test opens a pane, but the family it is exercising is now one field in it.
- `Options` lost seven fields and gained two, 63 down to 58; the ~120 lines of field documentation
  they carried moved onto the two interfaces without losing a sentence of it.
- The remaining func families named by the review — server, launcher, heartbeat — are the same
  refactor and are sequenced as later items of the same plan. This record is what they follow. The
  first of them, `ServerHost` (server ×4 + heartbeat ×2), is what decision 3a was written from.
- Behaviour is unchanged by construction: every degrade the nil funcs produced is produced by a nil
  host or by the act's own answer, and the whole existing suite passes with no expectation changed.
