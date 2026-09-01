---
Status: accepted
Amends: ADR 0045 (Deferred: model-chosen routing), ADR 0066 (decision 7), ADR 0039 (decision 3 — mixed-seat width)
---

# The top-level model picks the delegation seat

## Context

Where a delegation runs is, today, a property of the SESSION.
[ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md) put every
delegation on one flagged entry;
[ADR 0066](0066-sub-agent-routing-follows-the-sub-agents-server-root-key.md) moved that choice onto
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
**Orientation block** ([ADR 0023](0023-the-system-prompt-is-a-configured-template-rendered-per-request.md) §6)
is the channel for exactly that kind of host fact, and the `servers:` list is where an owner's
description of a box belongs
([ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)).

A grill on 2026-09-01 settled the arrangement, and this record is that ratification. It ratifies
ADR 0045's deferred item, uses ADR 0066 decision 7's consultation point, and amends
[ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
decision 3 for the one reply that now spans two servers.

## Decision

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
[ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
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

**7 — A mixed reply is sized by the smaller cap; a single-seat reply keeps its seat's cap — this
amends [ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
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
[ADR 0063](0063-sub-agent-runs-are-user-addressable-views.md) D3 makes for a steered child, and
the note sits directly beneath the work it qualifies rather than in front of it where it would
displace the child's first line. Where both apply, D3's steered trailer stays the final line and
the note goes immediately above it: D3 ratified "final line" and this record does not take it.
The routing-state notice ADR 0045 §4 already emits to the human is unchanged and is not repeated
per spawn — the note is for the MODEL, once, on the result of the call that asked.

## Considered and rejected

- **Let the model name a `servers:` entry.** The obvious generalisation, and the one that turns
  the tool schema into a second reader of the config file: entry names change, an entry the model
  names may not exist, and the model would need the whole list in its prompt to choose from it.
  It also quietly grants a delegation a machine the session was never bound to, which is a
  privilege question ADR 0045 never opened. Two seats are the two things that already exist.
- **A closed set of tiers (`fast` / `smart` / `local`) mapped to entries.** A vocabulary the
  owner would have to translate their servers into, kept in agreement with the `servers:` list
  forever, and wrong the first time someone runs three boxes that are all "fast". The owner's own
  `description:` says more and needs no mapping.
- **An error result when an ask cannot be honoured.** It burns a Turn to tell the model something
  it can be told in one line of a result that also contains the finished work. ADR 0045 §4
  rejected error-or-wait for the static case on the same grounds; a per-call ask does not change
  the arithmetic, and the parent's own server is by definition alive.
- **Two pools, one per seat, each running to its own cap.** More throughput on a mixed reply, at
  the price of slot accounting across two servers and two widths inside one Turn — the shape
  ADR 0039 avoided by staying depth-0 and single-width. Revisit only with evidence that mixed
  replies are common AND that the smaller cap is the bottleneck.
- **Spelling the parameter `seat:` (or the key `sub-agent-seat:`).** "Seat" is the right word for
  the CONTEXT.md term — the pair of places a delegation may run — and the wrong word on the wire,
  where the model reads a single line of schema text and `run_on: session` states an action while
  `seat: session` states a noun the model has to have been taught. The domain keeps the term; the
  tool and the config keys do not spell it.
- **A per-call model or endpoint override.** The far end of the same road: a delegation would
  stop being "a child of this run on one of two known boxes" and become an arbitrary outbound
  request the engine composes from model-supplied strings — the wire-silent engine
  ([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md))
  inverted.

## Consequences

- The `sub_agent` tool grows an optional `run_on` enum (`session` | `sub-agents-server`), present
  in the schema only at depth 0 and only under `sub-agents-choice: model`; the roster surface
  ([ADR 0057](0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md)) is
  untouched — this is a variant of one tool's schema, not a second tool.
- `ServerEntry` grows `description:` (free text, valid on any entry, no validation beyond its
  type); `fileConfig`/`Options` grow `SubAgentsChoice`, defaulting to `fixed`, and the settings
  registry grows a row for it. `/sub-agents-server`'s picker rows show the description; `/server`'s
  do not — that picker answers a different question.
- The engine resolves the seat at spawn, at ADR 0066 decision 7's single consultation point, which
  is now used rather than reserved; the recursion path is untouched, and a child spawns with no
  seat parameter in its own schema.
- Fan-out width gains one case: a depth-0 reply whose `sub_agent` calls do not all share a seat is
  batched at `min(session cap, target cap)`. Everything else about ADR 0039 — the pin-else-
  discover-else-1 cap, one width per reply, the depth-0 bound, per-child failure independence —
  stands.
- The Orientation block gains a Delegations line under `sub-agents-choice: model`, built from
  session-constant facts and omitted entirely under `fixed`, so ADR 0023 §6's per-session-constant
  and omit-what-you-do-not-have rules both hold.
- A delegation result may carry one appended note line ahead of ADR 0063 D3's trailer; readers
  that assert on the last line of a result assert on the trailer where one exists, and on the note
  otherwise.
- The invariant is untouched: seat choice steers nothing, injects nothing and executes only calls
  the model already made, so it is on under **Bypass** and owes no
  [ADR 0009](0009-the-ab-decision-rule.md) gate — ADR 0045 decision 6's reasoning verbatim. The
  gate's default (`fixed`) means no default moves either way.
- ADR 0045's deferred **Model-chosen routing** item is ratified here rather than left open; its
  "plus prompt-surface bench evidence" precondition is what `description:` and the Delegations
  line answer, and the bench arm for the prompt surface belongs to apogee-sim. **Launcher
  actuation stays deferred**, unchanged.
- **Out of scope, unchanged:** routing to arbitrary entries or tiers (one Sub-agent server stays),
  per-entry heartbeat monitors, launcher actuation, and any per-call choice below depth 0.
