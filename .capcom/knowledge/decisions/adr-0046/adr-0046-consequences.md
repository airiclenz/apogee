---
title: ADR 0046 consequences
---

- `ServerEntry` grows `max-output-tokens:`; `ValidateServers` grows the negative refusal, in the
  message shape its two siblings use.
- The agent's context configuration and the Delegation target both carry the pin, so a child on the
  flagged server derives from that server's numbers (ADR 0045 decision 3's pin, one field wider).
  *(Amended 2026-08-13 — `RebindSpec` carries it as well; see the Amendment below.)*
- The loop gains `maxOutputTokens()` beside `budget()`, applied at both `domain.NewRequest` sites
  BEFORE the pre-request hooks run — which is what makes `SamplingParams`'s "a nil field leaves the
  loop's value untouched" contract true of `MaxTokens` for the first time: a hook that sets it
  still wins.
- The north star (ADR 0031)
  gains a guarantee rather than a constraint: because the cap is engine behaviour, every Driver —
  TUI, bench, a future daemon — inherits it, and no Driver has to bound replies itself. A bench
  Driver sets the pin through the same config surface an operator does.
- `Temperature` stays unset (the server's default). The emergency fold and its retry
  (ADR 0018),
  `defaultReserveFraction`, and the `Allocate` split are all untouched — this record adds a
  statement of the reserve on the wire, not a new allocation policy.
