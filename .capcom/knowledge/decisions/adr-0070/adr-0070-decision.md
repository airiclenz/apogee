---
title: ADR 0070 decision
---

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
nobody asked for — the ADR 0031
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

**What is amended.** ADR 0006: the Bypass floor
is unchanged, but it is no longer *more* armed than the default — the two now agree about
off-ramps. ADR 0015: its
"`EnableMechanisms` is the one enable path" holds for every catalogued row, with exactly one stated
exception — the empty-list engine floor above; every other enable still arrives through that field.
ADR 0016: the manual-control
rule is untouched — a non-empty `mechanisms:` block still suppresses auto-application of a Validated
set, and naming an off-ramp `false` is such a block. ADR 0045
§2: a present `mechanisms:` map is still the child's entire catalogue, replacing whatever the parent
arms — above the floor, which the child's own resolution then unions in as it would for any block.
