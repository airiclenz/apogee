---
title: ADR 0093 considered and rejected
---

- **Pricing the token totals at display time.** One price times one total is wrong as soon as a
  session has moved servers, routed a delegation or seen a `price:` edit. The total no longer knows
  which tokens ran where.
- **A currency per server.** It would make every sum a conversion, and apogee has no rates and
  should not fetch any. One label keeps every sum a plain addition.
- **A built-in price table for known hosted models.** It would go stale silently and would guess
  for models it does not know. The user's configured price is the only source.
- **Treating an unpriced call as free.** For a hosted server with no `price:` set, that would
  report a confident amount that is too low. Counting unpriced calls separately says what is
  known.
- **Floating-point amounts.** They drift when summed over many small calls and across save and
  load. Integer millionths do not.
