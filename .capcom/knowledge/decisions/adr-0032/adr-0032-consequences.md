---
title: ADR 0032 consequences
---

- **A deliberate, documented deviation from oracle parity.** A `SKILL.md` written for either tool
  still loads in both; only collision *resolution* differs. The `sourceDirs` comment stops
  selling the order as parity and states this rule instead, citing this ADR.
- **A repo can still contribute skills, but never substitute one.** New ids from
  `.apogee/skills` load exactly as before; a colliding id now loses and is reported.
- **Shadowing becomes visible.** The `/skills` report gains a section distinct from load
  failures, naming the shadowed file and the winner's path. The same-source case, silent today,
  is covered by the same mechanism.
- **ADR 0002's
  open-extension-point posture is unchanged**, as is
  ADR 0001's injected-roots
  rule — the source dirs are still supplied by the caller, only their precedence is fixed here.
- **This supersedes no ADR.** The prior order was never ratified in a decision record; it lived
  in a code comment, which is the reason the trade-off went unweighed.
