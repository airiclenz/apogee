---
title: "Spend (money): the priced amount of a set of provider calls"
---

**Spend (money)**:
The priced amount of a set of provider calls: one call, a delegate's run, or a whole session with its
delegates and background workflows. Every call is priced **when its usage is recorded**, with the
**Price** of the server bound to that call. That covers the main loop's Turns, a delegate on its
own server or its parent's, and maintenance calls such as a Compaction fold. A token total is never
priced after the fact, so a server switch or a `price:` edit never reprices earlier calls. The
amount is kept as whole millionths of the currency unit (`CostMicros`, `int64`), with the priced and
unpriced call counts beside it. A call on an unpriced server adds its tokens and nothing to the
amount, and a surface shows an amount that covers only some of the calls as partial. A session
record stores the amount with the label it was priced under: the configured label when its first
priced call was saved, and that same label on every later save, a resumed session included. A
record with no priced call, or no label, takes the configured label. An amount is never
shown under another label, and amounts with different labels are never summed. It is shown on the
`/usage` pane, the footer, the `/sessions` spend cell and headless output. See
ADR 0093.
_Avoid_: "cost" alone (it collides with Context cost, which is tokens
apogee injects, not money), "spend" unqualified where money is meant (bare "spend" stays the token reading: the
session's `Usage` + `DelegateUsage`, and the token half of the `/sessions` cell), "bill" (apogee
reports the configured price times reported tokens, not what a provider invoices).
