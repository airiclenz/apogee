---
title: ADR 0063 decision
---

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
Turn back, and nothing about a child newly persists. (Amended 2026-10-02: a cancel no longer rolls
the Turn back — ADR 0088 supersedes
ADR 0013 §5(b): a cancel settles, and the Turns that finished before the stop stay.)

> **Amended 2026-09-26 (ADR 0086 D5).** A child is addressed by its engine-minted
> **run id** (ADR 0039's 2026-09-24 amendment), not its spawn call-ID, which may repeat across one
> fan-out: the signature is `Agent.InterjectChild(runID string, in domain.UserInput) error` and the
> registry is keyed by run id. The same key reaches `Agent.StopChild(runID)`, the per-child stop.

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
view is open (amended 2026-09-24, below: once the viewed run has reported usage the slot states that
run's context gauge instead), and stopping stays whole-run from the top level. The view is **Driver state**: a stack
of open runs in the `Model`, never encoded in the transcript, never written to a session record, and
never restored — a resumed session opens at the top level.

> **Amended 2026-09-26 (ADR 0086 D4, D5).** "Stopping stays whole-run from the top
> level" no longer holds. `ctrl+x` stops the viewed run alone — and every delegation under it —
> while the parent's Turn goes on, and the slot reads `esc back · ^x stop` while the viewed run is
> running; `esc` still goes back, and `esc×2` at the top level still cancels the whole Turn.

> **Amended 2026-09-28 (ADR 0090 D3).** A level of
> the view stack is a run **or a workflow stage**. A stage view lists one row per item of a Workflow
> stage and is not a run: its box is read-only, it states no gauge, and `^x` stops only an item's run
> from that item's row. The breadcrumb names the Workflow and the stage
> (`← main › <workflow> › <stage>[ › <item>]`), and `esc` still goes up one level. A Workflow's item
> run is a run like any other and opens as its own run view.

**D5 — Expanding a framed delegation opens its run view.** Expand, from the block cursor or from a
click on the run's header, no longer flips a fold flag: it opens the view, at the run's latest line,
following it as it grows. The inline expanded shape is removed, so a run has exactly two shapes —
collapsed row and run view — while the `✦ Sub-Agent (N)` umbrella keeps expanding inline to its
member rows. This **amends `layout.md:790`**: "nothing ever expands or collapses by itself" continues
to hold for fold states, and a run view opening at its latest line is not a fold state changing under
the user, it is a different surface. For the same reason `docs/layout/tool-layout.md:100`'s "exactly
two states per call" keeps holding — the run view is a third *surface*, not a third fold stage of the
row.

**D6 — A message to a child buys no privilege.** What the user sends into a run view is a plain
interjection: text, file references and skills, parsed exactly as a top-level message is. The child's
tool set, Agent mode, confinement and approval path are the ones it was spawned with, and
ADR 0005 is untouched — steering a child
cannot widen it beyond its parent.
