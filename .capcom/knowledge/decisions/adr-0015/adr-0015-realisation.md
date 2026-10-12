---
title: "ADR 0015 realisation of 2026-07-05: authorized implementation deviations"
---

`Config.EnableMechanisms`, the public descriptors, and the matchable enable errors shipped
item-by-item on the day this ADR was ratified, purely additive under v1 (§5). The build held to
the Decision's letter; the design-level refinements recorded during implementation — each
already in the plan's per-item NOTES, mirrored here so the ADR stays the ground truth — are:

- **A spawned sub-agent inherits the parent's already-built registry, not its
  `EnableMechanisms` list.** §1 places the build in the shared construction path, which is also
  the sub-agent construction path (`internal/agent` `newChildAgent`, which copies the parent
  `Config`). A child re-running the build over its inherited `Config.Mechanisms` would re-`Add`
  the same catalogued Mechanisms and trip the registry's duplicate-ID rejection on every spawn.
  The child therefore inherits the parent's already-built registry and clears `EnableMechanisms`,
  so it fires the identical Mechanisms without rebuilding — a construction detail §1 did not
  spell out, consistent with "built … at construction". *(Ownership amended 2026-08-08.)* The
  inheritance goes through **`MechanismRegistry.ForSubAgent()`**, not by pointer: ADR 0039's
  depth-0 fan-out runs **sibling children at once**, and two siblings driving one hook instance
  would both race on its state and see each other's run. The contract is now the one
  `Guards.ForSubAgent` states one line down (ADR 0013 §3) — isolate what is live, share what is
  read-only: the registry **container** is always the child's own (so no two agents can race
  through the registry itself, and it is read-only in the engine besides — only `Add` /
  `AddExperimental`, both construction-time, mutate one), a hook declaring
  **`domain.SubAgentScoped`** hands each child the instance it chooses, and a hook declaring
  nothing is inherited **verbatim**. Which is every Mechanism in today's catalogue but `library`:
  they are value hooks whose fields are read-only after construction, so the child fires the
  identical Mechanisms exactly as this bullet has always said. `library` — the one row held by
  pointer — declares a fresh instance over the **same** mutex-guarded store, because the store is
  the session's one cross-session memory. A catalogue test refuses any future pointer-held row
  that declares neither.
- **A degraded Library store never blocks construction.** §2's "the library store is loaded from
  `Config.LibraryDir` … the way `cmd/apogee/wire.go` derives them today" is made literal: a
  corrupt or absent store degrades to an empty store with an `os.Stderr` notice, so
  an unreadable Library disables learning rather than failing `New`/`Resume` — the posture the
  cmd path it replaces already had. *(Pointer corrected 2026-08-01. This bullet used to read "with
  wire.go's exact `os.Stderr` notice", which was true when written — the engine copied wire.go's
  string verbatim. Neither half holds today: the notice lives in the engine, in `deriveDeps`
  (`internal/agent/construct.go`), and it now reads `"%v — library store degraded to empty"`,
  appending the consequence to a `Load` error that already carries the house `apogee: ` prefix,
  where wire.go's original `"apogee: library store degraded to empty: %v"` wrapped that error and
  printed the prefix twice. The decision above is unchanged — only where the notice lives and how
  it is worded.)*

**Note (2026-07-25) — how this ADR reads after ADR 0003's row-shape amendment.** The Mechanism
registry now holds `RegisteredMechanism{Descriptor, Ordering, Hook}` rows and a Mechanism value
carries behaviour only
(ADR 0003, Amendment
2026-07-25). Nothing in the Decision above changes; three sentences are re-read:

- **§3's parenthetical "(instances must keep matching their rows)" is now vacuous** — there is no
  instance descriptor left to match. Descriptors were already "static catalogue data" here; they are
  now *only* that, joined to the behaviour once in `mechanisms.Build`, so the query still never needs
  to build and the drift the parenthetical guarded against is unrepresentable.
- **§5's "stable v1 API" is read against the 0.x version reset.** That clause was written under the
  `1.x` line; the human-facing release version reset to `0.8.0` on 2026-07-23, and under SemVer `0.x`
  makes no stability promise. `apogee.Mechanism` is therefore **removed** (not deprecated) in favour
  of `apogee.RegisteredMechanism`, and `MechanismRegistry.Add`/`.Ordered` change shape, shipping at
  `v0.8.3` as a **documented break with CHANGELOG notice** rather than a major bump. Everything §5
  actually names as the surface — the `EnableMechanisms` field, the `CataloguedMechanisms()` query,
  the enable errors and the "catalogue contents are not a contract" posture — is unchanged.
- **§2 is reaffirmed, not amended.** The engine still derives `Deps` from `Config`; a catalogue row
  now only *declares* which of them it needs (`mechanisms.DepNeeds` / `DepsNeeded`, consumed by
  `internal/agent`'s `deriveDeps`), which is what let the engine's `library`-only special case go. The
  rejected alternative — moving the store construction and the fingerprint ladder into
  `internal/mechanisms` — was rejected *because* it would contradict §2.

(The Context section above names the enable path of its day, "the catalogued constructor table
(`internal/mechanisms.Build`)". That table is now one `row` table and the wording is left as written:
a Context section records the world as it stood at ratification.)
