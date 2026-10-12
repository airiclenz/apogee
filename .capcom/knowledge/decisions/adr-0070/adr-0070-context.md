---
title: ADR 0070 context
---

Rule **D1** of the [mechanism catalogue](../../../../docs/design/archived/mechanism-catalogue.md) says every catalogued
Mechanism ships **off** until an A/B bench run proves it a win. That rule exists because a
Mechanism is *tuning*: it changes what a model is shown or asked before the model has failed at
anything, and a change like that can help one model and hurt the next. Shipping tuning unproven
would breach the project's hard invariant — that Mechanisms must never make a model perform worse
than the same agent with Mechanisms off.

Two catalogued rows are not tuning. `empty_response_recovery` and `tool_use_enforcer` carry the
`off-ramp` **Capability**: each fires only *after* a Turn has already failed — an assistant reply
with no visible text and no tool calls, or a reply that narrates an action instead of calling the
tool — and each does one thing, gives the model a way out of that failure. They are declared
`SuppressExempt` for the same reason (catalogue C1): suppressing a recovery guarantee leaves the
model with no exit from a dead Turn.

ADR 0006 already acts on that distinction from
the other end. **Bypass mode** — the honest "Mechanisms-off" floor, the bench's control arm, the
switch a user can flip themselves — turns every Mechanism off *except* the off-ramps, precisely so
the floor is a working agent rather than one that quits at the first stumble.

The two rules therefore disagreed about the same two rows. A stock install with no `mechanisms:`
block armed nothing, so it ran **without** the recoveries; the same install with `--bypass` ran
**with** them. The Mechanisms-off floor was strictly more robust than the default posture it is
supposed to be the floor of — the one configuration in which turning help *off* gave the model
more help. Every user hit it, because it was the shipped default.

The bench gate D1 asks for cannot resolve this. D1's question is "does this tuning help or hurt
across a Turn distribution?"; an off-ramp is not on that distribution — it fires only where the
alternative is a Turn that produced nothing. There is no arm in which withholding it is the safer
choice, which is exactly why ADR 0006 already exempts it.
