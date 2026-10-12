---
title: ADR 0076 consequences
---

- ADR 0071 keeps decisions 1, 2, 3 and 5 and its amendment; decision 4 and rejected alternative B
  are superseded and gain a pointer note. ADR 0073 keeps decisions 1 (term, as amended by decision 12
  above), 3, 6, 8 and 9 and its plugin and Driver-side-feed rejections; decisions 2, 4, 5 and 7 and
  the veto and lint rejections are superseded and gain a pointer note.
- *Noted 2026-09-24:* decision 1's deletion of the registry, the catalogue and
  `Config.EnableMechanisms` retires ADR 0003 and ADR 0015 (`superseded by ADR 0076`) and ADR 0002's
  Mechanism-catalogue half (its open-Tools half stands); ADR 0014, amended by removal in ADR 0071
  decision 5, reads `superseded by ADR 0071`. Each carries a pointer note under its header.
- ADR 0031 stands unchanged. ADR 0033 decision 5 and ADR 0034 §8's payload-discarded / payload-
  injected split, and ADR 0061 §4's B1 deferral, are not reopened here: a daemon Firing's or a
  skill's *own* payload still does not enter the model; a user advise reaction is a different
  route, opted into per entry.
- The advise cell carries the costs the findings document lists — an injection channel, token cost,
  staleness, prefix cache, forgery adjacency; decisions 6, 7 and 8 are the tools that address them,
  not a proof they are gone. Advise scripts should return facts, not imperatives; the manual says so.
- `docs/manual/hooks.md` and `docs/manual/configuration.md` are rewritten around `reactions:`; the
  glossary's `Reaction surface` entry becomes the matrix; the `Mechanism`, `Hook point` and
  `Experimental hook` entries become pointers.
- Most of the ~6k test lines across the three packages rewrite; one harness (Moment payload in,
  outcome out) replaces three.
- Bead `apogee-575` closes on this record. Implementation follows a plan in the house format; the
  bench identity arm for stage 1 and the fixed-text arm for stage 3 are that plan's acceptance,
  not this ADR's.
