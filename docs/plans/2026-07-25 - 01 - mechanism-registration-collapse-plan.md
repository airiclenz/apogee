# Plan — Collapse the 21× Mechanism registration ritual: the registry holds rows

**Date:** 2026-07-25
**Status:** READY (design resolved with the owner 2026-07-25; both forks answered — shape **B**
(registry holds rows) and candidate **07** folded in as item 2). Runs in `internal/domain`,
`internal/mechanisms`, `internal/agent`, `internal/validated` and the root facade (+ ADR 0003,
ADR 0015, CONTEXT.md, CHANGELOG, TODO.md).
**Public API BREAKS** — `apogee.Mechanism` is removed and `MechanismRegistry.Add` / `.Ordered`
change shape. Deliberate, owner-chosen, cheap at `v0.8.3` (0.x = honest pre-production).
**Source:** candidate **04** of `docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`
("Collapse the 21× Mechanism registration ritual", rated Strong), plus candidate **07** ("One home
for the Mechanism-stack validity rule", Worth exploring) folded in as item 2 — it is nearly free
once the metadata is row-shaped. Candidates 01 and 02 landed 2026-07-24/25.
**Track:** post-`v0.8.0` architecture deepening — makes the Mechanism descriptor **catalogue data**
instead of instance behaviour, so a row and the Mechanism it describes cannot drift by construction
rather than by a guard test.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs. `go test ./...`, `go test -race ./...`, `gofmt -l .`, `go vet ./...` green gate every item.

---

## The problem (grounded, verified 2026-07-25)

**1. Every Mechanism repeats a 7-part ritual of near-constant size.** ID const · two-map `init()` ·
struct · constructor · package-level descriptor var · `Descriptor()` method · `Ordering()` method.
Verified counts across `internal/mechanisms/*.go` (non-test): **21** `Descriptor()` methods, **21**
`Ordering()` methods, **21** `catalogue[…]` rows, **21** `descriptors[…]` rows. Of the 21
constructors, **14** are the one-liner `func newX(Deps) (domain.Mechanism, error) { return
xMechanism{}, nil }`. For those the ritual **is** the module.

**2. Two parallel maps are hand-synced.** `catalogue` (ID → constructor, `catalogue.go:68`) and
`descriptors` (ID → descriptor, `catalogue.go:76`) are written by the same `init()` in each file —
a convention, not a constraint. Nothing stops a file registering a constructor and forgetting the
descriptor row, or keying the two rows differently.

**3. The descriptor exists three times.** As a package-level `var xDescriptor`, as a row in the
`descriptors` map, and as the return value of an instance `Descriptor()` method. They agree only by
discipline, and `TestDescriptorsMatchCatalogue` (`catalogue_test.go:210-261`) exists *solely* to
prove they still do — a test whose whole job is guarding a duplication.

**4. An ID is re-declared as a literal in a second package.**
`internal/agent/construct.go:97` — `const libraryMechanismID domain.MechanismID = "library"` — with
a doc comment that admits it: *"The catalogue owns the canonical constant (unexported there); this
is the loop's copy of the same literal."*

**5. The one Deps-bearing Mechanism is special-cased in the engine's build loop.**
`buildEnabledMechanisms` (`construct.go:115-155`) hardcodes `if slices.Contains(ids,
libraryMechanismID)` before an otherwise-uniform loop. A second Deps-bearing Mechanism means a
second engine-side special case; the catalogue — which knows what each row needs — says nothing.

**6. A cross-reference is already drifting.** `toolfilter.go:85` declares its ordering edge with the
**raw string** `"decompose"` instead of `decomposeID`, unlike every other constraint in the package
(verified: 11 non-empty `Ordering()` bodies, 10 use consts, this one does not).

**7. The stack-validity rule is implemented twice** (candidate 07). `detectIncompatibility` +
`detectRequirements` (`internal/domain/registry.go:197,223`) walk constructed Mechanisms;
`validated.Validate` (`internal/validated/validate.go:36-48`) walks catalogue descriptors. Same
rule — *no member names another member `IncompatibleWith`; every member's `Requires` peers are
present* — two sites that can drift.

**Why the metadata cannot simply be "synthesized" onto the instance.** Go cannot wrap an arbitrary
value in a decorator that adds `Descriptor()`/`Ordering()` while preserving which of the five hook
interfaces the value satisfies — embedding a type parameter is illegal, and a concrete wrapper would
have to declare all five hooks (making every Mechanism claim every hook point, breaking `Ordered`).
So the metadata either stays on the instance (supplied by an embedded helper — the rejected option
A) or **leaves the instance entirely** (this plan). There is no third shape.

**Blast radius, measured.** `domain.Mechanism` appears outside `internal/domain` at:
`apogee.go:308` (the root alias), the 21 constructor signatures + `constructor` typedef in
`internal/mechanisms`, and three engine helpers (`selfreg.go:137,244`, `hookrun.go:42`).
`.Ordered(` has **18** call sites (5 production in `hookrun.go`, the rest tests).
`registry.Add` has **2** production call sites (`construct.go:150`, and `domain`'s own) plus test
helpers in `apogee_test.go:189`, `internal/agent/mechanism_dispatch_test.go:160`,
`internal/domain/registry_ordered_test.go:52`, and 8 sites in `internal/mechanisms/*_test.go`.
`benchreadiness_test.go:334` walks `reg.Ordered(at)` and reads `Descriptor().ID`;
`example_test.go:78` asserts the `apogee.Mechanism` alias exists.

---

## Decisions (resolved with the owner 2026-07-25)

- **D1 — the registry holds rows.** `domain.RegisteredMechanism{Descriptor, Ordering, Hook}`
  replaces the self-describing `domain.Mechanism` interface. Descriptor and ordering become
  **catalogue data supplied at registration**; the Mechanism value carries only behaviour.
  Rejected: keeping the metadata on the instance behind an embedded `catalogued` helper — zero
  blast radius, but the descriptor stays instance state and problem 3's guard test keeps earning
  its keep.
- **D2 — the public break is accepted.** `apogee.Mechanism` is **removed** (not silently
  redefined — a removed alias is a loud compile error, a redefined one could mislead);
  `apogee.RegisteredMechanism` is the new alias; `MechanismRegistry.Add` and `.Ordered` change
  shape. At `v0.8.3` the only external consumer is the bench (ADR 0001), and ADR 0015 §5's
  "stable v1 API" language predates the 0.x reset.
- **D3 — ADR 0003 gains a dated `## Amendment`**, following the convention ADR 0012 already uses.
  Its Decision clause *"a Mechanism is a self-contained module that (1) implements the interface
  for one Hook point, (2) supplies a Mechanism descriptor, and (3) declares its ordering
  constraints"* stays true in substance — the module still declares all three, in its own file, in
  one `register(row{…})` literal — but clauses (2)/(3) move from *method* to *registration
  argument*. That is an ADR-level change and is recorded as one. **Locality is preserved: adding a
  Mechanism still touches only the registry line and the new module.**
- **D4 — a catalogue row declares its Deps; the engine still derives them.** `mechanisms.DepNeeds`
  + `mechanisms.DepsNeeded(ids)`. Rejected: moving the `library.NewStore` + `ResolveFingerprintFrom`
  + stderr-degrade wiring into `internal/mechanisms` — it reads better beside the row that needs
  it, but it contradicts **ADR 0015 §2** (*"Deps stay internal; the engine derives them from
  Config"*) for no gain the declaration does not already give.
- **D5 — candidate 07's shared checker returns structured defects, not a formatted error.** The two
  call sites' messages differ on purpose (domain's are loud startup errors wrapping matchable
  sentinels; `validated`'s are soft skip-and-warn prose). Only the **rule** is shared; each caller
  renders its own wording, so `validate_test.go` and the domain registry tests pass **unchanged**.
- **D6 — `register` is unexported.** ADR 0002/ADR 0015 §6: the catalogue is curated, there is no
  public way to add a Mechanism to it. Every caller is in package `mechanisms`, so `row` and its
  fields stay unexported and the literal is the documentation.
- **Names:** `domain.RegisteredMechanism` (root alias `apogee.RegisteredMechanism`) ·
  `domain.CheckStack` / `StackDefect` / `StackDefectKind` · `mechanisms.row` / `register` /
  `DepNeeds` / `DepsNeeded`.

## Explicit non-goals

- **No new registration guard.** `Add` does **not** gain an empty-`Descriptor.ID` rejection. It is
  equally possible today (a Mechanism returning a zero descriptor), so adding it here is a
  behaviour change outside this card. Park it in `TODO.md` (item 6).
- **The hook interfaces, the five hook points, the dispatch order and self-regulation are
  untouched.** ADR 0003's registry semantics (topological sort, stable canonical-ID tiebreak,
  cycle-is-a-startup-error) survive byte-for-byte; only the **authoring** and **storage** seams
  move. Every existing ordering assertion must pass unchanged.
- **`AddExperimental` / `Experimental` are untouched.** An experimental hook has no descriptor by
  definition (ADR 0002) and keeps its `map[HookPoint][]any` slot and its synthetic ID.
- **No Mechanism behaviour changes.** No `PreRequest`/`PostResponse`/… body is edited by this plan.
  If an item needs one edited, the item is wrong — stop and flag it.
- **`Deps` gains no field and loses none.** `LookPath` and `GrammarConstraint` need no derivation
  (nil-defaulted / inert), so `DepNeeds` starts with exactly one field.

---

## 1. One catalogue table — `internal/mechanisms/catalogue.go` + the 21 files' `init()` — ✅ DONE (2026-07-25)

Kill the two hand-synced maps first, while `domain.Mechanism` still exists. This item is confined
to `internal/mechanisms` and changes **no** signature outside it.

NOTES (2026-07-25): the non-empty-`Ordering()` count is **13**, not 11 — the plan's list misses
`autofix.go` (After validate / Before syntax) and `syntax.go` (After validate, autofix), whose
returns are multi-line and so escaped the single-line grep. All 13 literals + their doc comments
moved to their rows; 8 rows omit the field.
NOTES (2026-07-25): `register(r row)` files into the production catalogue and delegates to a new
`registerIn(table, r)` — the same seam shape as `Build`/`buildFrom` — so
`TestRegisterRejectsDuplicateAndEmptyID` drives a local table as the item requires.
NOTES (2026-07-25): each `Ordering()` method whose rationale comment moved to the row keeps a
one-line pointer comment ("the row is the source of record; this method is the `domain.Mechanism`
remnant") rather than being left comment-less until item 4 deletes it.
NOTES (2026-07-25): `toolfilter.go`'s ordering comment moved verbatim except its second sentence
("decompose lands in item 12; until then the edge names an absent Mechanism…"), which existed
solely to justify the raw `"decompose"` literal this item deletes; it is replaced by a note that
decompose has landed so the edge names the const. The method body's raw literal was switched to
`decomposeID` too (identical value) so the file carries no raw ID at all.
NOTES (2026-07-25): the 19 `init()` doc comments that said a Mechanism registers "in the catalogue
constructor table" now say it registers its "catalogue row" — the constructor table no longer
exists. No other prose in those files was touched.

- **`type row struct`** — `descriptor domain.MechanismDescriptor`, `ordering
  domain.OrderingConstraints`, `construct constructor`. (The `needs` field lands in item 5.) Doc
  comment: *one catalogue entry — everything the engine needs to build and register one Mechanism.
  The row is the single source of a Mechanism's metadata; the ID is `descriptor.ID`, never a
  separate key.*
- **`var catalogue = map[domain.MechanismID]row{}`** replaces **both** `catalogue` and
  `descriptors`. The `descriptors` map is **deleted**.
- **`func register(r row)`** — keyed by `r.descriptor.ID`. It **panics** on an empty ID or a
  duplicate ID: both are `init()`-time programming errors inside this package, caught by the first
  test run, never a runtime condition. Doc comment says exactly that, and says the catalogue is
  curated (ADR 0002/0015 §6) so `register` is deliberately unexported.
- **`Build`** reads `catalogue[id].construct`; **`KnownIDs`** and the `knownIDs`/`knownList`
  helpers read the one table; **`Descriptors()`** reads `catalogue[id].descriptor` (keeping
  `cloneDescriptor` and the sorted, duplicate-free contract verbatim).
- **`buildFrom`** keeps its shape as the fake-table seam but now takes
  `map[domain.MechanismID]row`.
- **Each of the 21 Mechanisms**: its `init()` becomes one `register(row{…})` call carrying the
  descriptor, the ordering literal **moved out of the `Ordering()` method**, and the constructor.
  The `Descriptor()` and `Ordering()` **methods stay for now** — `Ordering()` returns the same
  literal it always did, so the instance and the row agree; item 4 deletes them. The doc comments
  currently attached to each `Ordering()` method (several are substantial — `cot.go:114-118`,
  `toolfilter.go:80-83`, `guided_decomposition.go:113-116`, `library.go:126-128`) **move with the
  literal** onto the `ordering:` field in the row, verbatim. Nothing explaining *why* an edge
  exists may be lost.
  - Files with a **non-empty** ordering (11, verified): `cot.go` ×3 (L120/161/204), `decompose.go`
    (L202), `guided_decomposition.go` (L118), `library.go` (L130), `readrepeat.go` (L53),
    `tool_result_cap.go` (L67), `validate.go` (L53), `toolloop.go` (L51), `toolfilter.go` (L85).
    The other 10 declare `domain.OrderingConstraints{}` and simply omit the field.
- **Fix the drift found in review:** `toolfilter.go:85`'s raw `"decompose"` becomes `decomposeID`.
- **`doc.go`** (`internal/mechanisms/doc.go:8-9`): *"each Mechanism file registers its constructor
  and descriptor in its own init()"* becomes *"each Mechanism file registers one catalogue row —
  descriptor, ordering constraints and constructor — in its own init()"*.

**Tests** (`internal/mechanisms/catalogue_test.go`):

- the three `buildFrom` tests move to the `row`-shaped fake table (mechanical);
- `TestProductionCatalogueHasPortedWaves` passes **unchanged** (it drives `KnownIDs`/`Build` only);
- `TestDescriptorsMatchCatalogue` passes **unchanged** — it is still the drift guard until item 4,
  and it now also proves the single table feeds both `Descriptors()` and `Build()`;
- `TestPreRequestOrderingSeeds` passes **unchanged** — it is the acceptance oracle that no ordering
  edge was lost in the move;
- **new** `TestRegisterRejectsDuplicateAndEmptyID`: `register` panics on a duplicate ID and on an
  empty `descriptor.ID` (use `func() { defer func(){ recover() }() … }()`), driven against a local
  table, not the production catalogue.

**Acceptance:** gates green; `grep -n "descriptors\[" internal/mechanisms/` returns **nothing**;
`grep -c "func init()" internal/mechanisms/*.go` shows one `init()` per Mechanism file, each a
single `register(row{…})` call (`cot.go` registers 3 rows, `offramps`' two members register in
their own files); no signature outside `internal/mechanisms` changed. Commit:
`refactor(mechanisms): one catalogue row per Mechanism — descriptor, ordering and constructor together`.

---

## 2. One home for the Mechanism-stack validity rule — `internal/domain/stack.go` (candidate 07) — ✅ DONE (2026-07-25)

Do this while the `Mechanism` interface still exists, so it lands as an isolated, independently
reviewable change. This is candidate **07** in full.

NOTES (2026-07-25): `detectIncompatibility` keeps its pre-existing `if len(mechanisms) < 2 { return
nil }` fast path rather than becoming a bare renderer. Dropping it would be a behaviour change: a
lone Mechanism naming *itself* in `IncompatibleWith` returns nil today and would start failing at
startup, which the plan's "no Mechanism behaviour changes" non-goal forbids. The guard is not a
`present` map and does not re-implement the rule. `detectRequirements` needed no such guard (its
zero/one-member cases already agree with `CheckStack`).
NOTES (2026-07-25): `stack_test.go` adds one case beyond the plan's six — an empty `descs` slice
⇒ nil — pinning `CheckStack`'s own early return.

- **New file `internal/domain/stack.go`:**
  - **`type StackDefectKind string`** with `StackMissingRequirement` (`"missing-requirement"`) and
    `StackIncompatible` (`"incompatible"`).
  - **`type StackDefect struct{ Kind StackDefectKind; Mechanism, Peer MechanismID }`** — `Mechanism`
    is the member that *declares* the relation, `Peer` the one it names.
  - **`func CheckStack(descs []MechanismDescriptor) []StackDefect`** — the ONE implementation of
    *"is this a valid Mechanism stack?"* (CONTEXT: Mechanism descriptor). It builds the present set
    from `descs`, then walks `descs` **in the order given** and, for each, emits its `Requires`
    defects **before** its `IncompatibleWith` defects. Returns `nil` when the stack is valid.
    - **The walk order is load-bearing and must be exactly as specified** — it is what makes all
      three call sites report the same first defect they report today: `validated.Validate` checks
      Requires-then-IncompatibleWith per member (`validate.go:36-48`), and domain's two gates are
      called in sequence by `newAgent` so each only needs the first defect *of its own kind* in
      registration order.
    - Doc comment states the timing split explicitly: `validated` calls it **pre-build** over
      catalogue descriptors (soft degrade — skip + warn), the registry calls it **post-build** over
      constructed Mechanisms (loud startup failure). *Share the rule, keep the timing.*
- **`internal/domain/registry.go`** — `detectIncompatibility` and `detectRequirements` lose their
  duplicated present-set/relation walks and become thin renderers over `CheckStack`: each takes the
  descriptors of the registered Mechanisms, finds the first defect of its own kind, and returns its
  existing error **with the message and wrapped sentinel byte-for-byte unchanged**
  (`registry.go:209` and `registry.go:232`). Add a small `descriptorsOf([]Mechanism)
  []MechanismDescriptor` helper.
- **`internal/validated/validate.go`** — keeps its unknown-ID and duplicate-ID checks (the registry
  cannot have either) and replaces the Requires/IncompatibleWith double loop (`:36-48`) with one
  `domain.CheckStack(setDescriptors)` call, rendering the **first** defect with its **existing
  wording** (`mechanism %q requires %q, which is not in the set` /
  `mechanisms %q and %q are declared incompatible`).
- **No root alias.** `CheckStack` is an internal cross-package seam; ADR 0010's public surface is
  the set of root aliases and this does not join it.

**Tests:**

- **new `internal/domain/stack_test.go`** — table-driven over `CheckStack`: valid stack ⇒ nil;
  a missing requirement; an incompatible pair; a member naming an *absent* peer as incompatible
  (not a defect); **both kinds present ⇒ the Requires defect is emitted first**; a transitive
  requirement chain (A→B→C with C absent) trips on B, matching `detectRequirements`' documented
  iterative behaviour;
- `internal/domain/registry_ordered_test.go`'s incompatibility/requirement cases pass
  **unchanged** — they are the acceptance oracle for "the messages and sentinels did not move";
- `internal/validated/validate_test.go` passes **unchanged** — same, for the soft path. Its rows at
  `:27-28` pin the exact substrings `requires "a"` and `declared incompatible`.

**Acceptance:** gates green; `grep -n "IncompatibleWith\|Requires" internal/validated/validate.go`
returns **nothing** (the rule left; only the rendering stayed); `internal/domain/registry.go` no
longer builds a `present` map for either gate; both test files above are untouched by this item —
if either needs editing, the rule was not moved faithfully and the item is wrong. Commit:
`refactor(domain,validated): one checker for the Mechanism-stack validity rule`.

---

## 3. The registry holds rows — `domain.RegisteredMechanism` — ✅ DONE (2026-07-25)

The one unavoidably cross-package item. It is now **mechanical**: the catalogue already holds
descriptor + ordering (item 1), so `Build` just returns them alongside the hook. The 21 Mechanism
files are **not edited** in this item — their `Descriptor()`/`Ordering()` methods simply stop being
called (item 4 deletes them).

NOTES (2026-07-25): owner call — `domain.Mechanism` is DELETED here, not deferred, so `constructor`
became `func(Deps) (any, error)` and all 21 constructor signatures changed in this item (the branch
the item's own text flags). Item 4 is now purely the deletion of the 42 dead methods; no compile
error was left behind.
NOTES (2026-07-25): consequence of the above — the 13 `Ordering()` doc comments item 1 left saying
"this method is the `domain.Mechanism` remnant the registry still reads" now say "a remnant of the
self-describing interface; nothing reads it". Both because the registry no longer reads them and
because the item's own acceptance grep for `domain.Mechanism` must return nothing, comments included.
NOTES (2026-07-25): `TestDescriptorsMatchCatalogue`'s instance-equality half is PRESERVED (the item
forbids weakening an assertion) through an ad-hoc `m.Hook.(interface{ Descriptor()
domain.MechanismDescriptor })` assertion, since no named interface declares it any more. It is the
half item 4 deletes; a row-vs-row check on `m.Descriptor` was added beside it.
NOTES (2026-07-25): the six `internal/mechanisms` descriptor/ordering tests that read the metadata
off a RAW constructor result (`newDecompose`/`newFileHint`/`newGrammar`/`newGuidedDecomposition`/
`newToolFilter`/`newToolResultCap`, plus `library_test.go`'s `newLibraryMech`) now build through
`Build`, and `cot_test.go`'s `TestStallListIncompatible` through `mustBuild` — a raw constructor
returns behaviour only. Every assertion is kept, unchanged in substance.
NOTES (2026-07-25): fixture shapes the item did not spell out — `internal/agent`'s five dispatch
fixtures each gained a `row()` helper (call sites unchanged bar `.row()`), `selfreg_test.go`'s
hook-less `regMech` became a `regRow(id, pol…)` row builder, and `apogee_test.go`'s `mustAdd` takes
the `orderingMech` fixture and builds the row from its fields.

**`internal/domain/mechanism.go`:**

- **`type RegisteredMechanism struct{ Descriptor MechanismDescriptor; Ordering
  OrderingConstraints; Hook any }`** — doc comment: *a catalogued Mechanism as the registry holds
  it. Descriptor and Ordering are catalogue DATA supplied at registration (ADR 0003 as amended
  2026-07-25); Hook is the behaviour — a value implementing at least one of the five hook
  interfaces. Metadata and behaviour are joined once, where the row is built, so they cannot
  disagree.*
- **`type Mechanism interface{ … }` is DELETED.** Its doc paragraph about "the registry
  type-asserts which [hook]" moves onto `RegisteredMechanism.Hook`.
- **`Add(m RegisteredMechanism) error`** — same three gates in the same order, now reading the
  struct: reserved `ExperimentalMechanismID`, duplicate ID (walk `r.mechanisms`), and
  `implementsAnyHook(m.Hook)`. The three error messages stay **byte-for-byte**.
- **`Ordered(at HookPoint) []RegisteredMechanism`** — same filter-then-`topoSort`, now keyed on
  `hookImplements(at, m.Hook)`.
- **`mechanisms []RegisteredMechanism`** is the registry's storage field.

**`internal/domain/registry.go`:**

- `implementsAnyHook(hook any) bool` — takes `any`; a nil `Hook` matches no case and is rejected,
  which is the behaviour we want.
- `detectOrderingCycle`, `topoSort`, `descriptorsOf` and the item-2 gates take
  `[]RegisteredMechanism` and read the **fields** `m.Descriptor.ID` / `m.Ordering` instead of
  calling methods. No algorithm changes — Kahn's, the stable canonical-ID tiebreak, the
  defensive leftover-cycle drain and the ignore-unregistered-constraint rule are all preserved.

**`internal/mechanisms/catalogue.go`:**

- **`func Build(id domain.MechanismID, deps Deps) (domain.RegisteredMechanism, error)`** — looks the
  row up, calls `row.construct(deps)`, and returns
  `domain.RegisteredMechanism{Descriptor: row.descriptor, Ordering: row.ordering, Hook: hook}`.
  Doc comment: *this is the single place a Mechanism's metadata and its behaviour are joined.*
  `buildFrom` follows. The unknown-ID error (wrapping `domain.ErrUnknownMechanism`, naming the
  known IDs) is unchanged.
- `constructor` keeps its `func(Deps) (domain.Mechanism, error)` signature **in this item** — item 4
  changes it to `(any, error)`. Build discards nothing: the returned value becomes `Hook`.
  - *If `domain.Mechanism` is deleted in this item, `constructor` must be typed `func(Deps) (any,
    error)` immediately and the 21 constructor signatures change with it.* That is the expected
    shape — do it here, and item 4 then only deletes the 42 dead methods. **Flag it in the item's
    NOTES either way; do not leave a compile error.**

**`internal/agent`:**

- `hookrun.go` — the five `Ordered` loops: `id := m.Descriptor.ID`, and
  `hook, ok := m.Hook.(domain.PreRequestHook)` (and the four siblings). `skipUnderBypass(m
  domain.RegisteredMechanism)` reads `m.Descriptor.Capability`.
- `selfreg.go:137,244` — `suppress` / `skipMechanism` take `domain.RegisteredMechanism` and read
  `m.Descriptor.Suppression` / `m.Descriptor.ID`.
- `construct.go:150` — `registry.Add(m)` now passes the row `Build` returned; the wrapping
  `"apogee: enable mechanism %q: %w"` error is unchanged.

**`apogee.go`:**

- `type Mechanism = domain.Mechanism` (`:307-308`) is **deleted**; `type RegisteredMechanism =
  domain.RegisteredMechanism` takes its place with a doc line naming what it is. Keep it in the
  same "Mechanisms & hook points" block, in the same position.

**Tests** (all mechanical; none should change what is *asserted*):

- `internal/domain/registry_ordered_test.go` — `preReqMech`/`postRespMech` become plain hook
  structs (no `Descriptor()`/`Ordering()`), with a `regd(id, opts…) RegisteredMechanism` helper
  building the row. `orderedIDs` and `registerAll` follow. **Every assertion stays.**
- `internal/domain/mechanism_test.go` — `r.Add(preReqMech{id:"dup"})` → `r.Add(regd("dup"))`.
- `internal/mechanisms/*_test.go` — `mustBuild` (`robustness_test.go:35`) returns
  `domain.RegisteredMechanism`; `reg.Add(m)` call sites (8) are unchanged in shape; hook assertions
  like `historyhints_test.go:27-31` and `library_test.go:62,65` become `m.Hook.(domain.…Hook)`;
  `catalogue_test.go`'s `fakeMechanism` keeps only `PreRequest`.
- `internal/agent/mechanism_dispatch_test.go:160` — `mustAddMech` takes a row; `recordingMech`
  loses its metadata methods.
- `apogee_test.go` — `orderingMech` loses its two methods; `mustAdd` builds an
  `apogee.RegisteredMechanism`. `:145`'s `AddExperimental` negative case is **unchanged** (an
  experimental hook is still a bare value).
- `benchreadiness_test.go:333-334` — `orderedIndex` reads `m.Descriptor.ID`.
- `example_test.go:78` — `_ apogee.Mechanism` becomes `_ apogee.RegisteredMechanism`.

**Acceptance:** gates green including `-race`; `grep -rn "domain\.Mechanism\b" --include=*.go .`
returns **nothing** (only `MechanismID` / `MechanismDescriptor` / `MechanismRegistry` /
`MechanismFiredEvent` remain); `grep -n "apogee.Mechanism\b" *.go` returns nothing;
`TestPreRequestOrderingSeeds`, `TestDescriptorsMatchCatalogue`, `benchreadiness_test.go` and the
whole `internal/agent` suite pass with only the mechanical edits listed above. **No assertion is
deleted or weakened in this item** — if one cannot be preserved, stop and flag it. Commit:
`refactor(domain,agent): the Mechanism registry holds rows, not self-describing values`.

---

## 4. Delete the dead metadata methods — `internal/mechanisms` (21 files) — ✅ DONE (2026-07-25)

Pure subtraction, confined to one package. After item 3 nothing calls these.

NOTES (2026-07-25): the `constructor` typedef and all 21 constructor signatures were ALREADY
`func(Deps) (any, error)` — item 3 changed them there, as its own NOTES record. This item's second
bullet is therefore recorded and skipped, exactly as the bullet's parenthetical allows; no
constructor was edited.
NOTES (2026-07-25): the 13 `Ordering()` methods whose rationale item 1 had already moved to their
rows carried only a pointer comment ("a remnant of the self-describing interface; nothing reads
it"), so those comments went with their methods. The other **8** — the rows that omit the
`ordering` field — carried a catalogue Table A rationale for having *no* edge (e.g.
truncate_history's *"none — cut only at AssistantBoundaries(), never PrefixEnd()"*), which lives
nowhere else. Rather than lose it, each moved onto its row where the omitted `ordering` field would
sit, in the same shape item 1 used for the non-empty ones, reworded from "Ordering declares no
constraints" to "No ordering edge". That is the item's only addition (35 inserted lines); the
files are cachedcontent, empty_response, errorenrich, filehint, grammar, readloop,
tool_use_enforcer, truncate_history.
NOTES (2026-07-25): no *type*-level doc comment needed rephrasing — none claimed the Mechanism
"supplies" its descriptor; the surviving mentions ("the descriptor's strikes-3 policy routes self-
regulation…") describe the descriptor's content, not where it lives, and stay true.
NOTES (2026-07-25): actual `git diff --stat` on `internal/mechanisms`: **20 files changed, 35
insertions(+), 235 deletions(-)** — a net **−200** lines, matching the item's estimate.

- Delete all **21** `Descriptor()` and **21** `Ordering()` methods.
- `constructor` becomes `func(Deps) (any, error)` and the 21 constructors return `any` — they
  return the hook, not a self-describing Mechanism. (If item 3 already did this, record it and
  skip.) Constructors that already return an error (`newLibrary`, `newAutofix`, `newGrammar`) keep
  their error paths verbatim.
- The 21 package-level `xDescriptor` vars **stay** — they are the row's `descriptor:` value and the
  natural home for the "why this Capability / Suppression" doc comments. Each is now referenced
  exactly once, from its `register(row{…})` call.
- Each Mechanism's doc comment is checked for a sentence that now describes a deleted method (e.g.
  *"Descriptor returns X's static catalogue descriptor"*); such standalone method comments go with
  the method, but any *type*-level comment that says the Mechanism "supplies" its descriptor is
  rephrased to say the **catalogue row** declares it.

**Tests:**

- `TestDescriptorsMatchCatalogue` (`catalogue_test.go:210-261`) **loses its instance-equality half**
  — there is no instance descriptor left to compare. What remains (sorted, duplicate-free, one row
  per `KnownIDs()` entry, each keyed by its own ID) stays, and its doc comment is rewritten to say
  the drift it used to guard is now **unrepresentable**: one row is the single source, joined with
  the hook once in `Build`. Do not delete the test — the sorted/duplicate-free contract is still
  real API behaviour behind `CataloguedMechanisms()`.
- Everything else in `internal/mechanisms` passes unchanged.

**Acceptance:** gates green; `grep -rn "Descriptor() domain.MechanismDescriptor\|Ordering()
domain.OrderingConstraints" internal/` returns **nothing**; `internal/mechanisms` shrinks by
roughly 200 lines (report the actual `git diff --stat`); `go doc ./internal/mechanisms` shows no
new exported symbol. Commit:
`refactor(mechanisms): a Mechanism carries behaviour only — the row carries its metadata`.

---

## 5. A catalogue row declares its Deps — `internal/mechanisms` + `internal/agent/construct.go` — ✅ DONE (2026-07-25)

Closes problems 4 and 5: the duplicate ID literal and the engine's hardcoded special case.

NOTES (2026-07-25): three doc comments in `internal/mechanisms` named the Deps-deriving path as
"`buildEnabledMechanisms` (internal/agent/loop.go)" — `Deps.Library`, `Deps.GrammarConstraint` and
`library.go`'s `init()` doc. Each now names `deriveDeps` (internal/agent/construct.go) and, for the
library pair, the row's `needs` declaration. Beyond the item's literal text, but they are exactly
the sentences this item makes false (the file pointer was already stale from the earlier
loop.go → construct.go split).

NOTES (2026-07-25): the third test bullet's corrupt-store arm already exists as
`internal/agent/library_corrupt_store_test.go`'s `TestEnableMechanisms_CorruptLibraryStoreDegradesToEmpty`
(construction succeeds, exactly one stderr degrade notice, empty store injects nothing). Verified
passing against the moved `deriveDeps` path and reused rather than duplicated, as the bullet directs.

- **`internal/mechanisms/catalogue.go`:**
  - **`type DepNeeds struct{ Library bool }`** — *which construction-injected collaborators a set of
    enabled rows requires, so the engine derives exactly those and nothing else.* `Library` means
    **both** `Deps.Library` (the store) and `Deps.Fingerprint` (the model identity it keys on) —
    they are resolved together and only `library` reads either. Doc comment says the struct grows
    with `Deps`: a future Deps field that must be *derived* adds a flag here, and the row that needs
    it declares it.
  - **`row` gains `needs DepNeeds`** (zero value = needs nothing, so 20 of 21 rows omit it).
  - **`func DepsNeeded(ids []domain.MechanismID) DepNeeds`** — ORs the `needs` of every row named in
    `ids`. An ID absent from the catalogue is **skipped silently**: `Build` is the one place an
    unknown ID is reported, and it reports it loudly a moment later. Say so in the doc comment.
  - **`library.go`'s row gains `needs: DepNeeds{Library: true}`.**
- **`internal/agent/construct.go`:**
  - **`const libraryMechanismID` is DELETED**, with its apologetic doc comment.
  - `buildEnabledMechanisms` calls `deps := deriveDeps(cfg, mechanisms.DepsNeeded(ids))` and then
    loops uniformly — no `slices.Contains` special case. The `slices` import stays (still used for
    `Clone`/`Sort`).
  - **new `func deriveDeps(cfg domain.Config, needs mechanisms.DepNeeds) mechanisms.Deps`** — holds
    the library-store construction, the `Load`-degrades-to-empty stderr notice, and the
    `ResolveFingerprintFrom` identity ladder **verbatim** from `construct.go:124-143`, now gated on
    `needs.Library` instead of the ID literal. Its doc comment records the ADR 0015 §2 split: *the
    engine derives Deps from Config; the CATALOGUE declares which rows need what.* The long
    `buildEnabledMechanisms` doc comment sheds the paragraph about the `library`-only special case
    and points at `deriveDeps`.

**Tests** (`internal/agent/enable_mechanisms_test.go` and the library tests):

- the existing assertions that a **non-`library`** arm wires **no** store, and that a `library` arm
  wires one Loaded from `Config.LibraryDir`, pass **unchanged** — they are the acceptance oracle
  (the deleted const's own doc comment names them as its guard);
- **new** `TestDepsNeeded` in `internal/mechanisms`: `DepsNeeded(nil)` is the zero `DepNeeds`;
  `DepsNeeded([]{"validate"})` is zero; `DepsNeeded([]{"validate","library"})` has `Library: true`;
  an unknown ID is skipped rather than panicking;
- **new**: a `library`-enabled arm with a **corrupt** store directory still constructs (the
  degrade-to-empty path moved intact) — if an equivalent test already exists, verify and reuse it
  rather than duplicating.

**Acceptance:** gates green; `grep -rn "libraryMechanismID\|\"library\"" internal/agent/` returns
**nothing** outside test data; `buildEnabledMechanisms` contains no Mechanism ID literal and its
loop body is uniform for every ID. Commit:
`refactor(agent,mechanisms): a catalogue row declares its Deps — the engine loops uniformly`.

---

## 6. Record it — ADR 0003 amendment, ADR 0015 note, CONTEXT.md, docs, CHANGELOG, TODO, ledger

- **`docs/adr/0003-mechanisms-are-a-constraint-declared-registry-not-a-fixed-pipeline.md`** — append
  `## Amendment (2026-07-25) — a Mechanism's descriptor and ordering are catalogue data, not
  instance methods`, following ADR 0012's amendment convention. Content: the Decision's three
  clauses stand, but (2) and (3) are supplied **at registration** (one `register(row{…})` literal in
  the Mechanism's own file) rather than through `Descriptor()`/`Ordering()` methods; the registry
  stores `RegisteredMechanism{Descriptor, Ordering, Hook}`. State what is preserved — the
  self-contained module, the locality property (*"adding a Mechanism touches the registry + the new
  module"*), the deterministic total order, the stable canonical-ID tiebreak, and cycle-as-startup-
  error — and what is bought: the descriptor is joined to its behaviour **once**, in `Build`, so
  row/instance drift is unrepresentable rather than test-guarded. Record the mechanical reason the
  alternative was impossible (Go cannot decorate a value with metadata methods while preserving
  which hook interfaces it satisfies). Note the public break (D2).
- **`docs/adr/0015-catalogued-mechanisms-are-enabled-by-id-through-config.md`** — a short dated note
  under the existing `## Realisation` section (not a new amendment): §3's *"instances must keep
  matching their rows"* is now vacuous — there is no instance descriptor — and §5's "stable v1 API"
  language is read against the 0.x reset, under which this change ships as a documented break.
  §2 (Deps derived by the engine from Config) is **reaffirmed**, not changed: item 5 moves only the
  *declaration* of which rows need what.
- **`CONTEXT.md` — "Mechanism descriptor" entry (~L364-370)**: it currently reads as per-Mechanism
  metadata. Add that the descriptor is **catalogue data supplied at registration**, the single
  source both the runtime registry and the public `CataloguedMechanisms()` query read. Prose only,
  no implementation detail, **no new headword** — the grill crystallised no new concept.
- **`internal/mechanisms/doc.go`** — already touched in item 1; verify it reads correctly after
  items 4–5 and mentions that a row may declare the Deps it needs.
- **`internal/domain/doc.go`** (`:3` mentions "the Mechanism registry's" pure logic) — check and
  extend only if it enumerates the interface; do not force.
- **`CHANGELOG.md` `## [Unreleased]`** — a `### Changed` **Breaking (Go API)** entry: `apogee.Mechanism`
  is removed in favour of `apogee.RegisteredMechanism`; `MechanismRegistry.Add` takes a
  `RegisteredMechanism` and `Ordered` returns them; a Mechanism value now carries behaviour only.
  Say plainly that **no shipped behaviour changes** — the same Mechanisms fire in the same order
  under the same gates — and that the CLI/TUI and the `mechanisms:` config surface are unaffected.
- **`TODO.md`** — park the two named non-goals: (a) `Add` does not reject an empty
  `Descriptor.ID`, pre-existing and worth a guard later; (b) `internal/mechanisms` could own its
  Deps *construction* (not just their declaration) if a second Deps-bearing Mechanism arrives —
  rejected here as ADR 0015 §2 conflict, recorded so the door is documented rather than silently
  shut.
- **`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`** — mark candidates **04**
  and **07** landed with this plan's date, exactly as 01 and 02 were handled: a `✅ **LANDED
  2026-07-25**` suffix on each heading, a bullet on each recording what was actually built (04 as
  the row-shaped registry rather than the card's "methods synthesized" sketch; 07 as
  `domain.CheckStack` with the timing split kept), and refresh the **Ledger** and **Recommended
  next step** paragraphs (03, 05, 06 remain outstanding; 05 stays the urgency pick). Leave 03/05/06
  untouched and leave the companion `.html` alone.

**Tests:** none (docs only). Re-run the full gate to catch a doc-comment typo that breaks `go vet`.

**Acceptance:** ADR 0003 renders with its amendment; CONTEXT.md contains no implementation detail
and no new headword; the review doc no longer lists 04 or 07 as outstanding; the CHANGELOG entry
names the break in the user's terms. Commit:
`docs(adr,context,changelog): record the Mechanism registry's row shape`.

---

## Verification (whole plan)

1. `go test ./... && go test -race ./... && gofmt -l . && go vet ./...` — green.
2. `grep -rn "domain\.Mechanism\b" --include=*.go .` returns **nothing**.
3. `grep -rn "descriptors\[" internal/` returns **nothing** — one table, not two.
4. `grep -rn "Descriptor() domain.MechanismDescriptor\|Ordering() domain.OrderingConstraints"
   internal/` returns **nothing** — 42 methods gone.
5. `grep -rn "libraryMechanismID" internal/` returns **nothing** — the duplicated ID literal is gone.
6. `grep -n "IncompatibleWith\|Requires" internal/validated/validate.go` returns **nothing** — the
   stack rule has one home.
7. **The behavioural pin:** `TestPreRequestOrderingSeeds` (`internal/mechanisms/catalogue_test.go`),
   `internal/domain/registry_ordered_test.go`, `internal/validated/validate_test.go` and
   `benchreadiness_test.go` all pass. Together they assert the dispatch order, the stable tiebreak,
   the three startup gates, the soft-degrade path and the public enable surface — i.e. that this
   plan changed **only** where the metadata lives.
8. `git diff --stat` on `internal/mechanisms` shows a net **reduction** (expect ~200 lines). Report
   the real figure; a net increase means the ritual was reshaped, not collapsed, and is a finding.
9. Manual: build the TUI and run one Turn with `mechanisms:` enabling `guided_decomposition` +
   `tool_result_cap` (the ADR 0014 stack) — it starts; enabling `guided_decomposition` alone still
   fails loudly with `ErrMissingRequirement`; enabling `stall_nudge` + `list_nudge` still fails
   with `ErrIncompatibleMechanisms`; a typo'd ID still fails naming the known set.
