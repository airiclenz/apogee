---
title: ADR 0090 context
---

ADR 0087 lets the engine run a Workflow: the model asks with `fan_out`, or the human launches a
Recipe with `/<id>`. Each item of a Workflow is done by a child run. The first cut
(`docs/plans/archived/2026-09-27 - 00 - engine-run-workflows-plan.md`, item 43) drew a Recipe's
Workflow as one block that "paints one way and never collapses". Every item run's narration and tool
calls painted railed beneath that block, in the order they happened. A `fan_out` card did the same
under its own fold, and "a head with many runs has no run view".

That was readable for two items and not for twelve. The item runs ran side by side, so their
entries interleaved, and one item's work could not be read on its own. An item run could also not be
reached the way ADR 0063 lets a reader reach any delegation: there was no view to open, no box to
message it from, and no row for `^x` (ADR 0086) to stop it on. The items are sub-agent runs like any
other, but they had none of the surfaces a sub-agent run has.
