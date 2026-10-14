---
title: ADR 0101 context
---

On 2026-08-28 two independently correct changes were found to have broken the model's journey
together: the F-13 resolved-path mount (2026-08-26) and the ADR 0056 scratch dir under
`~/.apogee` combined with the ADR 0049 §4 forced look. Each change passed its own item's tests.
Between them, a path apogee itself announced to the model (the `files: …` skill header, the
orientation's scratch line) was refused or prompted on by apogee's own tools. The read-root
residual had already shipped in v0.18.2.

The owner made the rule standing the same day
(`docs/plans/archived/2026-08-28 - 01 - announced-paths-regression-floor-plan.md`, item 1).
AGENTS.md carried it until this record took it over (2026-10-14). The same plan added the e2e
suite asserting that in Auto every path the engine announces is usable by every tool with zero
approval prompts.
