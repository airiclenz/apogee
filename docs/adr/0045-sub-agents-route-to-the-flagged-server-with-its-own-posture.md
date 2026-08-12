---
Status: accepted
---

# Sub-agents route to the flagged server, with its own posture

## Context

A smart model orchestrating a session pays smart-model tokens for every delegated sub-task,
because a sub-agent inherits the parent's Upstream verbatim — the whole inheritance is one
line, `newAgent(childCfg, a.upstream)` (`internal/agent/subagent.go`). Meanwhile the pieces a
cheaper arrangement needs already exist: the `servers:` list is the single definition of every
server the session can talk to ([ADR 0036](0036-the-servers-list-is-the-single-definition-and-the-last-switch-is-the-startup-choice.md)),
the Parallel-agents cap is already per-server
([ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)),
and the heartbeat already knows how to observe a server and apply its facts at a boundary
([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)). What
was missing was a decision: which server takes delegations, how apogee learns that server's
facts, and what a child runs *as* when it lands there — a small model doing grunt work wants
the Mechanisms a smart parent runs without (the parent may sit in Bypass; the child may need
guided decomposition). A grill on 2026-08-11 settled the shape; this record is that
ratification. The child model's thinking-tag profile rides the per-model profile resolution
ratified the same day (ADR 0044, reserved by the per-model-profiles plan, which executes
first — that is why this record is 0045 yet may land in the tree before 0044).

## Decision

**One `servers:` entry may be flagged `sub-agents: true`; every delegation routes to it,
carrying the entry's own bypass/mechanisms posture; a second heartbeat monitor keeps its
facts latched engine-side; an unusable flagged server degrades to today's behavior, loudly.**
Concretely:

**1 — Routing is a flag on the entry, not a new list.** The flagged entry is the **Sub-agent
server**. All delegations at every depth route to it (a routed child's own delegations route
to the same place — identity once there). Absent flag = today's behavior, children share the
parent's Upstream. Two flagged entries are a startup error from `ValidateServers` — a
delegation routes to ONE server, so a second flag is a defect in the file, not a preference
(the duplicate-name reasoning verbatim).

**2 — Posture rides the entry, and applies wherever the routing lands.** The flagged entry
may carry `bypass:` and `mechanisms:` (the existing top-level shapes verbatim): "delegations
to this server run with this posture." A present key replaces the inherited value WHOLE (a
present `mechanisms:` map is the child's entire catalogue — no per-ID merge, which would need
an 'inherit' spelling; the layer-precedence rule and ADR 0044's replace-whole call). An
absent key inherits the parent's LIVE value at spawn, exactly today's rule. Both keys are
refused on an UNflagged entry (loud at startup, the negative-parallel-agents posture).
Posture follows the ROUTING, not the parent's location: a parent that starts on — or
`/server`-switches onto ([ADR 0028](0028-a-server-switch-rehomes-the-session-and-the-first-beat-completes-it.md)) —
the flagged server changes nothing about what its children run with.

**3 — A second heartbeat monitor observes the flagged server; its facts latch as the
Delegation target.** Same beat machinery as the session's own monitor (ADR 0024's
one-code-path rule — no second discovery idiom). Observations latch an engine-side
**Delegation target** spec: endpoint, key, model, per-slot window, Parallel-agents cap, and
the model profile resolved for the OBSERVED model via the per-model resolution (ADR 0044).
The entry's `model:` stays a pin, and the entry GROWS a `context-window:` pin (the
`parallel-agents` idiom; a cloud server reports nothing, so the pins are how one is usable
at all). The latch is mutex-read at spawn and never idle-gated — beats land mid-Exchange,
and each spawn snapshots whatever is current. The engine stays wire-silent
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
the composition root runs the monitor and feeds the latch through a setter; the engine
builds a provider client from a spec exactly as `SwitchUpstream` already does, and a bench
Driver can drive the same setter directly (benchable-all-the-way-up holds).

**4 — Unusable target ⇒ fall back to the parent, loudly.** No beat yet, server down, no
model loaded: the spawn falls back to the parent's Upstream WITH the parent's posture — the
entry's posture rides the entry, and the entry is out of play. One notice per routing STATE
CHANGE (engaged/lost), never per spawn. Work never stalls on a dead grunt box
([ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)'s
degrade-don't-block posture); the cost policy degrades visibly rather than silently.

**5 — The receiving server's cap bounds the fan-out.** Routed ⇒ the flagged entry's cap
(its pin, else its observed `total_slots`, else 1); fallback engaged ⇒ the parent server's
cap, as today. Guided decomposition's batch width follows the same number — ADR 0039's
one-width-everywhere rule, with the width now sourced from the server actually holding the
slots.

**6 — The invariant stays untouched because no DEFAULT moves.** Routing is structural, not a
Mechanism — it steers nothing and executes only calls the model already made, so it is on
under Bypass (ADR 0039's reasoning). The posture keys are owner-written config, and every
absent key inherits today's behavior, so the
[ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)/[ADR 0009](0009-the-ab-decision-rule.md)
bench gate is not owed. A future default-ON posture split re-enters that gate explicitly.

**7 — Minimal surfacing.** The delegation's line shows the child's model name when it
differs from the parent's, and the per-agent usage attribution that already exists covers
the rest — the first debugging clue when routing surprises.

## Considered and rejected

- **A top-level `sub-agents:` block or flat `sub-agent-*` keys** for routing + posture: the
  owner chose the flag ON the entry — the server that takes the work is where the whole
  feature reads, and ADR 0036's single-definition claim stays intact.
- **Discover-at-spawn (cached)**: a second discovery code path beside the heartbeat, first
  delegation pays the latency, and a grunt-side model swap goes stale — rejected for the
  second monitor.
- **Error-result or bounded-wait on an unusable target**: burns a Turn or stalls a fan-out;
  the parent's server is by definition alive, so the fallback floor is always there.
- **Parent server's cap always**: throttles a big grunt box to the smart server's slots and
  can oversubscribe a small one.
- **Per-ID posture merge**: needs an 'inherit vs off' spelling — the ambiguity replace-whole
  exists to avoid.

## Deferred (dated 2026-08-11, not denied)

- **Model-chosen routing** — a `sub_agent` tool parameter letting the parent model pick a
  server/model per delegation. Needs this static routing to exist first, plus prompt-surface
  bench evidence.
- **Launcher actuation for the Sub-agent server** — llama-launcher auto-loading the grunt
  model when delegations start (ADR 0029's latch, second server). Works today when the owner
  pre-loads; actuation is its own grill.

## Consequences

- `ServerEntry` grows `sub-agents:`, `bypass:`, `mechanisms:`, and `context-window:`;
  `ValidateServers` grows the two-flags and posture-on-unflagged refusals.
- The engine gains the Delegation-target latch and its setter; `newChildAgent` consults it
  for upstream, window, profile, and posture instead of inheriting all four from the parent.
- CONTEXT.md's Sub-agent claim "its context window is not reduced" becomes conditional on
  routing — the child works against the TARGET's window once routed.
- The per-model-profiles plan (`docs/plans/2026-08-11 - 05`) must execute before this
  lands; the Delegation target consumes its `profiles.Resolve`.
- A session `/server`-switched ONTO the flagged entry is briefly observed by two monitors —
  harmless (same facts), noted so the wiring may dedupe but need not.
