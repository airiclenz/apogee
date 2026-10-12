---
title: ADR 0006 context
---

The hard constraint is that Apogee's Mechanisms must never make a model perform worse than
the same agent *without* them. To prove that, the bench needs a control arm — a
"without Mechanisms" baseline — and the constraint's wording needs an unambiguous referent
for "without Apogee."

The naive referent, *the naked model*, is wrong: a naked model with no Budget and no
Compaction simply overflows its context window on any non-trivial task and scores
catastrophically. Comparing against it would let almost anything look like an improvement —
the gate would pass trivially. The structural reducers (Budget, Compaction) are load-bearing
parts of the agent, not Mechanisms, and must stay on in the baseline.

apogee-sim already had a proxy-header bypass (`X-Apogee-Bypass`) that skipped the transform
pipeline; the merged Apogee has no proxy, so this must be re-expressed as a first-class
configuration of the loop.
