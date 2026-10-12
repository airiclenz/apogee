---
title: ADR 0079 context
---

apogee's hard invariant is that nothing it puts in front of a model may make that model perform
worse than the bare loop (ADR 0076).
The bench measures the *quality* side of that invariant; the *cost* side — how many tokens apogee
itself spends before the user has said a word — has never been a number anyone could read.

> **Amended 2026-10-12 (plan `2026-10-12 - 00` item 4).** The citation above is wrong: ADR 0076
> never says "the bare loop". The invariant's baseline is defined by
> ADR 0006 — the bare loop is **Bypass mode**,
> with the Floor guards and the structural reducers on, not a naked model (`CONTEXT.md`, opening
> paragraph).

What apogee injects at Turn 1 is spread across several producers: the rendered
System prompt template, the Orientation block, the delegate
report block on delegations, the Task list block once the model has written one, the workspace
Context files, the Reactions' directives, and the tool menu — as native `tools` on a
tool-capable profile, or as the rendered tool-instruction block on one that is not
(`internal/agent/standingblocks.go`, `loop.go`'s `buildRequest` / `standingSystem` / `toolMenu`,
`wire.go`'s `toProviderRequest` / `toolInstructions`). The only figure the engine reports today is
`ContextFilesReport.StandingTokens` (`internal/domain/contextfile.go`), a chars/token estimate of
the whole standing string, surfaced to the host as an oversize warning and to headless as
`run_finished.context_files.standing_tokens`. It is one lump: it cannot say which piece grew, it
excludes the tool menu, and nothing ties it to the `prompt_tokens` the provider actually charges.

Three things follow from that gap. The bench cannot chart context cost per arm, so a Reaction
that earns its quality delta by spending a thousand standing tokens is indistinguishable from one
that spends ten. The tool-surface work (`docs/design/tool-surface-findings.md`) cannot price a
tool description. And growth in the injected bytes is silent: a sentence added to the Orientation
block or a tool description lands on every request of every session without any test noticing.

`apogee probe` already splits into a free host report and an explicit, paid model battery
(ADR 0021);
the headless Event stream is a versioned protocol whose frames may grow additively under one `v`
(ADR 0075); and the goldens
rule (ADR 0062 call 13, restated at
`docs/design/test-drivers.md` §Goldens and `internal/tuitest/golden.go`) confines goldens to
rendering surfaces, with ADR 0075 §14 as its one prior supersession for a documented wire format.

The owner ratified the calls below on 2026-09-17 (plan `docs/plans/2026-09-17 - 00`).
