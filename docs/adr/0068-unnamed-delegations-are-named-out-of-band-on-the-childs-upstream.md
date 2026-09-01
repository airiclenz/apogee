---
Status: accepted
Amends: ADR 0022 (addendum — the naming call's "emits no events" claim gains a sibling that emits one)
---

# Unnamed delegations are named out of band, on the child's own Upstream

## Context

One delegation is shown to a human in five places — the collapsed call block, the
`✦ Sub-Agent (N)` umbrella's member rows, a **Run view**'s breadcrumb
([ADR 0063](0063-sub-agent-runs-are-user-addressable-views.md)), the `apogee headless`
`sub-agent:` line, and the **Session record** it is saved into — and every one of them asks the
same question: what is this run called? The `sub_agent` tool takes an OPTIONAL `name`, normalised
at the recursion point into the one line every display can paint (`delegationName`,
`internal/agent/subagent.go`), and when the call leaves it out every display falls back to the
delegated task's first line. That fallback is what a wall of delegations actually reads as: three
rows all opening `You are a repo scout. Read the...` are indistinguishable at the width a status
line has, and the one party that could have told them apart — the model that wrote all three
tasks — is the party that declined to name them.

Session records met this exact problem on 2026-07-31 and settled it
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)'s addendum): a
cosmetic completion, off to the side of the conversation, renames a record shortly after it is
born; `auto-title:` gates it, it fails silently, and the heuristic first-line title stands
underneath it the whole time. Naming a delegation is that same act one level down — the same
trigger (something was born unnamed), the same cost (one small completion), the same failure
posture (the fallback was already good enough to ship with). Re-deciding it from scratch would
have been re-deciding a settled question.

What is *not* the same is where it lives. The session title's call rides a nil-able func seam on
`tui.Options` and never enters the engine, which is correct because a Session is the TUI's object.
A delegation is the ENGINE's object: the engine spawns it, holds the name it was given, decides
what the parent's tool result says, and is the only party that knows when the child started and
when it stopped. And every **Driver** has the same want — headless prints the name, a **Firing**
saves it — so a naming that lived in `tui.Options` would name delegations in one Driver and leave
the other two reading task first lines. The seam has to sit where the run does.

A grill on 2026-09-01 settled the arrangement, and this record is that ratification.

## Decision

**A delegation whose call carried no name gets a short generated one from a single out-of-band
completion on the CHILD's own Upstream, requested through an injected `domain.DelegationNamer`,
fired concurrently with the child and bounded by its lifetime.** Concretely:

**1 — The seam is an injected `domain.DelegationNamer`, and the HOST implements it.** The
Approver precedent, verbatim: `domain.Config` gains `Namer DelegationNamer` beside `Approver`, and
a nil namer means naming never fires — so the bench, an embedder and every existing test compose
exactly as they do today and are unchanged. The engine hands the namer only what the engine knows
(the delegated task, and whether this child is routed) and takes back a name or an error. It
builds no request, holds no client, reads no config key and touches no endpoint:
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)'s
wire-silent invariant holds, and the same composition root that already builds the session's
naming client builds this one beside it.

**2 — The completion runs on the CHILD's own Upstream.** A routed child is named on the
**Sub-agent server** — that entry's endpoint, key, model and effort dialect
([ADR 0066](0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md)) — and an unrouted
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
[ADR 0009](0009-the-ab-decision-rule.md) non-inferiority gate does not reach it. What it does emit
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

## Considered and rejected

- **A host-side observer that watches the event stream and names what it sees.** No engine change
  at all, which is the whole of its appeal. But the name has to get BACK to where the run is held
  — the parent's tool result, the saved record, the headless line — and the only road back would
  be a second seam pointing inward, so the engine change is not avoided, only split in two. It
  would also make every Driver re-derive "this delegation is unnamed, and is still running" from
  the event stream, which is a fact the engine holds outright.
- **Naming before the spawn, from the task alone.** It would give the first paint the right name,
  and it would put a completion on the critical path of every delegation — including each of a
  fan-out's members, against a **Parallel agents** cap that exists to keep the server's slots for
  work. Delegated work would wait behind a status line. Concurrency is the point, and a name that
  lands a beat late costs nothing.
- **Always name, overwriting what the call gave.** A uniform naming voice across the umbrella's
  rows, at the price of overruling the party with the most context. It also makes a row that a
  human just read change its name underneath them, which reads as a bug rather than as a polish.
- **A second config key (`auto-name-delegations:`).** A key per naming site, a precedence question
  between them, and a settings row that answers the same question as the row above it. `auto-title`
  already means what this needs it to mean.

## Consequences

- `internal/domain` grows the naming seam (`DelegationNaming`, `DelegationNamer`, `Config.Namer`)
  and one event, `SubAgentNamedEvent`, which every event-completeness switch and the TUI's fold
  table must cover like any other.
- The engine's spawn path fires the namer for an unnamed child, applies the reply under the same
  lock the run's name is read through, and emits the event; a reply for a run that has ended is
  discarded there.
- `internal/title` stops being "the naming of a session" and becomes the naming of a session **or**
  a delegation: a second prompt builder beside `Prompt`, a shorter sanitiser cap, and its
  package-doc claim about the naming call scoped to the session title with this record beside it.
- The host gains one `DelegationNamer` implementation — a single completion built the
  `probe.Chat` way on the child's Upstream — wired in the composition root and gated by the live
  `auto-title` value, so flipping the key mid-session takes effect on the next delegation.
- Every delegation reader folds the rename rather than re-deriving a name: the TUI head and its
  saved scrollback, the run driver's `SubAgentUsage`, and `apogee headless`'s per-run line.
- ADR 0022's addendum is amended, not superseded: its decisions about the session title stand
  exactly as ratified, and its "not a Turn" reasoning is what this record inherits — with the one
  named exception that a delegation's name is announced as an event.
- **Out of scope, unchanged:** a human renaming a delegation by hand (a run is not a session; there
  is no `^r`), naming at any depth other than the child's own spawn, and any second naming attempt
  for a run whose first one failed.
