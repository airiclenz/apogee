---
title: ADR 0066 decision
---

**A root `sub-agents-server:` key names the `servers:` entry every delegation routes to, the
`/sub-agents-server` picker moves it in a running session, and the entry flag is removed.**
Concretely (*amended 2026-09-01 —
ADR 0069 makes "every delegation" mean
every delegation whose call carries no `run_on`, which under the default
`sub-agents-choice: fixed` is still every delegation there is*):

**1 — The target is a root key, not a flag on an entry — this amends ADR 0045 decision 1.**
`sub-agents-server: <name>` names one entry of the `servers:` list; every delegation at every
depth routes there — *scoped 2026-09-01 by
ADR 0069 to a delegation whose call
carries no `run_on`, which is every delegation under the default `sub-agents-choice: fixed`; an
explicit `run_on: session` runs unrouted with the parent's posture* — and a routed child's own
delegations route to the same place (identity once there, unchanged, and a child's own tool
carries no `run_on` at all). Every entry is an eligible target, including the one the session itself runs
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

> **Amended 2026-09-01 by ADR 0069.** The
> deferral this decision recorded is over, and the consultation point it reserved is now used.
> Model-chosen routing is ratified in a two-seat shape: with root `sub-agents-choice: model` the
> top-level model's `sub_agent` call may carry `run_on: session | sub-agents-server`, resolved at
> that same single consultation point, at depth 0 only. Routing stays human-chosen *by default* —
> `sub-agents-choice:` defaults to `fixed`, and with it the parameter is not in the schema at all —
> so this decision's claim holds wherever the key is unset, and the file and the picker remain the
> only doors that move the target itself.
