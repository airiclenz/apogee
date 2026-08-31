---
Status: accepted
Amends: ADR 0045 (decisions 1-2)
---

# Sub-agent routing follows the `sub-agents-server` root key

## Context

[ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md) marked the
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

## Decision

**A root `sub-agents-server:` key names the `servers:` entry every delegation routes to, the
`/sub-agents-server` picker moves it in a running session, and the entry flag is removed.**
Concretely:

**1 — The target is a root key, not a flag on an entry — this amends ADR 0045 decision 1.**
`sub-agents-server: <name>` names one entry of the `servers:` list; every delegation at every
depth routes there, and a routed child's own delegations route to the same place (identity once
there, unchanged). Every entry is an eligible target, including the one the session itself runs
on. An ABSENT or empty key is the pre-ADR-0045 behaviour — children share the parent's Upstream —
so the feature is opt-in and the floor is the one ADR 0045 already promised. A name no entry
carries is **not** a startup refusal, which is the `server:` key's own precedent (ADR 0036): one
notice says which name went missing and lists the names the file does carry, and routing degrades
to the session's own server. The `sub-agents: true` flag is REMOVED rather than deprecated —
nothing decodes it — and with it go `ValidateServers`' two-flags refusal and CONTEXT.md's claim
that a second flag is a startup error.

**2 — Posture is valid on ANY entry and applies whenever that entry is the target — this amends
ADR 0045 decision 2.** `bypass:` and `mechanisms:` on a `servers:` entry say what DELEGATIONS to
that server run as, never what the session runs as. ADR 0045 refused them on an unflagged entry;
with a target that moves, every entry is a candidate and that refusal would forbid writing down
the posture of the box you are about to switch to. Everything else about the keys is unchanged: a
present key replaces the inherited value WHOLE, an absent key inherits the parent's live value at
spawn, and the posture follows the ROUTING rather than where the parent happens to be.

**3 — Retargeting is allowed while the agent runs, and already-spawned children keep their
server.** The pick moves where the delegations spawned AFTER it go; a child in flight snapshotted
its target at spawn (ADR 0045 decision 3's mutex-read latch), so nothing can reach one. The seam
is deliberately not idle-gated, matching the latch beneath it. It forgets the routing state along
with the old server, so the new server's first beat announces the route exactly once through the
notice path every other routing change uses — the seam itself announces nothing, because saying it
at the pick would state a route not yet in force.

**4 — A name from the picker is refused; a name from the file degrades.** The one place the seam
and the key differ, and deliberately: a name in a config file is written once and read for months,
so refusing to load over it would be hostage-taking, while a name handed over by a human picking a
row in this session can be answered honestly — say so, change nothing. An entry whose
`mechanisms:` map is defective is refused whole at both doors for the same reason, so a posture
never lands half-installed.

**5 — A pick is recorded, and the recording is not a config change.** The chosen name is spliced
back into `config.yaml` as `sub-agents-server:` — the `server:` idiom verbatim (comments and
layout untouched, re-parsed and compared before it replaces the file, landing under its commented
example per ADR 0035). apogee's own write is exempted from the file watcher the way every
self-made write is (ADR 0041 decision 8), so the pick does not come back as a config change; a
HAND-edited key still does what a hand edit has always done. The settings pane carries the key as
a READ-ONLY row: `⏎` on a server-kind row switches the SESSION's upstream, and this key is not
that, so `/sub-agents-server` is the single way it changes.

**6 — A config still carrying the retired flag gets a start-up migration offer.** The
key-migration precedent: an unasked-for pane under a notice naming the entries, offered after the
more urgent start-up questions and never beside them, with two rows — "move it" (write
`sub-agents-server: <name>`, drop the flag, and re-point THIS session's delegations at it) and
"not now" (leave the file alone; the offer comes back at the next start-up). There is no "never"
row: the flag is dead weight its owner removes once, and an answer that made the question
permanent would preserve a line that does nothing.

**7 — Model-chosen routing stays deferred, and now has one consultation point.** ADR 0045's
deferred item is unchanged in status: routing is human-chosen, by the file or by the picker. What
changes is that the wiring consults the target name in exactly ONE place, so a future per-call
`sub_agent` parameter slots in there without touching the recursion path.

## Considered and rejected

- **Keep the flag and add the root key beside it.** Two spellings for one fact, and a conflict
  rule (which wins?) that ADR 0036's single-definition claim exists to avoid. The flag goes.
- **Refuse a stale `sub-agents-server:` name at startup.** A name written once and read for
  months would take the whole config hostage over a typo; the notice plus the degrade says more
  and costs nothing.
- **A CLI flag or env var for the target.** The target is a recorded session choice, like
  `server:`; the file and the picker are its two doors, and a third would need a precedence rule
  for a key that already moves mid-session.
- **A `· current` marker on the picker's rows.** `/server`'s third cell means "the session is
  bound here", which is the one thing a delegation target is not — the mark would tell a human
  their delegations run on the box they are talking to, on every session where they do not.
- **A "never for this entry" row on the migration offer.** It would persist a line that nothing
  decodes.

## Consequences

- `ServerEntry` loses `sub-agents:`; `ValidateServers` loses both the two-flags refusal and the
  posture-on-an-unflagged-entry refusal. `fileConfig` and `Options` grow `SubAgentsServer`, the
  settings registry grows a non-editable `KindServer` row, and the seeded `config.yaml` documents
  the key beside `server:`.
- The delegation wiring grows a retarget seam and takes the `servers:` list as a reader rather
  than a snapshot: a retarget resolves its name against the list as it stands then.
- The TUI gains `/sub-agents-server` — `/server`'s grammar, whose bare form opens a picker and
  which is allowed while the model works — and a second unasked-for start-up pane.
- CONTEXT.md's **Sub-agent server** term is no longer "the flagged entry" but "the entry the root
  key names", and its "at most one entry may carry the flag" sentence is gone.
- ADR 0045 is amended, not superseded: decisions 3-7 of that record are the mechanism this key
  steers, and its deferred items stay deferred.
