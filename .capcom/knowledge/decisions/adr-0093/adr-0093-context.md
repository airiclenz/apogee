---
title: ADR 0093 context
---

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
  (ADR 0028).
  Delegations can route to the Sub-agent server
  (ADR 0045,
  ADR 0066), while an unrouted
  delegate shares its parent's server. Maintenance calls such as a Compaction fold go to whatever
  server the agent making them is bound to. One token total cannot be priced with one price.
- **A price can change mid-session.** A `price:` edit applies to the running session like every
  other settings edit (ADR 0037,
  including its Amendment 2026-08-24 for the runs a session raises).
- **The word "cost" is taken.** **Context cost**
  (ADR 0079, decision 1) is the token
  report of what apogee itself puts in front of the model. **Server stats** already lists "cost" as
  a word to avoid. Calling money "cost" without qualification would collide with both.
