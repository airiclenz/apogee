---
title: "Price: the configured per-1M-token rate of one `servers:` entry"
---

**Price**:
The configured rate of one `servers:` entry, set by its optional `price:` key: `input`, `output`
and an optional `cached-input` (defaulting to `input`), each the amount per 1M tokens in the one
currency the root `currency:` key names (default `USD`, a free label printed as written and never
converted). An entry without `price:` is **not priced**, which is the default and the right answer
for a local server. The price rides the server binding, so whatever moves an agent onto a server (a
rebind, a `/server` switch, a routed delegation) moves that server's price with it, and a `price:`
edit applies to the running session's next call. It is never guessed: apogee has no price table and
never borrows one entry's price for another. See
ADR 0093.
_Avoid_: "cost" (that is **Context cost**, ADR 0079's
token report of what apogee injects), "rate card", "pricing table" (there is no built-in table,
only what the user configured).
