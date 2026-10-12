---
title: ADR 0083 considered and rejected
---

- **Leaving the denials in the plans' verdict tables.** The archived plan is where they were, and
  the 2026-09-16 and 2026-09-20 reviews found every one of them again; a plan is a design artefact
  for the work it contains, and its *Out of scope* line is read by the run that executes it, not
  by the next review.
- **One ADR per denial.** Six records that each say "a review proposed X; the owner said no, and
  here is why" is the same content behind six file names; the reason a reader opens any of them
  is "has this been ruled on?", which one list answers.
- **A `denied:` marker in `CONTEXT.md`.** The concept map defines the domain language; a ruling on
  a candidate is an architectural decision, and settled decisions live under `docs/adr/`
  (`AGENTS.md`, *Where knowledge lives*). `CONTEXT.md` links here only from the entries that
  already cite the topic a denial protects.
- **Recording each denial as a bead.** A bead is open work or parked work; a standing denial is
  neither — it is the decision that there is no work.
- **Writing the six as amendments to the ADRs they protect** (0022, 0037, 0052, 0060). Three of
  the six protect no single ADR (the delegate value, the bound entry, host boot), and a reviewer
  looking for "was this ruled on?" would have to know which ADR to open before finding out.
