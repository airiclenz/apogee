---
title: "ADR 0071 decision 4: The machinery stays as the lab surface; the catalogue is frozen"
---

**The hook API, the registry, `Config.EnableMechanisms`, the `mechanisms:` config key, the
`/settings` mechanisms row and `--bypass` all stay**, as ADR 0001's
externally-driven lab surface: a bench arm hands `New` its own registry and its own experimental
hooks, and that is how a future intervention gets measured before anyone argues about shipping it.
Removing the layer would cost the bench its only instrument to buy a smaller `internal/` tree.

**The shipped catalogue is frozen.** No further ports from the sim, no new rows. The remaining port
plan in `catalogue.md`'s Table A is closed as a plan; the table survives as a record. The shipped
catalogue and the shipped Validated roster both end **empty** — that is the intended end state, not
a transitional one, and the invariants that used to assert "twenty rows" now assert emptiness.

**Bypass switches off lab rows only.** With the catalogue empty, `--bypass` on a stock install is a
no-op in effect, and the honest floor it names is now the engine's own — Floor guards on, nothing
armed above them. ADR 0006's definition is otherwise unchanged, and its same-code-path guarantee
gets stronger: the control arm and the default posture are now literally the same agent unless a
bench arm arms something.
