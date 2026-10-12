---
title: "ADR 0018 addendum of 2026-09-22: §8's crossover arithmetic: History is measured, and the cap lives in `budget()`"
---

§8's "History is ~60% of the working room (~48% of the window at the default 20% reserve)" — and
§7's "~60%-of-working-room History allocation" — describe the FIXED-SHARE split retired by
ADR 0084: History is now the
working room less what the standing parts actually measure, never below 50% of the working room,
and `(*Agent).budget()` caps it at the fold's own transcript budget
(`max(Window − (compactMaxTokens + compactPromptOverheadTokens), compactMinTranscriptTokens)`)
whenever a window is advertised — so §8's survivability ordering, the structural floor below the
fold's transcript budget, now holds at every window instead of only above the ~8.9k crossover this
section computes, and the crossover band is arithmetic history rather than a live caveat.
