---
title: ADR 0068 considered and rejected
---

- **A host-side observer that watches the event stream and names what it sees.** No engine change
  at all, which is the whole of its appeal. But the name has to get BACK to where the run is held
  — the parent's tool result, the saved record, the headless line — and the only road back would
  be a second seam pointing inward, so the engine change is not avoided, only split in two. It
  would also make every Driver re-derive "this delegation is unnamed, and is still running" from
  the event stream, which is a fact the engine holds outright.
- **Naming before the spawn, from the task alone.** It would give the first paint the right name,
  and it would put a completion on the critical path of every delegation — including each of a
  fan-out's members, against a **Parallel agents** cap that exists to keep the server's slots for
  work. Delegated work would wait behind a status line. Concurrency is the point, and a name that
  lands a beat late costs nothing.
- **Always name, overwriting what the call gave.** A uniform naming voice across the umbrella's
  rows, at the price of overruling the party with the most context. It also makes a row that a
  human just read change its name underneath them, which reads as a bug rather than as a polish.
- **A second config key (`auto-name-delegations:`).** A key per naming site, a precedence question
  between them, and a settings row that answers the same question as the row above it. `auto-title`
  already means what this needs it to mean.
