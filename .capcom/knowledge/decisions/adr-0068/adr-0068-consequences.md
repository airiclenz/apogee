---
title: ADR 0068 consequences
---

- `internal/domain` grows the naming seam (`DelegationNaming`, `DelegationNamer`, `Config.Namer`)
  and one event, `SubAgentNamedEvent`, which every event-completeness switch and the TUI's fold
  table must cover like any other.
- The engine's spawn path fires the namer for an unnamed child, applies the reply under the same
  lock the run's name is read through, and emits the event; a reply for a run that has ended is
  discarded there.
- `internal/title` stops being "the naming of a session" and becomes the naming of a session **or**
  a delegation: a second prompt builder beside `Prompt`, a shorter sanitiser cap, and its
  package-doc claim about the naming call scoped to the session title with this record beside it.
- The host gains one `DelegationNamer` implementation — a single completion built the
  `probe.Chat` way on the child's Upstream — wired in the composition root and gated by the live
  `auto-title` value, so flipping the key mid-session takes effect on the next delegation.
- Every delegation reader folds the rename rather than re-deriving a name: the TUI head and its
  saved scrollback, the run driver's `SubAgentUsage`, and `apogee headless`'s per-run line.
- ADR 0022's addendum is amended, not superseded: its decisions about the session title stand
  exactly as ratified, and its "not a Turn" reasoning is what this record inherits — with the one
  named exception that a delegation's name is announced as an event.
- **Out of scope, unchanged:** a human renaming a delegation by hand (a run is not a session; there
  is no `^r`), naming at any depth other than the child's own spawn, and any second naming attempt
  for a run whose first one failed.
