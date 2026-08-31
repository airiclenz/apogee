---
Status: accepted
---

# The scheduler is a library and the TUI is its first Driver surface

## Context

`TODO.md`'s `/schedule` bullet asks for a standing instruction: a prompt re-run every time a
chosen cycle passes while apogee is open, each run a fresh context saved as its own session,
`/schedule-stop` to end it. The 2026-08-03 north-star grill enriched that brief with a layering
constraint and left five branch points explicitly open — overlap policy, which Agent modes a
schedule may use, whether schedule identity lands in the session record's browsable `Meta`,
fresh-vs-resumed context per run, and where the thing that actually performs a run lives. A
second grill the same day settled them; this record is that settlement.

[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
makes one direction non-negotiable. The engine must stay sufficient for *any* **Driver**, and the
first mechanical consequence it names is the deferred **`apogee headless`** runner. A scheduler
welded into the TUI — cycle timing, overlap policy and session grouping living in a Bubble Tea
model — is precisely the silent door-closing that ADR was written against: the daemon it
anticipates would have to rebuild every when-and-how decision from scratch, and nothing would
visibly break in the meantime to say so.

The scheduler is also the first Apogee surface that runs the loop with **no human present**. That
makes ADR 0031's invariant 2 (the Approver seam is wait-tolerant, parking is a Driver's
composition) load-bearing in the other direction: an unattended run that parks on an approval
waits forever, and under any sane overlap policy a schedule whose run never finishes stops firing
altogether. The unattended host's composition must therefore be "never park", stated once, in the
runner.

## Decision

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
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)); a
schedule is never silently escalated into it.

**4. Schedule identity lands in browsable `Meta`.** The session record
([ADR 0022](0022-sessions-persist-per-turn-as-dual-representation-records.md)) gains two optional
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
stay `internal/` ([ADR 0010](0010-package-layout-domain-core-and-thin-root-facade.md)), out of
reach of the bench, which is a **separate module** and cannot import `internal/`
([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)). The moment
a scheduled run injects model-visible content — a preamble, a carry-over summary, chained steps —
that content lands as a catalogued Mechanism (engine-side, already benchable) or the
facade-export question re-opens. It does not get to stay unbenched inside a Driver's library.

## Considered options

- **Queue the ticks, or run a schedule's firings concurrently** — rejected. Queueing turns a slow
  Firing into an unbounded backlog that fires long after the moment the schedule was about, and
  concurrency multiplies contention on a single-slot local server for a benefit nobody asked for.
  Skipping is the only policy whose failure mode is legible: the next tick behaves normally.
- **Allow Ask-Before (or Allow-Edits) schedules** — rejected for v1. Their whole value is a human
  at the gate, and a Firing has none: the run would park on the wait-tolerant Approver until
  somebody noticed, holding the schedule's one serial slot and silently starving every subsequent
  tick. Making that safe needs a cross-session approval surface — a real feature, not a mode flag.
- **Carry a summary of the previous Firing into the next** — rejected. It is model-visible content
  injected by scaffolding, which is exactly the thing invariant 4 requires be benchable before it
  ships. Decision 5 keeps v1 honest and leaves the door open at the right layer.
- **Keep the runner private to the scheduler** — rejected. It is the smaller change today and the
  duplication that ADR 0031's headless consequence exists to prevent tomorrow: the daemon and the
  `headless` subcommand would each re-implement "one run, saved as a session", and drift.
- **Cap a schedule's mode at the host TUI's current mode** — rejected. It sounds conservative but
  makes a schedule's authority a function of whatever mode the human happened to leave the
  session in, which is neither predictable nor a real safety property. Auto-eligibility is the
  actual gate (decision 3); the explicit choice at creation is the actual authorization.

## Consequences

- **A three-layer split.** `internal/run` performs one Firing; `internal/schedule` owns every
  when-and-how decision (cycles, the skip policy, the quiescence gate, lifecycle) behind injected
  seams and a clock; the TUI owns nothing but input and display — commands, pickers, status rows
  and notices ([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md) for the command
  namespace, [ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md) for the
  seam shape).
- **`apogee headless` shrinks to a thin CLI over `internal/run`.** When it ships, its plan is
  argument parsing and exit codes, not a second runner.
- **A Firing's record resumes without scrollback.** v1's runner records no transcript blob, so
  resuming a Firing from `/sessions` takes ADR 0022's documented degrade path ("resumed, no
  scrollback recorded") — an engine-only resume, correct and already handled, not a defect.
  *(Amended 2026-08-31: no longer true. Now that the transcript codec is Driver-neutral
  (`internal/session/transcript.go`), the runner folds its own scrollback from the Event stream
  and fills `Record.Transcript` — `internal/run/transcript.go` — so a Firing's record REPLAYS.
  The fold is narrower than the TUI's by design: stream facts only, no presenter verdicts. The
  degrade path stands for records written before this and for a blob that could not be encoded.)*
- **A Firing runs without MCP tools.** MCP connections are live host state re-established per
  session (ADR 0022, [ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md) —
  external effects are non-forkable); handing the TUI's live connections to a concurrent second
  agent is exactly the fork those records forbid.
- **The gate releases on engine idle, not on a Turn boundary.** An Exchange spans Turns
  ([ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md)), so releasing a
  deferred Firing at `StatusTurnComplete` can land it mid-Exchange, contending with the very task
  the deferral exists to protect. Policy lives in the scheduler; the busy/idle state stays the
  TUI's to publish.
- **The fresh-per-firing answer feeds back into ADR 0031**, whose open-questions list now points
  here for the scheduled-trigger half.
- **Schedules die with the TUI.** Nothing persists to config, and that is the honest v1 promise —
  "while apogee is open, re-run this every N minutes". Durable schedules that survive quit are
  the future daemon's value-add over this same library, so building it here closes no door.
- **`CONTEXT.md` gains `Schedule` and `Firing`** as canonical terms, cross-linked to the
  **Driver** and **Embeddable agent** entries.

## Addendum (2026-08-04) — a Firing's own Events render as a block, and notices keep the lifecycle

The three-layer consequence above spells the TUI's display half "commands, pickers, status rows and
notices", which was the shape the first Driver surface shipped with: every Event, one line. A
Firing's **fired / completed / failed** Events now render instead as one expandable block carrying
the run's answer, the prompt that produced it, its stats and its record pointer (`layout.md`, "The
firing block"), because the answer — the point of putting a prompt on a cycle — was reachable only
by opening the saved record. This is a scoping of that sentence, not a reversal of a decision:
**created / skipped / stopped stay notices**, the split holds — the answer, the run's stats and the
scheduler-measured elapsed cross as plain data on `schedule.Outcome` / `schedule.Event`, so any
Driver gets them and none of it is TUI-only — and the text flows model → surface without injecting
anything model-visible, so
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)'s
invariant 4 is untouched and the facade-export question stays closed.
