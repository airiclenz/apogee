---
title: "Sub-agent server: the `servers:` entry every delegation routes to"
---

**Sub-agent server**:
The `servers:` entry the root `sub-agents-server:` key names — the server **every delegation
routes to**, so a cheap grunt model does delegated work while a smarter model orchestrates.
Every entry is an eligible target, including the one the session itself runs on, and the choice
**moves in a running session**: `/sub-agents-server` re-points the delegations spawned from then
on and records the name back into the file, while children already in flight keep the server they
were spawned against. ANY entry may also carry the children's **posture** — the `bypass:`
override that applies to every child *routed there* whenever that entry is the
target (present key replaces whole, absent key inherits the parent's live value; where the
*parent* runs is irrelevant). Delegations there also speak that server's own
Thinking-effort **wire dialect** — its `effort-dialect:` pin, else
what it advertises — because the dialect is a property of the server a request lands on; an entry
that names neither leaves its delegates speaking the SESSION server's dialect, and apogee says so
once when routing engages. An unset key means today's behavior: children share the parent's
Upstream. A name no entry carries is not a startup error — apogee says which name went missing,
names the entries the file does carry, and routes to the session's own server. ANY entry may also
carry a free-text `description:` — what the owner keeps that box for — which the **Orientation
block**'s Delegations line relays and the `/sub-agents-server` picker shows in its rows. With root
`sub-agents-choice: model` the key stops being the last word: a depth-0 `sub_agent` call may carry
`run_on: session | sub-agents-server` and pick its own **Delegation seat**, and the key's rule
covers every call that carries no `run_on` — which, under the default `sub-agents-choice: fixed`,
is all of them. Ratified 2026-08-11, re-shaped as a root key 2026-08-31, opened to the model
2026-09-01
(ADR 0045,
ADR 0066,
ADR 0069).
_Avoid_: "grunt server" (colloquial), "worker server", "delegation server" (too close to the
**Delegation target**, which is the latched spec rather than the entry), "flagged server" (there
is no flag any more — the root key names it).
