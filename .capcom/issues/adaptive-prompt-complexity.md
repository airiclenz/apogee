---
title: Adaptive prompt complexity — request slimming driven by the capability tier
status: parked
type: feature
priority: 3
labels: [bench]
aliases: [apogee-fsx]
created: 2026-09-03T18:40:21Z
---

**Status:** parked 2026-07-22 by decision, not by omission
([ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)
Q3; `../apogee-sim/mission.md` item 2). Phase 5 ships the **capability tier** as a reported
`apogee probe model` field and stops there.

The idea: a pre-request transform that shapes the outgoing request to what the model can
actually digest — stripping tool descriptions down to names and one line, shortening the system
prompt, simplifying output formatting — selected by the tier the probe observed. It is the
mission's "prompt complexity tier" and aims squarely at the smallest models, the ones this
project exists for.

Why it is not built with the probe: this is model-facing behaviour inside the loop, so it is a
**lab row** on the Mechanism registry — earning its place per model on the non-inferiority gate
against Bypass, with a Table B bench-validation entry
([ADR 0009](docs/adr/0009-the-ab-decision-rule.md); the Phase-5 settled design: nothing
model-facing ships default-on without bench evidence) — or, if the evidence turns out to hold for
every model, a **Floor guard**, which takes a verdict of its own under
[ADR 0071](docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md).
Shipping it alongside the probe would mean an unvalidated transform in front of every model
either way. The tier signal costs nothing and is already there when the evidence is.

When picked up: build it as a **pre-request** transform, **off** until a stored probe record's tier
arms it (so it no-ops entirely for an un-probed model), and bench it on at least one small model
before it reaches anyone by default. Open design questions kept warm: whether slimming applies
per-request or per-session (a mid-session change of tool descriptions is a history-consistency
question), and whether the tier or the individual battery findings (native tool calls vs. JSON
vs. multi-step) are the better gate — the findings are strictly more informative, the tier is
strictly easier to reason about.
