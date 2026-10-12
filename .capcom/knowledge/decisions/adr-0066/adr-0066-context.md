---
title: ADR 0066 context
---

ADR 0045 marked the
**Sub-agent server** with a flag on the entry itself: one `servers:` entry spelled
`sub-agents: true`, a second one was a startup error, and `bypass:`/`mechanisms:` were refused
anywhere else. That put a SESSION's choice — where this run's delegations go — inside the
definition of a server, which is the one place ADR 0036 keeps free of session state: the
`servers:` list is the single definition of every server, and which of them the session is
bound to is the separate root `server:` key that the last `/server` switch keeps current.

The flag's cost showed up as soon as a config named two candidate grunt boxes. Moving the
delegations from one to the other is two edits of the entries plus a restart, where moving the
SESSION is one `/server` pick; and the postures of the two boxes cannot both be written down,
because the keys that say what delegations to a server run as are refused on the entry that is
not flagged today. A property of a server was being spelled as a property of the run.

A grill on 2026-08-31 settled the replacement, and this record is that ratification. It amends
ADR 0045's first two decisions and leaves the rest of that record — the second heartbeat
monitor, the Delegation-target latch, the loud fallback, the receiving server's cap, the
untouched Bypass invariant, the surfacing — standing exactly as ratified, because the mechanism
they describe is what this key now steers.
