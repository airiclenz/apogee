---
title: "ADR 0033 addendum of 2026-08-04: a Firing's own Events render as a block, and notices keep the lifecycle"
---

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
ADR 0031's
invariant 4 is untouched and the facade-export question stays closed.
