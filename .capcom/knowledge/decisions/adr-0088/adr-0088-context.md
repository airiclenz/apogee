---
title: ADR 0088 context
---

`esc×2` cancels the Exchange. Stage A of bead `apogee-2un` (2026-09-19) already made the cancel
**settle** rather than abort: the Turns that finished before the stop stay (`Agent.SettleExchange`).
The Turn in flight, though, is still rolled back. When it is a delegation pool, the pool joins every
child and then drops the whole group, finished siblings included (ADR 0013 §5(b), ADR 0039 D4), and
the retained-delegation set returns to its Turn-start value (ADR 0086 D3). The incident behind
`apogee-2un`: two cancels wiped a saved plan and three reviewer reports. Stage B, keeping the
finished siblings, stayed open because it needed a grill on the wire shape.

ADR 0087 makes a Workflow durable, so a cancelled `fan_out` would keep its finished items on disk
while a cancelled `sub_agent` pool beside it threw its finished children away. In the 2026-09-27
grill the owner settled both at once: "reverting is what git was built for; this does not need to
be in apogee."
