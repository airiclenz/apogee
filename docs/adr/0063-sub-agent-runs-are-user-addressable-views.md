---
Status: accepted
---

# Sub-agent runs are user-addressable views

## Context

A delegation is the one place in apogee where the human is a spectator. Two entries in `IDEAS.md`
say it from opposite ends. `IDEAS.md:14`: expanding a sub-agent should open it *full screen*,
jumping to its latest line, with a way back up — and opening it that way is what would finally
"allow the user to send prompts to the sub agent. This is not currently possible". `IDEAS.md:18`:
with several children live, the activity line above the prompt box "flickers back and forth between
the different sub agents", because there is exactly one activity slot and every child writes it.

Three structural facts stand behind those complaints.

**1 — The inline expanded shape nests a run inside its parent.** Expanding a framed delegation opens
`expandedSubAgentView` (`internal/tui/subagentblock.go:548`): a rail drawn *inside* the parent
transcript, the child's rows one depth deeper than the parent's, sharing the scroll position and the
sticky header with everything around it. It also paints the delegation's tool-result body above the
child's prompt rows, which is the open defect at `ISSUES.md:40` — the report printed twice, once
badly formatted at the top and once properly at the end.

**2 — A running child cannot be addressed at all.**
[ADR 0025](0025-interjections-commit-at-the-between-steps-boundary.md) gave the human a way into a
running Exchange — `Agent.Interject`, committed at a between-Steps boundary — but it is a *top-level*
contract, and it is realised by the Driver drives-`Step`-itself pattern: `Run`'s own doc says it
"owns the between-Steps boundaries it loops over, so there is no seam to interject at"
(`internal/agent/agent.go:531-533`), and directs an embedder who wants mid-Exchange delivery to drive
`Step`. A delegated child cannot take that advice. Per
[ADR 0013](0013-the-sub-agent-orchestrator-is-the-recursion-point-with-isolated-live-guard-state.md)
D5 a sub-agent runs atomically inside the parent's Turn, driven by `Run` from inside the parent's
tool dispatch — so `Run` *is* the child's driver and no host holds its Step loop. "Drive `Step`
yourself" is not an option anyone has.

**3 — The identity already exists.**
[ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md) D5
put the spawning call-ID on `EventBase` so a Driver can tell one child's stream from another's. That
ID is exactly the handle a user-facing address needs; nothing new has to be invented to name a child.

Two invariants bound the answer.
[ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
requires the engine to stay sufficient for **any** Driver, so reaching a child must be an engine
capability, never a TUI-only shortcut around it.
[ADR 0035](0035-the-settings-surface-persists-one-key-per-deliberate-edit.md) settled that a
full-height surface in apogee is a pane inside apogee's own frame, and rejected the alternate-screen
takeover that "drops apogee's status line" and puts the user's scrollback out of reach.

The shape below was ratified with the owner on 2026-08-30 and is realised item-by-item by
`docs/plans/2026-08-30 - 01 - sub-agent-run-view-plan.md`.

## Decision

**D1 — A child is addressed by its spawn call-ID, through an engine-side mailbox drained between the
child's Steps.** The engine gains
`Agent.InterjectChild(spawnCallID string, in domain.UserInput) error`. An `Agent` keeps a registry of
the children currently running under it, keyed by the spawning call-ID (registered around
`sub.Run(ctx)` in `runSubAgent`), and every `Agent` keeps a mailbox of queued `domain.UserInput`.
`InterjectChild` appends to the named child's mailbox, recursing into registered children so a
grandchild is reachable from the top-level agent, and refuses when no such child is running. It is
non-blocking and safe from any goroutine — the TUI calls it from its program goroutine, exactly as it
already calls `AbortExchange`. Delivery is the child's own `Run` loop: before every Step after the
first, the goroutine driving that child pops its mailbox in order and delivers each message through
`Agent.Interject`, which is legal there because it is the same goroutine at a between-Steps boundary
— ADR 0025's boundary, unmoved. **This supersedes, for child agents (depth > 0) only, the two
rejected options at `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md:204-210`** —
"A new engine `Event` for interjections" and "A drain hook on `Agent.Run` for embedders". The
top-level contract of ADR 0025 stands untouched: a top-level `Run` drains nothing and emits no
interjection event, and an embedder who wants mid-Exchange delivery at top level still drives `Step`
itself. The child is different for one reason only — nobody else can drive its Steps. ADR 0013 D5 is
unchanged in every other respect: the parent's Turn is still atomic, a cancel still rolls the whole
Turn back, and nothing about a child newly persists.

**D2 — Delivery is observable, so a Driver can paint it honestly.** `domain.ErrNoSuchChild` names a
child that is not running (the user aimed at a run that has just finished), and the engine emits
`domain.ChildInterjectionEvent{Input, Landed}` for every queued message through the same shared sink
the child's other events ride, stamped with the child's depth and its spawn call-ID:
`Landed: true` when the message was committed into the child's conversation, `Landed: false` for
anything the child finished before reaching. No Driver has to guess whether a message arrived, and no
Driver needs a private channel to find out. Like the drain itself, the event exists for depth > 0
only.

**D3 — A steered child's result tells the parent it was steered.** A child that received user
messages while it ran returns its result with the trailer
`(the user sent N message(s) to this sub-agent while it ran)` as its final line, on every outcome
except a cancelled dispatch. A parent that reads a result shaped by instructions it never issued must
be able to see that from the result alone; the alternative is a model quietly reasoning about a
divergence it has no evidence for.

**D4 — The run view is a transcript-slot takeover, never a screen.** Opening a run replaces the
transcript slot's content while the status line, prompt box and footer stay exactly where they are —
the same frame arithmetic every pane already uses (`frameRowPlan` reserve = 0), so ADR 0035 stands
and no alternate screen is entered. A clickable breadcrumb header row (`← main › <name>`, chained for
nested runs) and `esc` each go **one** level up; the status line's right slot reads `esc back` while a
view is open, and stopping stays whole-run from the top level. The view is **Driver state**: a stack
of open runs in the `Model`, never encoded in the transcript, never written to a session record, and
never restored — a resumed session opens at the top level.

**D5 — Expanding a framed delegation opens its run view.** Expand, from the block cursor or from a
click on the run's header, no longer flips a fold flag: it opens the view, at the run's latest line,
following it as it grows. The inline expanded shape is removed, so a run has exactly two shapes —
collapsed row and run view — while the `✦ Sub-Agent (N)` umbrella keeps expanding inline to its
member rows. This **amends `layout.md:797`**: "nothing ever expands or collapses by itself" continues
to hold for fold states, and a run view opening at its latest line is not a fold state changing under
the user, it is a different surface. For the same reason `docs/layout/tool-layout.md:98`'s "exactly
two states per call" keeps holding — the run view is a third *surface*, not a third fold stage of the
row.

**D6 — A message to a child buys no privilege.** What the user sends into a run view is a plain
interjection: text, file references and skills, parsed exactly as a top-level message is. The child's
tool set, Agent mode, confinement and approval path are the ones it was spawned with, and
[ADR 0005](0005-sub-agent-privileges-are-bounded-by-the-parent.md) is untouched — steering a child
cannot widen it beyond its parent.

## Consequences

- **ADR 0025 now has a named child-side exception.** Its rejected Run-drain and interjection-Event
  options (`docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md:204-210`) no longer
  hold for depth > 0. `Run`'s doc comment, which today tells embedders there is "no seam to interject
  at", is rewritten to say that a child's `Run` drains its mailbox between its Steps. Anyone reading
  either document from the top-level side finds the contract unchanged.
- **Reaching a child is an engine capability, so every Driver has it.** `InterjectChild`,
  `ErrNoSuchChild` and `ChildInterjectionEvent` are `internal/agent` / `internal/domain` surface; the
  TUI only addresses them, and a bench or a future daemon can address them the same way. The engine
  stays wire-silent and gains no Driver-only shortcut, so ADR 0031's doors stay open.
- **The parent's context grows by one line per steered child** (D3), and only when a child was
  actually steered — an unsteered child's result is byte-identical to today's.
- **One TUI shape disappears and a defect closes with it.** `expandedSubAgentView` and the rail it
  drew are deleted, which removes the duplicated, badly formatted report body recorded at
  `ISSUES.md:40`. Every doc sentence and sketch that draws a delegation "expanded in place" is
  restated against the two-shape rule.
- **The activity line gets one slot per run.** With ≥2 children live the top level reads
  `N sub-agents · working` instead of alternating between them, and a run view shows that child's own
  slot — `IDEAS.md:18` closes. Stopping stays sticky and run-wide.
- **What stays out.** Child conversation persistence and prompting a *finished* child remain the
  deliberate non-goal recorded in `ISSUES.md` (a run view of a finished or scheduled child opens
  read-only). Per-child stop, auto-naming children (`IDEAS.md:16`) and any CLI surface for addressing
  a child from headless or the daemon are deferred — the engine seam is sufficient for them, so no
  Driver is blocked by the deferral.
- **Nothing here is a Mechanism.** A message to a child is the human's own input, the trailer reports
  a fact about that human's actions, and the run view only changes what the human sees. There is
  nothing for Bypass to switch off and nothing for a bench arm to measure; the Bypass floor is
  untouched.
