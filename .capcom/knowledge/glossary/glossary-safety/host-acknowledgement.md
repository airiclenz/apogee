---
title: "Host acknowledgement (`unconfined-hosts:`): the user's claim that one named machine is disposable"
---

**Host acknowledgement** (`unconfined-hosts:`):
The user's recorded claim that **one named machine** is disposable, so Auto may run unconfined
*there* — the same loosen as `confine-to-workspace: false` at the grain the claim is actually true
at (ADR 0012, amendment 2026-07-21). A global-config-only list of entries (`id`, `acknowledged`,
`note`) matched against the current **host id**; resolution is: explicit
`confine-to-workspace: false` → unconfined everywhere; else a host-id match → unconfined here; else
confined. It exists because the flag is **global** while the claim is **host-specific**, so a
throwaway container's acknowledgement must not follow `~/.apogee/config.yaml` onto a laptop. The
host id is a **safety interlock, not authentication** — it stops an acknowledgement travelling
unnoticed, it does not resist forgery (anyone who can edit the config can write any id) — and it
fails **closed**: an unmatched host is simply confined again, and a machine with no identity to
match by (no hostname *and* no machine id, so its id is shared by every such machine) is refused as
an identity in both directions — the match is ignored and `--save` will not write it. Written only
by an explicit user act (`/confine off --save`); an unknown id is "not this host", never an error.
_Avoid_: "trusted host" (it is not a trust store, and nothing is verified), "whitelist",
"per-host confine-to-workspace" (the key is global; only the acknowledgement is host-scoped).
