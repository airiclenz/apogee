---
title: ADR 0063 consequences
---

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
  drew are deleted, which removes the duplicated, badly formatted report body `ISSUES.md` records as
  this is written (its entry goes with the fix). Every doc sentence and sketch that draws a
  delegation "expanded in place" is restated against the two-shape rule.
- **The activity line gets one slot per run.** With ≥2 children live the top level reads
  `N sub-agents · working` instead of alternating between them, and a run view shows that child's own
  slot — `IDEAS.md:18` closes. Stopping stays sticky and run-wide.
- **What stays out.** Child conversation persistence and prompting a *finished* child remain the
  deliberate non-goal recorded in `ISSUES.md` (a run view of a finished or scheduled child opens
  read-only). Per-child stop, auto-naming children (`IDEAS.md:16`) and any CLI surface for addressing
  a child from headless or the daemon are deferred — the engine seam is sufficient for them, so no
  Driver is blocked by the deferral.
  > **Amended 2026-09-26 (ADR 0086).** Per-child stop has shipped (`Agent.StopChild`,
  > `^x` in the TUI). A run view of a finished child stays read-only: the human messages a running
  > child, and the parent continues a finished one with `continue:` (ADR 0086 D1). Child
  > conversation persistence and a CLI or headless surface for addressing a child stay out.
- **Nothing here is a Mechanism.** A message to a child is the human's own input, the trailer reports
  a fact about that human's actions, and the run view only changes what the human sees. There is
  nothing for Bypass to switch off and nothing for a bench arm to measure; the Bypass floor is
  untouched.
