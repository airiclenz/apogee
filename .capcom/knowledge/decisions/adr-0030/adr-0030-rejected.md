---
title: ADR 0030 considered options
---

- **Normalize the content instead** — keep grapheme width as the single measure and strip or
  fold VS16 out of everything the TUI renders. Rejected: it is a narrowing, not a fix — ZWJ
  sequences and regional indicators can still diverge — and it changes what the user sees
  (`⚠️` would paint as `⚠`). It also treats the terminal's measure as apogee's to overrule.
- **Always wcwidth** — simplest, and correct on every legacy terminal. Rejected: wrong by one
  cell per VS16 grapheme on any terminal that *does* answer mode 2027, which is the growing
  majority, and it would have made the drift worse over time rather than better.
- **Always grapheme width and force the painter** — rejected as not available: apogee cannot
  make a terminal support mode 2027, and bubbletea does not even ask on Apple Terminal or over
  SSH with a known `TERM_PROGRAM`. A program that assumes the capability is wrong exactly where
  it cannot detect that it is.
- **A package-level function or a package var holding the method** — rejected under ADR 0011
  and under the same reasoning that keeps the theme a value: a global would make the measure
  invisible at the call site and untestable in two methods within one test binary. Hanging it on
  `theme` puts the measure where the styles already are.
- **Fix `wrap.go` and `truncate.go` upstream and wait for a release** — rejected as a
  dependency on someone else's schedule for a contract `layout.md` states as absolute. Both
  workarounds are commented with the upstream defect and revert to delegation after a bump.
- **Convert the remaining popup sites now** — rejected on collision grounds only: plan
  `2026-07-31 - 01` owns those files while it is live. See Consequences.
