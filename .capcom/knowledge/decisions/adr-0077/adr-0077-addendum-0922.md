---
title: "ADR 0077 addendum of 2026-09-22: the \"60% / ~48%\" figures were the fixed-share split, now retired"
---

The Context's "gives History 60% of what remains, so the automatic Compaction line sits at roughly
**48% of the working ceiling**" — and the Rejected "the model would be folded at ~48%" that rests
on it — describe the FIXED-SHARE split retired by
ADR 0084: History is now the
working room less what the standing parts measure, never below 50% of the working room and never
above the fold's transcript budget, so the line the ladder is scaled to moves with the session's
own standing content rather than sitting at one fraction of the window. The decision is unchanged
— scaling the rungs to the History allocation rather than to the window is exactly what makes that
survivable — and decision 3's "the notice and the fold never disagree" holds as written, because
the cap lives where History is produced (`(*Agent).budget()`) and every reader, the notice and the
fold included, reads that one number.
