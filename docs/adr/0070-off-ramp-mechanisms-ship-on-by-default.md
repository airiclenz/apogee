---
Status: accepted
Amends: ADR 0006 (Bypass floor), ADR 0015 ("EnableMechanisms is the one enable path" — the empty-list engine floor is the one exception, stated below), ADR 0016 (manual-control rule unchanged), ADR 0045 §2 (a present `mechanisms:` map still replaces whole, above the floor)
---

# Off-ramp Mechanisms ship on by default

## Context

Rule **D1** of the [mechanism catalogue](../design/mechanism-catalogue.md) says every catalogued
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

[ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md) already acts on that distinction from
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

## Decision

**The `off-ramp` Capability is the one exception to D1: catalogued off-ramps ship enabled, and are
armed unless a `mechanisms:` block names one explicitly `false`. Every other Capability keeps D1
unchanged.** The rule has two halves, in two places, because there are two entry points into an
enable set:

**1 — The Driver-side union, for a `mechanisms:` block.** `mechanisms.OffRampFloor(block)` returns
the catalogued rows whose `Capability` is `off-ramp` and whose key is not explicitly `false` in
that block. `mechanisms.ResolveEnabled` unions it into what the block names, deduplicated and
re-sorted, and `withOffRampFloor` folds it into a Validated set the same way. So an **absent**
block resolves to the two off-ramps, an **empty** one does too, a block enabling other rows gets
them *beside* the floor, and `tool_use_enforcer: false` resolves to `empty_response_recovery`
alone. An absent key means ON — which is why the block is read for an explicit `false` rather than
for a `true`.

**2 — The engine floor, for an empty `EnableMechanisms`.** `agent.buildEnabledMechanisms` builds
the same floor when the list is empty *and the engine made the registry itself*
(`Config.Mechanisms == nil`). This is what keeps a library embedder or a bench arm that hands
`New` a bare `Config` on the same recovery guarantees the TUI runs, rather than on a quieter agent
nobody asked for — the [ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)
benchable-all-the-way-up door. A **handed-in** registry is never floored: that caller assembled
the arm itself (both sub-agent spawn paths and `Rebind` pass one), and flooring it would re-add
rows the registry may already hold, failing construction as "already registered".

The floor is harvested from the production catalogue's `Capability` column, not from a hand-kept ID
list, so a row that later joins or leaves `off-ramp` moves the floor with it and no second list can
drift from the first.

**This does not change the shape of any bench arm.** ADR 0006's control arm keeps the off-ramps
already, and the floor is Capability-derived and identical in every arm, so the same-code-path
guarantee holds: what a user runs by default and what the control arm runs still differ only in the
tuning rows under test.

**What is amended.** [ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md): the Bypass floor
is unchanged, but it is no longer *more* armed than the default — the two now agree about
off-ramps. [ADR 0015](0015-catalogued-mechanisms-are-enabled-by-id-through-config.md): its
"`EnableMechanisms` is the one enable path" holds for every catalogued row, with exactly one stated
exception — the empty-list engine floor above; every other enable still arrives through that field.
[ADR 0016](0016-curation-is-per-model-validated-sets-keyed-by-fingerprint.md): the manual-control
rule is untouched — a non-empty `mechanisms:` block still suppresses auto-application of a Validated
set, and naming an off-ramp `false` is such a block. [ADR 0045](0045-sub-agents-route-to-the-flagged-server-with-its-own-posture.md)
§2: a present `mechanisms:` map is still the child's entire catalogue, replacing whatever the parent
arms — above the floor, which the child's own resolution then unions in as it would for any block.

## Considered options

**A — Leave D1 as it is and let the user enable the off-ramps.** Rejected: it keeps the defect for
everyone who never reads the catalogue, and it asks a user to opt into a recovery from a failure
they have not had yet. It also leaves the absurdity intact — `--bypass` remaining the more robust
posture.

**B — Bench the two off-ramps and flip them on the evidence.** Rejected: D1's gate measures a
Mechanism across a Turn distribution, and an off-ramp is off that distribution — it fires only on a
Turn that already failed. The measurement would be dominated by noise from Turns where the row
never ran, and the arm that withholds it is not a posture anyone would want to ship even if it
scored level. ADR 0006 made the same judgment when it exempted them from Bypass.

**C — Stop calling them Mechanisms; make them structure.** Rejected: they *are* gated,
descriptor-declared, hook-attached behaviour with a catalogue row, an ID and a config key, and
users can still turn one off. Moving them out of the catalogue would cost the `/settings` row, the
config key and the descriptor discipline to buy a wording change.

**D — Default them on, with no way to turn them off.** Rejected: an off-ramp injects text, and an
operator debugging their own upstream may want a genuinely silent floor. The explicit `false` costs
one line and keeps the guarantee an opinion rather than a lock.

## Consequences

- A stock install now arms two Mechanisms. The `/settings` mechanisms list shows them `on` with no
  block present, so the pane reads honestly; switching one off writes `<id>: false`.
- **A non-empty `mechanisms:` block still means manual control** (ADR 0016), and a block that names
  only `tool_use_enforcer: false` is one — it suppresses auto-application of a Validated set. That
  is a real cost of turning an off-ramp off and is the user's own choice.
- A Validated set that never named an off-ramp now runs with them anyway, since `withOffRampFloor`
  folds the floor into the applied set. That is the intended reading of ADR 0016's "a model with no
  Validated set runs the D1 floor (structure + off-ramps)", now true of a model *with* one too.
- Documents stating the old blanket default were amended in the same commit: `README.md`,
  `CONTEXT.md` (Bypass, Mechanism, Off-ramp), `docs/manual/configuration.md`,
  `docs/manual/commands.md`, the catalogue's D1 line and Table B rows, the shipped `config.yaml`
  template, and the code comments at the enable seams.
- The catalogue's D1 stays the rule for the other nineteen rows. A future Capability wanting the
  same exemption needs its own record; "recovery guarantee, fires only after a failure, survives
  Bypass" is the test this one passed.
