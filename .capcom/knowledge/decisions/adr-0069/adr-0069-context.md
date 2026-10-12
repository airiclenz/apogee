---
title: ADR 0069 context
---

Where a delegation runs is, today, a property of the SESSION.
ADR 0045 put every
delegation on one flagged entry;
ADR 0066 moved that choice onto
the root `sub-agents-server:` key so a human could re-point it mid-session from a picker. Both
records say the same thing about *who* chooses: a human does, once, for every delegation the run
will ever make. ADR 0045 deferred the alternative by name — "model-chosen routing, a `sub_agent`
tool parameter letting the parent model pick a server/model per delegation" — because the static
routing had to exist first; ADR 0066 kept that deferral and did one thing for it, resolving the
target name at exactly ONE consultation point so a per-call parameter could slot in there without
touching the recursion path.

The static routing now exists, and the shape of what it cannot express is clear. A session
routing its delegations to a cheap grunt box gets the cost win on the nine tasks that deserve it
and pays for it on the tenth, where the child is asked to do the actual thinking; a session that
leaves the key unset pays orchestrator tokens for every `git log` a child reads. The whole-session
key can only be set to whichever of those two mistakes is cheaper. And the party that knows which
task is which — that this delegation is "read the config and report the keys" and that one is
"design the migration" — is the model composing the call, in the same reply, at the moment it
writes the task.

What that party lacks is not judgment but facts: nothing in the standing prompt says a second
server exists, what model sits on it, or what the owner keeps it for. The
**Orientation block** (ADR 0023 §6)
is the channel for exactly that kind of host fact, and the `servers:` list is where an owner's
description of a box belongs
(ADR 0036).

A grill on 2026-09-01 settled the arrangement, and this record is that ratification. It ratifies
ADR 0045's deferred item, uses ADR 0066 decision 7's consultation point, and amends
ADR 0039
decision 3 for the one reply that now spans two servers.
