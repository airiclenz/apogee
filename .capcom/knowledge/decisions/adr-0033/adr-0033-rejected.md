---
title: ADR 0033 considered options
---

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
