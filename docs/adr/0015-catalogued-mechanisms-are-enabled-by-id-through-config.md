---
Status: accepted
---

# Catalogued Mechanisms are enabled by ID through Config, and their descriptors are public

## Context

The bench (apogee-sim, the [ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)
consumer) must arm catalogued Mechanisms **in-process** to run the A/B campaign: the
aggregate mechanisms-on vs Bypass gate and the per-Mechanism leave-one-out arms of
[ADR 0009](0009-the-ab-decision-rule.md), including the leave-the-STACK-out arm for
`guided_decomposition + tool_result_cap` ([ADR 0014](0014-guided-decomposition-steers-the-primary-call-and-serializes-delegation.md) §4).

Today the enable path is cmd-only. `Config.Mechanisms *MechanismRegistry` is public, but
the catalogued constructor table (`internal/mechanisms.Build`) and its injected `Deps`
are internal; only `cmd/apogee/wire.go` walks the YAML `mechanisms:` map into a built
registry. The executable embedding contract, `benchreadiness_test.go`, arms Mechanisms
only by being in-repo — its header apologises for importing `internal/mechanisms` — so a
separate module cannot do today what the contract demonstrates.

The grill (2026-07-05) rejected the two ways around a new surface before settling its
shape:

- **Whole-binary arms via the `cmd/apogee` config** — the binary is TUI-only (headless
  subcommands are deferred), so this path *also* requires new apogee work, and it
  abandons the in-process instrument the bench already has: event capture as Go values,
  snapshot/resume forking, hermetic workspace scoring.
- **Re-implementing Mechanisms as experimental hooks** — experimental hooks fire under
  the synthetic `experimental` ID and are, by the glossary's own definition, behaviours
  that are *not (yet)* Mechanisms. Benching a re-implementation is evidence about a copy,
  not about the shipped Mechanism; a default-ON flip cannot rest on it.

## Decision

**`Config` gains `EnableMechanisms []MechanismID`. `New`/`Resume` build the named
catalogued Mechanisms internally — the one enable path for the CLI, the bench, and any
embedder — and the Mechanism descriptor becomes public so the bench can plan arms from
the single source of truth.** Concretely:

**1 — Enable-by-ID is a Config field, not a builder or a public package.** The engine
builds each listed ID via the internal catalogue (sorted, deterministic — the wire.go
precedent) and registers it into the consumer-provided `Config.Mechanisms` registry, or a
fresh one when nil, so catalogued Mechanisms and bench experimental hooks coexist in one
arm. All existing construction gates apply unchanged and at the same boundary: unknown
IDs fail loudly naming the known set, duplicates are rejected by the registry, and
ordering / incompatibility / requirement validation fires in `New`/`Resume` — a
half-armed stack (`guided_decomposition` without `tool_result_cap`) is refused with
`ErrMissingRequirement`, so multi-ID stack arming is expressible and enforced by
construction. `cmd/apogee/wire.go` collapses to a YAML→ID-list producer; the `mechanisms:`
file surface is unchanged.

**2 — `Deps` stay internal; the engine derives them from Config.** Everything the
constructors inject is already a function of `Config`: the library store is loaded from
`Config.LibraryDir` (only when `library` is enabled), the fingerprint is resolved from
`Config.Model`, `LookPath` defaults itself, and `GrammarConstraint` remains the inert
false seam. Because the inputs are consumed once, the builder-function failure mode —
arming the library Mechanism against a different model than the loop runs — is
unrepresentable, and nothing of `*library.Store` leaks into the v1 API.

**3 — The Mechanism descriptor goes public for arm planning.** Root aliases for
`MechanismDescriptor`, `Capability`, and `SuppressionPolicy`, plus a
`CataloguedMechanisms()` query returning the sorted descriptors of every buildable
Mechanism. The glossary already names the descriptor "the single source of truth for
which Mechanisms are exempt, which can co-fire, and which only make sense together" —
leave-the-stack-out arms derive from `Requires`, invalid arms are skipped via
`IncompatibleWith`, and the [ADR 0006](0006-bypass-mode-is-the-mechanisms-off-floor.md)
off-ramp treatment reads `Capability`, all without apogee-sim mirroring relations by
hand. Descriptors become static catalogue data (instances must keep matching their rows)
so the query never needs to build.

**4 — The error surface is matchable.** `ErrMissingRequirement` is re-exported at root
(the dual of the already-public `ErrIncompatibleMechanisms`), and unknown IDs wrap a new
`ErrUnknownMechanism` sentinel while still naming the known IDs in the message.

**5 — The surface is stable v1 API; the catalogue contents are not a contract.** The
field, the query, and the errors land as an additive minor. Which IDs exist remains "the
current best guess at what helps, decided by evidence" (CONTEXT.md) — Mechanisms may be
added or removed in minor releases with CHANGELOG notice, and a removed ID surfaces as
the loud unknown-ID error, never a silent no-op.

**6 — The catalogue stays curated.** Enable-by-ID opens *selection*, not *authorship*:
there is no public way to add a Mechanism to the catalogue
([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md) stands;
Tools remain the open extension point, experimental hooks remain the bench's candidate
surface).

## Considered options

- **Whole-binary arms via `cmd/apogee` config** — *rejected*: TUI-only today, so it needs
  new apogee work anyway, and it trades the in-process instrument for subprocess
  scraping; per-arm construction across leave-one-out subsets is combinatorial config-file
  management.
- **Experimental-hook re-implementations** — *rejected*: benches copies under the
  synthetic ID, not the shipped Mechanisms; invalid evidence for a default-ON flip.
- **A root builder function (`BuildMechanisms(ids, opts)`)** — *rejected*: `Deps` need
  `LibraryDir` and the model id, so the consumer supplies both twice (builder opts and
  Config), and a mismatch silently arms `library` against the wrong fingerprint;
  wire.go keeps a parallel path instead of collapsing.
- **A public `mechanisms` subpackage (Build/Deps/KnownIDs exported)** — *rejected*: leaks
  `*library.Store` and the fingerprint type into v1, and reads as an open Mechanism SPI
  against ADR 0002's curated-catalogue posture.
- **IDs-only export (no descriptors)** — *rejected*: apogee-sim would hand-mirror the
  stacking relations from the catalogue doc — the exact duplication the descriptor
  exists to prevent — and a future `Requires` change would surface only as a runtime
  arm failure, not at planning time.

## Consequences

- **The bench arms any stack in one Config** — catalogued IDs plus experimental hooks,
  Bypass toggled per arm with identical construction — and computes valid arm sets from
  public descriptors.
- **`benchreadiness_test.go` sheds its `internal/mechanisms` and `internal/library`
  imports**: the ADR 0001 executable contract becomes a true external-surface consumer,
  proving the enable path it previously had to cheat around.
- **`Config.Mechanisms` is repositioned by documentation, not renamed** (v1 semver): its
  external role is the experimental-hook carrier; catalogued enablement is
  `EnableMechanisms`.
- **Descriptor fields become public API**: future descriptor growth (as `Requires` was in
  ADR 0014) is an additive v1 change and must be treated with API discipline.
- **Default-off posture is unchanged (D1)**: an absent or empty `EnableMechanisms` builds
  no catalogued Mechanism; flips to default-ON remain bench-evidence-gated
  (ADR 0009).
- **No CONTEXT.md change**: the grill crystallised no new term — "enable" was already the
  canonical enable-time verb in the Mechanism-descriptor entry.

## Realisation (2026-07-05) — authorized implementation deviations

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
  spell out, consistent with "built … at construction".
- **A degraded Library store never blocks construction.** §2's "the library store is loaded from
  `Config.LibraryDir` … the way `cmd/apogee/wire.go` derives them today" is made literal: a
  corrupt or absent store degrades to an empty store with wire.go's exact `os.Stderr` notice, so
  an unreadable Library disables learning rather than failing `New`/`Resume` — the posture the
  cmd path it replaces already had.
