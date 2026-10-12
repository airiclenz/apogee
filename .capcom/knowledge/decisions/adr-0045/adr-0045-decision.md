---
title: ADR 0045 decision
---

**One `servers:` entry may be flagged `sub-agents: true`; every delegation routes to it,
carrying the entry's own bypass/mechanisms posture; a second heartbeat monitor keeps its
facts latched engine-side; an unusable flagged server degrades to today's behavior, loudly.**
Concretely:

**1 — Routing is a flag on the entry, not a new list.** The flagged entry is the **Sub-agent
server**. All delegations at every depth route to it (a routed child's own delegations route
to the same place — identity once there). Absent flag = today's behavior, children share the
parent's Upstream. Two flagged entries are a startup error from `ValidateServers` — a
delegation routes to ONE server, so a second flag is a defect in the file, not a preference
(the duplicate-name reasoning verbatim). (Amended 2026-09-30: the flag is removed —
ADR 0066 decision 1 routes by the
root `sub-agents-server:` key, and `internal/config/configmigrate.go` offers to migrate an old
`sub-agents: true` line.)

**2 — Posture rides the entry, and applies wherever the routing lands.** The flagged entry
may carry `bypass:` and `mechanisms:` (the existing top-level shapes verbatim): "delegations
to this server run with this posture." A present key replaces the inherited value WHOLE (a
present `mechanisms:` map is the child's entire catalogue — no per-ID merge, which would need
an 'inherit' spelling; the layer-precedence rule and ADR 0044's replace-whole call). An
absent key inherits the parent's LIVE value at spawn, exactly today's rule. Both keys are
refused on an UNflagged entry (loud at startup, the negative-parallel-agents posture).
Posture follows the ROUTING, not the parent's location: a parent that starts on — or
`/server`-switches onto (ADR 0028) —
the flagged server changes nothing about what its children run with. (Amended 2026-09-30:
ADR 0066 decision 2 makes
`bypass:` valid on ANY entry, applying whenever that entry is the target. The `mechanisms:` key is
gone with the Mechanism catalogue, ADR 0076;
`ServerEntry` keeps `bypass:` only.)

**3 — A second heartbeat monitor observes the flagged server; its facts latch as the
Delegation target.** Same beat machinery as the session's own monitor (ADR 0024's
one-code-path rule — no second discovery idiom). Observations latch an engine-side
**Delegation target** spec: endpoint, key, model, per-slot window, Parallel-agents cap, the
server's thinking-**effort dialect** (the wire shape ADR 0060 detects per server; amended
2026-08-31 — see the Consequences), and the model profile resolved for the OBSERVED model via
the per-model resolution (ADR 0044).
The entry's `model:` stays a pin, and the entry GROWS a `context-window:` pin (the
`parallel-agents` idiom; a cloud server reports nothing, so the pins are how one is usable
at all). The latch is mutex-read at spawn and never idle-gated — beats land mid-Exchange,
and each spawn snapshots whatever is current. The engine stays wire-silent
(ADR 0031):
the composition root runs the monitor and feeds the latch through a setter; the engine
builds a provider client from a spec exactly as `SwitchUpstream` already does, and a bench
Driver can drive the same setter directly (benchable-all-the-way-up holds).

**4 — Unusable target ⇒ fall back to the parent, loudly.** No beat yet, server down, no
model loaded: the spawn falls back to the parent's Upstream WITH the parent's posture — the
entry's posture rides the entry, and the entry is out of play. One notice per routing STATE
CHANGE (engaged/lost), never per spawn. Work never stalls on a dead grunt box
(ADR 0042's
degrade-don't-block posture); the cost policy degrades visibly rather than silently.

*Amended 2026-09-02 — the NOTICE is debounced; the rule above is not.* An already-routed
server owes `delegationFailureThreshold` (2) CONSECUTIVE unusable beats before the human is
told it is unavailable, and the first usable beat says so again at once — the session
heartbeat's own debounce (ADR 0024 D7) applied to the routing notice, for the same reason:
one timed-out `/v1/models` is not evidence of an absent server, and a pair of notices
alternating between "unavailable" and "routing to" every interval tells the human nothing
they can act on. A cold start still announces on the first unusable beat, and the notice
stays one per routing STATE CHANGE. A `/v1/models` answered with HTTP 429 is SILENCE, not a
verdict: the beat is not landed at all — no push, no notice, the failure run untouched — so
the last verdict stays latched until a non-429 beat replaces it; timeouts, 5xx and refused
connections remain unusable beats, and 401/403 stay unusable.

*What the debounce does NOT change.* The threshold gates words, never beats: decision 3's
"the latch is mutex-read at spawn and never idle-gated — beats land mid-Exchange" stands
verbatim, and every unusable beat still pushes nil into the latch under the lock the moment
it lands. So the fallback rule of this section is untouched — an unusable target is not an
error, and a spawn inside the debounce window still falls back to the parent's Upstream with
the parent's posture (`CONTEXT.md`'s Sub-agent entry says the same). Only the sentence the
human reads waits.

**5 — The receiving server's cap bounds the fan-out.** Routed ⇒ the flagged entry's cap
(its pin, else its observed `total_slots`, else its floor — 4 for a keyed entry, 1 otherwise,
ADR 0039 D2 as amended 2026-09-19); fallback engaged ⇒ the parent server's
cap, as today. Guided decomposition's batch width follows the same number — ADR 0039's
one-width-everywhere rule, with the width now sourced from the server actually holding the
slots.

**6 — The invariant stays untouched because no DEFAULT moves.** Routing is structural, not a
Mechanism — it steers nothing and executes only calls the model already made, so it is on
under Bypass (ADR 0039's reasoning). The posture keys are owner-written config, and every
absent key inherits today's behavior, so the
ADR 0006/ADR 0009
bench gate is not owed. A future default-ON posture split re-enters that gate explicitly.

**7 — Minimal surfacing.** The delegation's line shows the child's model name when it
differs from the parent's, and the per-agent usage attribution that already exists covers
the rest — the first debugging clue when routing surprises.
