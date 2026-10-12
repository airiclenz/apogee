---
title: ADR 0033 decision
---

**1. Overlap policy: skip the tick.** A tick that lands while that schedule's previous **Firing**
is still in flight is skipped, with a notice; the next tick fires normally. A schedule's firings
are strictly serial — never queued, never concurrent. The same rule covers a Firing waiting for
the host to go quiescent: at most one Firing is ever pending per schedule.

**2. A Schedule runs in Plan or Auto only, and its Approver is a fail-safe denier.** A schedule is
created in read-only Plan or in confined Auto; any other mode is refused at creation. The
Approver a Firing runs under **denies every request with a recorded reason**, so a gated action
fails visibly and nothing ever parks — the headless-Asker pattern of the **Ask-user** entry in
`CONTEXT.md`, applied to the safety gate. The other interactive delegates are pinned the same
way: a Firing's **Asker** and **Presenter** are `nil`, which simply unregisters `ask_user` and
`present_document`. A Firing's deliverable is a file in the workspace with its path in the
conversation, not a rendezvous with a human.

**3. The creation choice authorizes the mode.** A schedule's mode is chosen explicitly when the
schedule is created and is independent of the host TUI's current mode — a schedule created in
Plan stays Plan after the human switches the interactive session to Auto, and vice versa. Auto
remains gated by the same global Auto-eligibility ladder that gates *launching* in Auto
(confinement capability plus `confine-to-workspace`,
ADR 0012); a
schedule is never silently escalated into it.

**4. Schedule identity lands in browsable `Meta`.** The session record
(ADR 0022) gains two optional
`Meta` fields — schedule id and schedule name — empty on every ordinary session, set on every
Firing. They are browsable data, so `/sessions` can label a Firing under its Schedule without
opening payloads. The addition is backward-compatible in both directions (older builds ignore
unknown fields and never write them), so it carries **no `RecordVersion` bump**.

**5. Each Firing gets fresh context.** A Firing constructs a new Agent and a new session record;
nothing carries over from the previous Firing of the same Schedule. This answers, for the
scheduled-trigger case, the fresh-vs-resumed question ADR 0031 records as open for workflow
nodes; the workflow-node case stays open. No carry-over summary in v1 — an injected summary is
**model-visible content**, which makes it a catalogued Mechanism to be benched, not a scheduler
feature (invariant 4, and see the posture below).

**6. The runner is its own library component.** One Firing — construct a fresh agent, submit the
prompt, drive it to the quiescent boundary, persist the record — is `internal/run`, a package the
scheduler *calls* rather than contains. It is the shared core the deferred `apogee headless`
subcommand reuses unchanged, which is what turns ADR 0031's "first mechanical consequence" from
an intention into code. The scheduler stays **runner-agnostic**: it takes the runner as an
injected seam, so a daemon composes its own without inheriting the TUI's.

**7. v1 UX scope.** Multiple concurrent schedules; the argument form
`/schedule <cycle> [auto] <prompt>` beside the picker path; ticks that arrive while the
interactive session is mid-**Exchange** defer to the quiescent boundary rather than contending
with the live task; and a status surface (bare `/schedule` lists the live schedules with their
cycle, mode, next fire and fired/skipped counts).

These seven are ratified, not provisional: recording them in a decision record before any code was
itself the grill's closing decision.

**Two scopings this ADR states rather than assumes:**

**The save cadence — a Firing saves once, at completion.** ADR 0022's per-Turn cadence is the
*interactive TUI's* crash-safety policy, implemented in the TUI worker; `session.Store` itself
prescribes no cadence. A Firing is bounded and unattended, so at-completion is both honest and
cheap, and a crash costs exactly one Firing rather than a conversation a human is mid-way
through. On a run error the Firing still saves whatever completed, with the interruption visible
in the record's ordinary state — no new record fields. This is a deliberate **scoping** of ADR
0022, recorded here so it is never mistaken for a silent contradiction of it.

**The invariant-4 posture.** ADR 0031's "benchable all the way up" imposes nothing on v1 *because
the scheduler injects no model-visible content* — the prompt is the prompt, the timing decides
only when it runs. That is what makes it acceptable for `internal/run` and `internal/schedule` to
stay `internal/` (ADR 0010), out of
reach of the bench, which is a **separate module** and cannot import `internal/`
(ADR 0001). The moment
a scheduled run injects model-visible content — a preamble, a carry-over summary, chained steps —
that content lands as a catalogued Mechanism (engine-side, already benchable) or the
facade-export question re-opens. It does not get to stay unbenched inside a Driver's library.
