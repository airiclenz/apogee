---
title: "When you see something else worth fixing during a task: leave it and report it, unless it is a regression"
labels: [seed:baseline, seed-text:0.17.1:24e61b4d759f]
---

Keep a change to what the task asks. An unrelated refactor, rename, reformat or dependency
update does not belong in it, even when it looks like an improvement: it hides the real
change from review and collides with other people's work. Report what else you found: to the
user, or as an Issue (`capcom create`) with a body that stands on its own.

A regression is the exception, and this repository's rule wins over the paragraph above
([ADR 0101](../decisions/adr-0101.md)): a regression as that record and the agent guide
define it is fixed in the run that finds it, or it blocks that run's closeout. Never leave
one behind as a report or an Issue. Fix it in its own commit, apart from the task's change,
and name it in your report.
