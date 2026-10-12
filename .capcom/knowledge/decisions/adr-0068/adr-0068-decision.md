---
title: ADR 0068 decision
---

**A delegation whose call carried no name gets a short generated one from a single out-of-band
completion on the CHILD's own Upstream, requested through an injected `domain.DelegationNamer`,
fired concurrently with the child and bounded by its lifetime.** Concretely:

**1 — The seam is an injected `domain.DelegationNamer`, and the HOST implements it.** The
Approver precedent, verbatim: `domain.Config` gains `Namer DelegationNamer` beside `Approver`, and
a nil namer means naming never fires — so the bench, an embedder and every existing test compose
exactly as they do today and are unchanged. The engine hands the namer only what the engine knows
(the delegated task, and whether this child is routed) and takes back a name or an error. It
builds no request, holds no client, reads no config key and touches no endpoint:
ADR 0031's
wire-silent invariant holds, and the same composition root that already builds the session's
naming client builds this one beside it.

**2 — The completion runs on the CHILD's own Upstream.** A routed child is named on the
**Sub-agent server** — that entry's endpoint, key, model and effort dialect
(ADR 0066) — and an unrouted
one on the session's server, because the child's Upstream is the machine that is already warm for
this run and the naming call is about its work. The alternative spends orchestrator tokens on a
status line: a session that routes its grunt work to a cheap box precisely to keep the smart box
free would hand that box one extra call per delegation. Retries are off and the thinking channel
is off — a name that needs a second attempt is not a name worth having, and reasoning tokens spent
on a two-to-four-word answer are the failure mode ADR 0022's token backstop already documents.

**3 — It fires concurrently with the child, and dies with it.** The call is issued as the child is
spawned and runs against the child's own lifetime: a name that arrives while the run is live is
applied; one that arrives after the run has finished is DROPPED rather than applied, because a
finished run has already been read under the name it wore. So the first paint of a delegation
still shows the task's first line and the name replaces it a beat later — an improvement that
arrives, never a stall that has to be waited out. Nothing about the child's own work waits on the
naming call, and cancelling the child cancels it.

**4 — It fires ONLY when the call named nothing, and never over a name the model gave.** A name in
the `sub_agent` call is the model's own account of what it delegated and always wins; the
generated name exists for the case where there is nothing to win against. Delegations a
**Mechanism** synthesised are named by the same rule for the same reason — unnamed is unnamed,
whoever composed the call. In the other direction the `name` parameter's schema text is sharpened
to ask for a name, so the generated one stays the fallback rather than the norm: the model naming
its own sub-task is both cheaper and better informed than a second model reading the task cold.

**5 — The gate is the existing `auto-title` key — there is no second one.** One switch over one
act: *let apogee name things for me*. A user who turned automatic naming off did not turn off
session titles specifically, and a second key would make them say the same thing twice and then
keep the two in agreement forever. With the key off, both halves fall back to their heuristics and
neither call is made.

**6 — It is not a Mechanism and not a Turn — but it emits exactly one event.** It fires at no Hook
point, never shapes the primary call, never enters any conversation and adds not one token to any
model's context, so it runs under **Bypass** unchanged and the
ADR 0009 non-inferiority gate does not reach it. What it does emit
— and this is where ADR 0022's addendum gains a sibling — is one `SubAgentNamedEvent` per
generated name, carrying the CHILD run's identity (its depth and the spawning call's ID) and the
name. That addendum's "emits no `TokenEvent` or `UsageEvent`" claim is true of the SESSION title
and stays true of it; the delegation half emits because a name is not a token count. Every Driver
already learns that a delegation exists by reading the event stream, so a rename has to reach
those same readers by that same road — a second, out-of-band channel to say a run changed its name
would be a private wire between the engine and one Driver. No `TokenEvent`, no `UsageEvent`, no
movement of the context gauge: the event says a run is now called something, and nothing else.

**7 — The generated name replaces the retained name everywhere the run is shown, the record
included.** It lands where the given name would have landed, so the collapsed block, the umbrella
row, the run view's breadcrumb, the headless `sub-agent:` line and the saved record all read it
from one place, and a RESUMED session paints the generated name rather than reverting to the
task's first line. A name is not view state (ADR 0063 keeps the open-view stack out of the
record); what the run is *called* is part of the run.

**8 — Every failure is silent.** A nil namer, an endpoint that refuses, a reply with nothing
usable in it, a timeout, a child that finished first: the run keeps the task's first line and
nothing is said to anyone. This is ADR 0022's posture unchanged and for its reason — a cosmetic
maintenance nicety must never nag, and the fallback it fails to is the behaviour that shipped.
