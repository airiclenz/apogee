---
title: ADR 0084 consequences
---

- A session with no workspace context file and a modest prompt gets most of the working room as
  History rather than 48% of the window; a session with a large `AGENTS.md` gets a larger
  `FileContext` reservation and a correspondingly smaller History, never below the 50% floor.
- The first prune pass now sits at 70% of a larger History instead of 60% of a fixed 48% share, so
  the thrash the live session showed needs a genuinely full window to reproduce. Six protected
  Turns covers a multi-chunk read in progress.
- Decision 5 makes ADR 0018 §8's survivability ordering — the structural floor below the fold's
  transcript budget — hold at *every* advertised window rather than only above the ~8.9k crossover
  §8 computes. The three allocation parts no longer sum to `ContextLimit − ResponseReserve` when
  the cap bites; `Budget`'s field comments say so.
- ADR 0077 decision 3's "the notice and the fold never disagree" holds unchanged, precisely because
  the cap lives in `budget()` and not in `deriveGrowthBounds`: there is one History, and every
  reader reads it.
- ADR 0026 §8's comparison stops being a tautology. Its rejection of a configurable size limit
  "because the Budget already allocates a system-prompt share" no longer states a live fact — the
  advisory ceiling of decision 4 is what carries that reasoning now.
- Fixtures that pinned a History figure for a named window move: at an 8192-token window History is
  3584 tokens (the cap) where it was 3933. Tests re-derive from `a.budget().History` and from
  `PruneKeepTurns` rather than pinning either.
