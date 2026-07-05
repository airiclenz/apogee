# Plan — The public Mechanism enable surface (`Config.EnableMechanisms`, ADR 0015)

**Date:** 2026-07-05
**Status:** ready to implement — not started.
**Track:** post-`v1.2.0` public-surface extension motivated by the bench campaign (the
2026-07-05 handoff's path (b)). Purely **additive** under v1 semver; no breaking change to
any exported name. This plan ends with an external module able to arm any catalogued
Mechanism stack in-process; the bench campaign itself is apogee-sim work and out of scope.

**Authoritative sources (precedence):** if any item text disagrees with
[ADR 0015](../adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md) or the
`CONTEXT.md` entries it cites (Mechanism, Mechanism descriptor, The bench, Experimental
hook), **the ADR and CONTEXT.md win** — they are the ratified ground truth from the
2026-07-05 grill. Existing behaviour contracts cited below (validation boundary, YAML
surface, Bypass semantics) are grounded at their file:line and win over any paraphrase
here.

## Where things stand (grounded, verified 2026-07-05)

- **`Config`** (`internal/domain/config.go`): public via the root alias (`apogee.go:67`,
  `type Config = domain.Config`). Carries `Endpoint`, `Model` (:16-17), `Bypass` (:21),
  `Mechanisms *MechanismRegistry` (:48), `LibraryDir` (:59). There is **no enable-by-ID
  field yet**.
- **The catalogue** (`internal/mechanisms/catalogue.go`): `catalogue` constructor table
  (:65), `Build(id, deps)` (:71, loud unknown-ID error naming `KnownIDs()` at :84),
  `KnownIDs()` (:77). ~21 rows registered via per-file `init()` (`catalogue[fooID] =
  newFoo`). **Descriptors are instance-only** — `Mechanism.Descriptor()`
  (`internal/domain/mechanism.go:99`) is a method on built instances; there is no static
  descriptor data, and `newLibrary` refuses nil-store Deps, so descriptors cannot be
  harvested by zero-building.
- **`Deps`** (`internal/mechanisms/catalogue.go:19-52`): `Library *library.Store` (built
  only when `library` is enabled), `Fingerprint domain.ModelFingerprint`, `LookPath`
  (nil ⇒ `exec.LookPath`), `GrammarConstraint` (inert false everywhere in production).
  `cmd/apogee/wire.go` derives all of it: store rooted at the injected `LibraryDir`
  (:308 area), `library.ResolveFingerprint(model)` (:324).
- **The cmd enable path** (`cmd/apogee/wire.go:253-305`, `buildMechanismRegistry`): sorts
  enabled YAML keys for deterministic order (:274-278), validates enabled AND disabled
  keys against the known set (:280-289), Builds and Adds each enabled ID (:294-303). The
  YAML `mechanisms:` map is file-only (`cmd/apogee/config.go:232-237`).
- **Validation boundary** (`internal/agent/loop.go:54-62`): `ValidateOrdering` →
  `ValidateIncompatibilities` → `ValidateRequirements`, in that order, at agent
  construction — shared by `New` and `Resume`. `ErrMissingRequirement`
  (`internal/domain/errors.go:39`) is **not** re-exported at root (its dual
  `ErrIncompatibleMechanisms` is, `apogee.go:409`); unknown-ID has **no sentinel** (a
  `fmt.Errorf` in `Build`).
- **Registry** (`internal/domain/mechanism.go:189-207`): `Add` rejects the reserved
  `experimental` ID, duplicate IDs (:195-197, "already registered"), and hook-less
  Mechanisms. `AddExperimental` (:209) is the experimental-hook carrier. Insertion order
  does not decide firing order (`Ordered` derives the constraint-declared total order,
  ADR 0003) — but sorted build order keeps error surfaces deterministic (the wire.go
  precedent).
- **The executable embedding contract** (`benchreadiness_test.go`): package
  `apogee_test`, drives the loop via the public facade, but arms Mechanisms by importing
  `internal/mechanisms` + `internal/library` (:33-34) — its header (:11-18) explicitly
  apologises for this ("apogee-sim, a separate module, cannot import internal/*").
- **The consumer is real and waiting**: apogee-sim's `internal/coreagent` drives the loop
  through the public API with an empty registry today (`replace
  github.com/airiclenz/apogee => ../apogee` in its go.mod).
- **CHANGELOG**: `[Unreleased]` currently holds the guided-decomposition entries; this
  plan's items add theirs under one new heading there (the next cut is a `v1.3.0`,
  owner's call).

### Decisions locked with the owner (grill, 2026-07-05)

1. **Shape: `EnableMechanisms []MechanismID` on `Config`** — not a root builder function,
   not a public `mechanisms` subpackage (ADR 0015 Considered options). `New`/`Resume`
   build the IDs; one enable path for CLI, bench, and embedders.
2. **Merge semantics:** enabled IDs are built **into** the consumer-provided
   `Config.Mechanisms` registry (or a fresh one when nil) so catalogued Mechanisms and
   experimental hooks coexist in one arm. Duplicates surface via the registry's existing
   "already registered" rejection.
3. **`Deps` stay internal, engine-derived** from `Config.LibraryDir` / `Config.Model`
   exactly as wire.go derives them today; `GrammarConstraint` stays inert-false,
   `LookPath` stays defaulted. No Deps type appears in the public API.
4. **Full descriptors go public**: `MechanismDescriptor` / `Capability` /
   `SuppressionPolicy` root aliases (with their constants) + `CataloguedMechanisms()`
   sorted query, backed by static descriptor rows beside the constructors.
5. **Errors:** re-export `ErrMissingRequirement`; add a matchable `ErrUnknownMechanism`
   sentinel that the unknown-ID error wraps (message still names the known IDs).
6. **Semver posture:** the surface is stable v1 API (additive minor); catalogue
   *contents* are data, not contract — IDs may change in minors with CHANGELOG notice.
7. **No CONTEXT.md change** — the grill crystallised no new term; "enable" is already
   canonical in the Mechanism-descriptor entry. (Recorded so no later item "fixes" it.)

---

## 1. Mechanisms: descriptors become static catalogue data + the unknown-ID sentinel — ✅ DONE (2026-07-05)

**What:** make every catalogued Mechanism's descriptor available without building.
Restructure each mechanism file so its descriptor is a single package-level
`domain.MechanismDescriptor` value that BOTH the instance's `Descriptor()` method returns
AND the `init()` registers beside the constructor (single source per file — equality by
construction, not by duplicated literals; the exact registration mechanics are the
implementer's, e.g. a two-field row struct or a parallel `descriptors` map keyed like
`catalogue`). Add `Descriptors() []domain.MechanismDescriptor` to the package (sorted by
ID, returning copies) next to `KnownIDs()` (`catalogue.go:77`). Add
`ErrUnknownMechanism` to `internal/domain/errors.go` (house comment style, near
`ErrMissingRequirement` :39) and make `Build`'s unknown-ID error (`catalogue.go:84`) wrap
it while still naming the known IDs.

**Authoritative source:** ADR 0015 §3 (static descriptor data; instances must keep
matching their rows), §4 (sentinel); locked decisions 4–5.

**Tests:** invariant test in `internal/mechanisms`: for every `KnownIDs()` entry there is
a descriptor row, its `ID` equals the catalogue key, and `Descriptors()` is sorted and
duplicate-free; where a Mechanism is buildable with benign Deps (all but `library`),
assert the built instance's `Descriptor()` equals the static row — and for `library`,
assert it against a store built in `t.TempDir()` (the `catalogue_test.go` fake-Deps
patterns). `errors.Is(err, domain.ErrUnknownMechanism)` on a bogus Build ID; error text
still names known IDs. Existing catalogue/descriptor tests stay green.

**Acceptance:** gates green; diff confined to `internal/mechanisms`,
`internal/domain/errors.go` + CHANGELOG. Commit:
`refactor(mechanisms): static descriptor rows, Descriptors query, unknown-ID sentinel`.

---

## 2. Engine: `Config.EnableMechanisms` + the internal build path in agent construction — ✅ DONE (2026-07-05)

**NOTES (2026-07-05):** the item text names only `New`/`Resume`, but `newAgent` — the shared
construction path the build was placed in — is ALSO the sub-agent construction path
(`subagent.go` `newChildAgent`, which copies the parent `Config`). A child inheriting the
parent's `EnableMechanisms` plus its shared `Mechanisms` registry would re-Add the
already-built Mechanisms and trip the duplicate rejection on every sub-agent spawn. Fixed in
`newChildAgent` (in-scope, `internal/agent`): the child now inherits the parent's
already-built `a.registry` and clears `EnableMechanisms`, so it fires the same Mechanisms
without re-building. The library-store `Load` error is degraded to an empty store with
wire.go's exact `os.Stderr` notice (faithful to "the way wire.go does today").

**What:** add `EnableMechanisms []MechanismID` to `Config`
(`internal/domain/config.go`, beside `Mechanisms` :48, doc comment per ADR 0015 §1: named
catalogued Mechanisms built at construction; unknown IDs and half-armed stacks fail
`New`). In the shared construction path used by both `New` and `Resume` (the site that
runs the :54-62 validations), BEFORE those validations: sort a copy of the IDs
(deterministic, wire.go precedent :274-278), derive `mechanisms.Deps` the way wire.go
does today (store loaded from `Config.LibraryDir` **only when** `library` is among the
enabled IDs — wire.go :308 area; `library.ResolveFingerprint(Config.Model)` :324;
`LookPath` nil; `GrammarConstraint` false), `mechanisms.Build` each ID and `Add` it into
`Config.Mechanisms`' registry — creating a fresh registry when nil (locked decision 2).
Errors propagate as construction failures: unknown ID (item-1 sentinel), duplicate ID
(registry's :195-197 rejection — covers an ID listed twice AND an in-repo caller who
pre-built the same Mechanism), then the existing ordering/incompatibility/requirements
gates run over the merged registry unchanged. Empty/nil `EnableMechanisms` builds nothing
(D1 default-off posture untouched). Do NOT touch cmd/apogee in this item — wire.go's own
path stays until item 3 removes it (transient duplication across items is expected).

**Authoritative source:** ADR 0015 §1–2; locked decisions 1–3; the validation boundary at
`internal/agent/loop.go:54-62`.

**Tests:** in `internal/agent` (scripted-upstream style): `New` with a valid ID list
registers exactly those Mechanisms (observe via `MechanismFiredEvent`/registry effects,
not internals); `guided_decomposition` without `tool_result_cap` →
`errors.Is(err, domain.ErrMissingRequirement)`; bogus ID →
`errors.Is(err, domain.ErrUnknownMechanism)`; ID listed twice → the duplicate rejection;
IDs + a provided registry carrying an experimental hook → both are live (merge, not
replace); nil and empty lists build nothing; `Resume` with the same Config arms
identically (mechanisms are config, not session state); enabling `library` with a
temp `LibraryDir` builds (store constructed, no error) while enabling it with an empty
`LibraryDir` behaves exactly as wire.go's path would today (assert parity, don't invent
policy); a non-`library` list never touches `LibraryDir`.

**Acceptance:** gates green (incl. `-race` on `internal/agent`); diff confined to
`internal/domain`, `internal/agent` + CHANGELOG. Commit:
`feat(domain): Config.EnableMechanisms — catalogued mechanisms built at construction`.

**Depends on:** item 1.

---

## 3. cmd/apogee: wire.go collapses to a YAML→ID-list producer

**What:** replace `buildMechanismRegistry` (`cmd/apogee/wire.go:253-305`) and the cmd-side
Deps derivation (:308-324) with: validate the YAML map's keys (enabled AND disabled,
preserving the :280-289 behaviour — a typo'd *disabled* key must still fail loudly; this
stays cmd-side because the engine only ever sees enabled IDs), then set the sorted enabled
IDs as `Config.EnableMechanisms` and let the engine build. The YAML `mechanisms:` surface
(`cmd/apogee/config.go:232-237`), `defaults/config.yaml` (incl. the commented
guided-decomposition STACK example), and every user-visible behaviour are unchanged —
same errors at the same startup boundary, possibly different `%w` chains. Delete what
becomes dead (the cmd-side store/fingerprint derivation) rather than stranding it.

**Authoritative source:** ADR 0015 §1 ("wire.go collapses"; "the `mechanisms:` file
surface is unchanged"); locked decision 3.

**Tests:** existing `wire_test.go` / `config_test.go` / `guided_decomposition_test.go`
(cmd) assertions stay semantically green — update error-text/chain assertions only where
the wrapping changed, never the boundary (startup) or the named IDs; unknown enabled key,
unknown disabled key, half-stack, and incompatibility cases all still refuse to boot; the
booting-stack case still boots.

**Acceptance:** gates green; diff confined to `cmd/apogee` + CHANGELOG. Commit:
`refactor(cmd): wire mechanisms through Config.EnableMechanisms`.

**Depends on:** item 2.

---

## 4. Root facade: the public surface

**What:** in `apogee.go` (the thin facade, ADR 0010 — root may import internal, never the
reverse): alias `MechanismDescriptor`, `Capability`, `SuppressionPolicy` and re-export
their constant values (the Capability values ADR 0006 gates Bypass by, and the
suppression-policy values — exact names from `internal/domain/mechanism.go`); add
`CataloguedMechanisms() []MechanismDescriptor` returning item 1's sorted copies; re-export
`ErrMissingRequirement` and `ErrUnknownMechanism` in the root error block (:395-425 area,
house comment style — `ErrMissingRequirement` documented as the dual of
`ErrIncompatibleMechanisms` :409). Reposition the `Config.Mechanisms` doc comment
(`internal/domain/config.go:48`): the experimental-hook carrier; catalogued enablement is
`EnableMechanisms` (v1 semver forbids a rename — locked with the owner). Add an
`example_test.go` Example arming the `guided_decomposition + tool_result_cap` stack and an
Example computing a leave-the-stack-out arm from `CataloguedMechanisms()` (`Requires`
traversal — the bench's planning idiom, compiling documentation).

**Authoritative source:** ADR 0015 §3–4; locked decisions 4–5; ADR 0010 (facade
direction).

**Tests:** the Examples compile and run (hermetic); a root-level test asserts
`CataloguedMechanisms()` is non-empty, sorted, and contains `guided_decomposition` with
`Requires: [tool_result_cap]` (the ADR 0014 relation, asserted through the PUBLIC surface
only); `errors.Is` works through the root exports.

**Acceptance:** gates green; diff confined to `apogee.go`, `example_test.go` (+ a root
test file if needed) + CHANGELOG. Commit:
`feat(apogee): public mechanism descriptors, catalogue query, and matchable enable errors`.

**Depends on:** items 1, 2.

---

## 5. benchreadiness: the executable contract stops cheating

**What:** migrate `benchreadiness_test.go` to the public surface: arm every arm via
`Config.EnableMechanisms` (+ `AddExperimental` for the hook arms, unchanged), drop the
`internal/mechanisms` and `internal/library` imports (:33-34) and the manual
Deps/registry construction (`armRegistry`, :237-262 area), and rewrite the header comment
(:11-18) — the apology becomes the statement that a separate module can now do everything
this test does (`internal/session`, if still needed for snapshot-schema inspection, is a
separate concern — verify, don't force). Add the acceptance the campaign needs, all
through public API: a half-stack arm refuses construction
(`errors.Is(err, apogee.ErrMissingRequirement)`); a bogus-ID arm refuses
(`apogee.ErrUnknownMechanism`); a catalogued+experimental combined arm fires both
(existing event-order assertions extended); a leave-the-stack-out arm set computed from
`apogee.CataloguedMechanisms()` constructs successfully for every member; the Bypass arm
still shows zero non-exempt catalogued fires (ADR 0006 floor) under identical
construction.

**Authoritative source:** ADR 0015 Consequences ("becomes a true external-surface
consumer"); ADR 0001 (the test IS the embedding contract); ADR 0006/0009 (what the arms
must prove).

**Tests:** this item IS tests. Gate: `go test -race` on the root package passes; `grep`
proves no `internal/mechanisms` or `internal/library` import remains in
`benchreadiness_test.go`.

**Acceptance:** gates green; diff confined to `benchreadiness_test.go` + CHANGELOG.
Commit: `test(bench): benchreadiness arms mechanisms through the public enable surface`.

**Depends on:** items 2, 4.

---

## 6. Docs close-out (the one owning item for every cross-cutting doc edit)

**What:** (a) CHANGELOG: reconcile items 1–5's lines under one `[Unreleased]` heading for
the enable surface (the guided-decomposition rollup precedent — one coherent entry, the
next cut is `v1.3.0`, owner's call). (b) ADR 0015: append a dated **Realisation** note
ONLY for authorized deviations recorded in items' NOTES lines (the ADR 0014 precedent:
design refinements, not mechanical test fixes); if there are none, no note. (c) Verify
`docs/design/mechanism-catalogue.md` and README statements about how Mechanisms are
enabled — if either describes the cmd/YAML path as the *only* path, amend minimally to
name the library surface (verify, don't invent; the YAML surface itself is unchanged).
(d) CONTEXT.md: **no edit** (locked decision 7) — confirm and leave it alone. (e) Confirm
the 2026-07-05 handoff's "record the chosen path when the bench harness lands" is
satisfied by ADR 0015 + this plan (no extra doc).

**Authoritative source:** the plan-author convention (every cross-cutting doc amendment
has exactly one owning item); ADR realisation-note precedent (ADR 0013/0014).

**Tests:** none (docs). Verify: links resolve; the CHANGELOG entry reads as one feature.

**Acceptance:** diff confined to docs (CHANGELOG.md, docs/adr/0015, and only-if-needed
docs/design/mechanism-catalogue.md, README.md). Commit:
`docs: mechanism enable surface close-out`.

**Depends on:** items 1–5.

---

## Explicitly NOT in this plan

- **The bench campaign itself** (the two-arm aggregate, leave-one-out arms, the Library
  longitudinal experiment) — apogee-sim work; this plan only makes it possible.
- **Any default-ON flip** — ADR 0009 evidence only.
- **A public Mechanism-authorship SPI** — ADR 0002 stands; selection opened, authorship
  not (ADR 0015 §6).
- **A `cmd/apogee` headless subcommand** — path (a)'s prerequisite, rejected with it.
- **Renaming `Config.Mechanisms`** — v1 semver; doc-comment repositioning only (item 4).
- **`GrammarConstraint` activation or any Deps growth** — inert seams stay inert.
- **Catalogue content changes** (new/removed Mechanisms, constant tuning) — separate
  tracks, bench-gated.

## Critical files

**New:** `docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md`
(written at ratification), `docs/plans/mechanism-enable-surface-plan.md` (this file).
**Modified:** `internal/mechanisms/*.go` (descriptor rows; catalogue.go),
`internal/domain/errors.go` (`ErrUnknownMechanism`), `internal/domain/config.go`
(`EnableMechanisms`), `internal/agent/loop.go` (build path at the validation site),
`cmd/apogee/wire.go` (collapse), `apogee.go` + `example_test.go` (facade),
`benchreadiness_test.go` (migration), `CHANGELOG.md`, and only-if-needed
`docs/design/mechanism-catalogue.md` / `README.md` (item 6c).
