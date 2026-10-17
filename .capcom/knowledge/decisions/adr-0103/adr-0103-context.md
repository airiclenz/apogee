---
title: ADR 0103 context
---

A person wants to follow a running apogee session from a second terminal: a TUI session, a
`headless` run or a daemon Firing. They want to see it live, read-only and in the TUI: the
streamed text, the tool cards, the Run views of its delegations and its thinking. Before this
record nothing offered that:

- **The record is too coarse.** A Session record is saved per Turn (ADR 0022). It holds no
  streamed text, no tool card in flight and no thinking, so a reader of the record sees the
  session one Turn late and never sees it work.
- **The doors refuse.** A record another apogee holds is refused at `--resume`, `--continue` and
  every `/sessions` door (ADR 0022 D7, 2026-09-19).
- **The Event stream never leaves the process, except one way.** Event lines reach headless
  stdout only, and only under `--format json` (ADR 0075 D13). The daemon writes none, and the
  TUI writes none.
- **The lines lose what a tool card needs.** A Tool summary has no wire form (ADR 0052
  amend-0824). A view rebuilt from the lines would lose the read spans, match counts and Edit
  regions a live card paints. `SubAgentGroupEvent` writes no line at all (ADR 0025 D2, 2026-10-05),
  so a watcher could not show the queued members of a delegation wave.
- **Prompts are not Events.** The human's prompt and each Interjection are committed by the
  engine but never emitted (ADR 0025). A stream of Events alone shows answers with no question.

The owner ratified the design calls on 2026-10-17. The plan is
`docs/plans/2026-10-17 - 03 - live-session-watch-plan.md`.
