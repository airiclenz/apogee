---
title: ADR 0065 consequences
---

- **`internal/skills` stops being builtin-free, and its doc comment's attribution moves here.**
  `doc.go`'s "no builtins shipped — ADR 0002" claim is superseded by §1: ADR 0002 makes tools an
  open extension point, which is an argument for allowing user skills, never one against shipping
  any. The open point is untouched — a shipped skill occupies no privileged position and can be
  shadowed by a folder any user writes.
- **The suggestion band works out of the box.** ADR 0061's matcher indexes the shipped skills like
  any others, so a fresh install gets suggestions on its first draft — the feature's first real
  exercise, and a standing check on the shipped skills' `triggers:` being written honestly (four at
  ratification; five since `audit` joined, ADR 0087 D6, 2026-09-27).
- **A shipped body is apogee's text in front of the model, on every upgrade.** Same exposure as ADR
  0064's default prompt and the orientation block, and the same obligation: a bad paragraph reaches
  everyone. The Bypass floor is not engaged — a skill body enters a turn only when invoked — but the
  quality bar is ADR 0064 §6's, not a lower one.
- **The model can now spend tokens on its own initiative.** A `load_skill` call and its body are
  charged to the Budget like any tool result and capped like one; a model that calls it every Turn
  is a roster problem, solved with `tools.disabled`, not a new gate.
- **CONTEXT.md's *Skill* entry gains the shipped source, the `/skills` verb's export form and
  `load_skill`**, keeps its "*Avoid*: 'tool'" with §7's distinction attached, and its
  layered-discovery sentence now names four sources rather than three.
