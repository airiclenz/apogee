---
title: ADR 0033 context
---

`TODO.md`'s `/schedule` bullet asks for a standing instruction: a prompt re-run every time a
chosen cycle passes while apogee is open, each run a fresh context saved as its own session,
`/schedule-stop` to end it. The 2026-08-03 north-star grill enriched that brief with a layering
constraint and left five branch points explicitly open — overlap policy, which Agent modes a
schedule may use, whether schedule identity lands in the session record's browsable `Meta`,
fresh-vs-resumed context per run, and where the thing that actually performs a run lives. A
second grill the same day settled them; this record is that settlement.

ADR 0031
makes one direction non-negotiable. The engine must stay sufficient for *any* **Driver**, and the
first mechanical consequence it names is the deferred **`apogee headless`** runner. A scheduler
welded into the TUI — cycle timing, overlap policy and session grouping living in a Bubble Tea
model — is precisely the silent door-closing that ADR was written against: the daemon it
anticipates would have to rebuild every when-and-how decision from scratch, and nothing would
visibly break in the meantime to say so.

The scheduler is also the first Apogee surface that runs the loop with **no human present**. That
makes ADR 0031's invariant 2 (the Approver seam is wait-tolerant, parking is a Driver's
composition) load-bearing in the other direction: an unattended run that parks on an approval
waits forever, and under any sane overlap policy a schedule whose run never finishes stops firing
altogether. The unattended host's composition must therefore be "never park", stated once, in the
runner.
