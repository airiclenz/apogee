---
title: ADR 0015 decision
---

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
false seam [retired 2026-08-29 — the seam was never populated and was removed with the
grammar Mechanism]. Because the inputs are consumed once, the builder-function failure mode —
arming the library Mechanism against a different model than the loop runs — is
unrepresentable, and nothing of `*library.Store` leaks into the v1 API.

**3 — The Mechanism descriptor goes public for arm planning.** Root aliases for
`MechanismDescriptor`, `Capability`, and `SuppressionPolicy`, plus a
`CataloguedMechanisms()` query returning the sorted descriptors of every buildable
Mechanism. The glossary already names the descriptor "the single source of truth for
which Mechanisms are exempt, which can co-fire, and which only make sense together" —
leave-the-stack-out arms derive from `Requires`, invalid arms are skipped via
`IncompatibleWith`, and the ADR 0006
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
(ADR 0002 stands;
Tools remain the open extension point, experimental hooks remain the bench's candidate
surface).
