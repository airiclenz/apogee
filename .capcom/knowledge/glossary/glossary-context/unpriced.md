---
title: "Unpriced: a provider call with no Price to apply"
---

**Unpriced**:
A provider call with no **Price** to apply: one made on a `servers:` entry without `price:` (the
default, and right for a local server), or one a resumed session makes under a currency label other
than its record's (`domain.Usage.Unpriced`). Its tokens and the call itself are counted, its money
is not: apogee never borrows another server's price, never assumes zero for a hosted model and
never estimates. The reading keeps priced and unpriced call counts apart, so a **Spend (money)**
amount that covers only some of a session's calls reads as **partial**, and a record written before
pricing existed reads as unpriced, never as zero spend. See
ADR 0093.
_Avoid_: "free" / "zero-cost" (an unpriced call has no known amount, not an amount of zero),
"estimated".
