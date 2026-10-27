---
title: ADR 0069 decision
---

**With `sub-agents-choice: model`, the top-level model picks per delegation which of two seats the
child runs on — the session's own server or the Sub-agent server — through an optional `run_on`
parameter on the `sub_agent` tool, guided by a per-entry `description:` the Orientation block
relays for both seats.** Concretely:

**1 — There are exactly two seats, and no third spelling of "where".** `run_on` takes `session` or
`sub-agents-server`, and nothing else: the two places a delegation can already run today. The
**Delegation seat** is the domain term for that pair; the tool never names a `servers:` entry, a
model, a tier or an endpoint. The model is being asked which KIND of box this task wants, which it
can answer from the task it just wrote, and not which machine to dial, which it cannot answer at
all without becoming the config file's second reader. This also keeps the feature's blast radius
at one boolean: everything ADR 0045 and ADR 0066 decided about how a routed child is built —
posture, window, profile, effort dialect, the latched **Delegation target**, the second heartbeat
monitor — is what the `sub-agents-server` seat still means, unchanged.

**2 — An absent `run_on` is the root key's rule, so nothing moves for anyone who says nothing.**
No parameter on the call ⇒ the delegation routes exactly as ADR 0066 decision 1 says it does:
to the entry `sub-agents-server:` names, or, with that key absent or empty, to the parent's own
Upstream. The parameter is additive on top of a rule that keeps running underneath it, which is
also why a model that ignores the parameter entirely is not a degraded model — it is a model
getting today's behaviour.

**3 — The choice is offered at depth 0 only.** Only the top-level agent's `sub_agent` schema
carries `run_on`; a routed child's own tool does not have the parameter at all, so a child cannot
promote its grandchildren onto the smart box or bounce them back. This is
ADR 0039
decision 3's depth-0 rule and ADR 0045 decision 1's identity-once-there rule agreeing: below the
first hop, a delegation keeps the seat it landed on. Removing the parameter from the child's
schema rather than accepting and ignoring it is the honest form — a schema that advertises a knob
it discards teaches the model a lie about its own leverage.

**4 — The gate is a root `sub-agents-choice: fixed|model`, defaulting to `fixed`.** With `fixed`
the parameter is not in the schema, the Orientation block says nothing about seats, and the run is
byte-identical to one built before this record. An owner who has deliberately pointed every
delegation at one box does not want a model second-guessing that per call, and an owner who has
not thought about it at all should not have their prompt surface grow. The key is a root key, not
a per-entry one, because it describes how the RUN chooses rather than what a server is — the
distinction ADR 0066 drew when it moved the target off the entry.

**5 — The marker is a free-text `description:` per `servers:` entry, and the Orientation block
describes both seats symmetrically.** The owner writes what a box is for in the one place a box is
defined; the engine relays it on a **Delegations** line that names the session's seat and the
Sub-agent seat side by side — each with its entry name, its `description:`, the entry's `model:`
pin and the bound session model. Symmetry is the point: a line that described only the far seat
would read as an instruction to use it, and the model choosing "keep it here" needs the same facts
as the model choosing to send it away. Free text rather than an enum of capabilities, because the
owner's own sentence — "small fast box, good for greps and file reads, weak at code" — is the
prompt, and any vocabulary we invented would be a worse one.

**6 — The Delegations line states session-constant facts only, so ADR 0023 §6 is KEPT.** Entry
name, `description:`, the entry's `model:` pin and the bound session model: every one of them is
constant within a session and moves only on a human door — `/server`, `/model`,
`/sub-agents-server`. No availability state, no "unavailable now", no beat-driven text of any
kind. The block is prefix-KV-cache stable by construction and this line does not become the one
exception; the scratch-dir move is the precedent for a fact that changes only at a session
boundary. An unusable target is not withheld from the model — it is reported by decision 8's note,
after the fact, where it costs no cache.

> **Superseded in part 2026-09-20 (plan `2026-09-20 - 00`, item 4; ADR 0039's 2026-09-20 note).** The
> orientation block now carries a `Delegation bounds:` line whose first clause states a width — `up
> to W run at once` — and the width is a number a heartbeat may discover. "No beat-driven text of any
> kind" is kept for the Delegations line and relaxed for ONE latched-per-seat number
> (`statedDelegationWidth`): the far server's cap once it has stated one since the seat last moved,
> else the session server's. It moves only on the human's doors (`/server`, `/sub-agents-server`) and
> on a cap's FIRST statement — a heartbeat's slot discovery, the far server's first stated cap — and
> never on a target-down beat, so a flapping far server cannot flap the prompt; the beat that re-states
> an unchanged cap re-renders the block byte-identical. Availability state stays out of the block, as
> written above.

> **Note 2026-09-24 (plan `2026-09-24 - 00`, item 13).** Because the seat door FORGETS the far width
> and a usable target STATES it, the two are order-sensitive wherever both are applied together.
> `lateEngine.Bind` (`cmd/apogee/wire_engine.go`) replays the pending seat BEFORE the pending
> target, so a session bound late states the pending target's width from its first request; the
> reverse order wiped the width the target had just stated and told the model the session width
> until the next heartbeat beat.

> **Note 2026-09-24 (the same order, on the headless path).** `run.Once` (`internal/run/run.go`)
> applies a Firing's `Spec.DelegationSeat` BEFORE its `Spec.DelegationTarget` for the same reason.
> A Firing has no heartbeat, so the reverse order, which it had, told the model the session width
> for the whole run rather than for one beat.

> **Note 2026-10-27 (plan `2026-10-27 - 00`; `adr-0076-amend-1027`).** Seat-before-target is now
> structural inside `agent.New` and `agent.Resume`: the delegation seat and target ride the
> construction seed, and construction applies the seed's seat and then its target, before it
> announces `session-started`, so the first request states the target's width. Drivers no longer
> order them — `lateEngine.Bind` and `run.Once` seed both instead of applying them after
> construction, and the two notes above describe the order construction now keeps for them.

**7 — A mixed reply is sized by the smaller cap; a single-seat reply keeps its seat's cap — this
amends ADR 0039
decision 3.** One depth-0 reply may now fan out to both seats at once, which ADR 0039 never had to
consider: its cap was the cap of the one server every child was going to. When all of a reply's
children share a seat the width is that seat's **Parallel agents** cap, exactly as before. When
they do not, the batch is sized by `min(session cap, target cap)` — one width for the reply,
ADR 0039 decision 1's one-width rule preserved, chosen small enough that neither server is
oversubscribed. Per-seat pools that let each side run to its own cap would be two widths in one
Turn and slot accounting to keep them apart; the deadlock ADR 0039 designed away by staying at
depth 0 is not worth re-opening for the tail of a mixed reply.

**8 — `run_on: session` in a routed session runs unrouted, with the parent's posture.** The seat
carries everything the seat means, and ADR 0045 decision 2's rule — posture follows the ROUTING,
not the parent's location — is what makes that unambiguous in both directions. A child asked for
the session seat is built the way a child of an unrouted session is built: the parent's Upstream,
the parent's live bypass/mechanisms, the parent's window and dialect. It does not pick up the
Sub-agent entry's `bypass:`/`mechanisms:` overrides, because those keys say what delegations *to
that server* run as and this delegation went elsewhere.

**9 — An unusable `sub-agents-server` ask falls back per ADR 0045 §4 and says so in the result.**
No beat yet, server down, no model loaded, no such entry: the spawn degrades to the session
server exactly as static routing already degrades, and the run happens. On top of that fallback —
and only for a child whose call ASKED for the far seat — the result body gains
`note: ran on the session server — the sub-agents server was unavailable` as its LAST line,
appended, never prefixed. A parent that made a routing decision and had it overruled must be able
to see that from the result alone, the same argument
ADR 0063 D3 makes for a steered child, and
the note sits directly beneath the work it qualifies rather than in front of it where it would
displace the child's first line. Where both apply, D3's steered trailer stays the final line and
the note goes immediately above it: D3 ratified "final line" and this record does not take it.
The routing-state notice ADR 0045 §4 already emits to the human is unchanged and is not repeated
per spawn — the note is for the MODEL, once, on the result of the call that asked.
