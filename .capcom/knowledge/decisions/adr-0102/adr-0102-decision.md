---
title: ADR 0102 decision
---

1. **The plan field records ownership.** When a plan takes ownership of an issue, set
   `capcom update <id> --plan '<plan basename>'` (for example
   `2026-09-03 - 01 - driver-parity-and-residues-plan`). The field says the design work is done
   and which plan owns it.
2. **The two queries:** `capcom ready --planned` lists designed issues to hand to
   `/implement-plan`, and `capcom ready --unplanned` lists issues that still need design.
   `capcom list --plan '<prefix>'` lists one plan's issues.
3. **Ownership comes from a plan's item sections.** A plan's *Out of scope* list disowns the
   issues it merely mentions, so ownership is read from the items, never from a grep for ids.
