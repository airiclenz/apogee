---
title: ADR 0093 decision
---

**Every provider call is priced when its usage is recorded, with the price of the server bound to
that call. Unpriced calls are counted, never guessed. A persisted amount always carries the
currency label it was written in.**

1. **One currency label for the whole configuration.** An optional root key `currency:` names it,
   with `USD` as the default. It is a free label, printed exactly as written. apogee never
   converts between currencies, never looks up a rate and never checks the label against a list.
   All servers are priced in that one currency.

2. **A price belongs to a server entry.** An optional `price:` key on a `servers:` entry holds
   `input`, `output` and an optional `cached-input`. Each is the amount in the configured currency
   per 1M tokens. When `cached-input` is missing it defaults to `input`. An entry without `price:`
   is not priced. That is the default, and it is the right answer for a local server.

3. **A call is priced at record time, at the server bound to that call.** The price rides the
   server binding: whatever moves an agent onto a server (a model rebind, a `/server` switch, a
   routed delegation) moves that server's price with it. The usage of each completed call is
   priced at the moment it is recorded:
   - the cached share of the prompt at `cached-input`,
   - the rest of the prompt at `input`,
   - the completion at `output`.

   This covers every call apogee makes: the main loop's Turns, a delegate's calls on its own
   server or on its parent's, and maintenance calls such as a Compaction fold or the emergency
   fold of ADR 0018.
   A price is never applied later to a token total. A `/server` switch or a `price:` edit
   therefore changes the price of later calls and never reprices calls already recorded.

4. **Unpriced calls are counted, never guessed.** A call on a server without `price:` adds its
   tokens as before and adds nothing to the amount. The reading counts priced and unpriced calls
   separately, so every surface can say that an amount covers only part of the session. apogee never borrows a
   price from another server, never assumes zero for a hosted model and never estimates.

5. **The amount is exact integer arithmetic.** Money is kept as a whole number of millionths of
   the currency unit (`int64`). Each call's amount is rounded to whole millionths once, half away
   from zero, when the call is priced. After that every sum is a plain integer addition, so
   summing readings across agents, delegates and saves never drifts. A display rounds only for
   itself and writes nothing back.

6. **A persisted amount carries its currency label.** A session record stores the amount next to
   the label in force when it was written. An amount is never shown under a label other than its
   own, and amounts with different labels are never summed. Changing `currency:` later relabels
   nothing that was already written. A record written before this ADR carries no amount and reads
   as unpriced, never as zero spend.

### Vocabulary

- A **Price** is the configured rate of one server entry (decision 2).
- **Spend (money)** is the priced amount of a set of calls, together with its currency label and
  its priced and unpriced call counts (decisions 3 to 6).
- "Cost" on its own is not used for either. In the glossary it stays **Context cost**'s word
  (ADR 0079). "Spend" on its own keeps its existing meaning of tokens wherever the token reading is
  meant.
