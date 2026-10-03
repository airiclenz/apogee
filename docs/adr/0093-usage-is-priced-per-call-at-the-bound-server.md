---
Status: accepted
---

# Usage is priced per call at the bound server

## Context

apogee counts tokens and nothing else. `domain.Usage` sums the calls, prompt tokens (with the
cached share broken out where the server reports one), completion tokens and the server's own
totals. The session record keeps the main agent's reading in `Meta.Usage` and the delegates' sum
beside it in `Meta.DelegateUsage` (`internal/session/store.go`). The `/sessions` row shows their
total as a token count (`internal/tui/sessions.go` `sessionSpendCell`). "Spend" in the code and in
the docs has meant tokens so far.

A user on a paid upstream wants the money too: what this session cost, what the delegates cost,
what the run that just finished cost. apogee cannot learn a price from the provider. No wire it
speaks reports one, and a local server has no price at all. The price has to be configured.

Three things make a price harder to apply than a token count:

- **A session is not on one server.** `/server` moves the session to another entry
  ([ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)).
  Delegations can route to the Sub-agent server
  ([ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md),
  [ADR 0066](0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md)), while an unrouted
  delegate shares its parent's server. Maintenance calls such as a Compaction fold go to whatever
  server the agent making them is bound to. One token total cannot be priced with one price.
- **A price can change mid-session.** A `price:` edit applies to the running session like every
  other settings edit ([ADR 0037](0037-every-settings-edit-applies-to-the-running-session.md),
  including its Amendment 2026-08-24 for the runs a session raises).
- **The word "cost" is taken.** **Context cost**
  ([ADR 0079](0079-context-cost-is-a-first-class-engine-report.md), decision 1) is the token
  report of what apogee itself puts in front of the model. **Server stats** already lists "cost" as
  a word to avoid. Calling money "cost" without qualification would collide with both.

## Decision

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
   fold of [ADR 0018](0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md).
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

## Considered and rejected

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

## Consequences

- `internal/config` gains the root `currency:` key and the per-entry `price:` key. Both are
  documented in the configuration manual.
- `domain.Usage` and the session record's usage carry the integer amount and the priced and
  unpriced call counts. The record carries the currency label as well.
- The engine prices each call where it records usage, from the price on the agent's server
  binding.
- The amount appears on the `/usage` pane, the footer, the `/sessions` spend cell, and headless
  JSON and text output. Each shows the label and marks a partial amount.
- CONTEXT.md defines **Price** and **Spend (money)** beside **Context cost**.
