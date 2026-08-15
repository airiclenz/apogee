# Open-defects plan — the 2026-08-15 residuals sweep's three findings

- **Goal:** close all three open defects in `ISSUES.md` (the "Run residuals — open
  (2026-08-15, residuals + response-reserve sweep)" section): the NaN hole in
  `internal/context.Allocate`'s fraction guard, the tautological/incomplete kind-projection
  test, and the missing `response-reserve:` rebind ride + its missing arrival-site tests.
- **Date:** 2026-08-15
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `ISSUES.md` "Open defects" section (the three bullets this plan removes).
  - `docs/plans/archived/2026-08-15 - 03 - residuals-response-reserve-plan.md` — the plan
    whose residuals these are; its items 11/12 record why the guard and the validator were
    split, and its lines ~468–473 describe the pre-ceiling-field `max-output-tokens:`
    behaviour that item 4 here mirrors for the reserve.
  - Commit `cbbdfc9` (per-server response-reserve override rides every rebind) — the landed
    precedent items 3–4 extend.
  - The `MaxOutputTokens *int` ceiling-field precedent: `internal/agent/rebind.go:89`
    (field), `:156-158` (apply), `cmd/apogee/wire_settings.go:1064` (fill),
    `cmd/apogee/wire_test.go:4360` / `:4442` (ride tests),
    `internal/agent/rebind_test.go:431` (engine test).
- **Ratified design calls:**
  - Scope = all three open defects (owner, 2026-08-15, via AskUserQuestion at plan-write
    time).
  - The `RebindSpec` reserve field copies the `MaxOutputTokens` ceiling-field design
    exactly — pointer type, nil = the spec says nothing, stated 0 = drop to apogee's
    default — because `ISSUES.md` itself frames the defect as "the same gap
    `max-output-tokens:` had before its ceiling field" (plan author, 2026-08-15, following
    the landed precedent; not a new design).
  - Guard shape in `Allocate`: `!(fraction > 0 && fraction < 1)` — positive-form
    comparison, no `math` import needed, same semantics as
    `config.isResponseReserveShare` for NaN (plan author, 2026-08-15; mechanical).
- **Standing requirements:** skills: coding-standards. Any authorized deviation from item
  text lands as a dated NOTES line under the item.
- **Out of scope:**
  - Guarding NaN at the other engine doors (`internal/agent/rebind.go:290`
    `SwitchUpstream`, the construction `Config` at `internal/agent/agent.go:250`): after
    item 1 the `Allocate` guard is the defensive floor beneath every door — that is the
    floor's documented job (`internal/context/budget.go:64-66`); no per-door validation.
  - Making the top-level `response-reserve:` key live-editable. It stays file-only by
    design (`liveSettings.pinnedReserve` has no setter, per its field doc); item 4 only
    pins the existing refusal with a test.
  - Everything under `ISSUES.md` "Parked / deferred work".
  - Any version bump (see the closing note).

## 1. Treat NaN as unset in `Allocate`'s fraction guard — ✅ DONE (2026-08-15)

NOTES (2026-08-15): CHANGELOG.md is listed in the item's Files but the implementer never edits it — the entry text above is for the verifier to apply.
NOTES (2026-08-15): also refreshed the doc comment on `TestAllocate_ReservePrecedence` to name NaN, since "outside that range" did not describe an input that compares false to every bound; `Allocate`'s own doc comments were left untouched as the item directs.

**What:** In `internal/context/budget.go:76`, replace the guard
`if fraction <= 0 || fraction >= 1` with `if !(fraction > 0 && fraction < 1)`, so NaN —
which compares false to everything and today slips past both comparisons — falls to
`defaultReserveFraction` like every other non-share value. Today a NaN fraction reaching a
zero-reserve call produces `int(NaN)` at `budget.go:79`, which is
implementation-dependent: on arm64 it saturates to a silent zero reserve; on amd64 it
yields `math.MinInt64`, making `working` negative and every split field garbage — which
among other things disarms the automatic compaction trigger
(`internal/context/budget.go:183` treats negative History as "no basis"). The fix also
makes two existing comments true again: the closed-contract claim at `budget.go:64-66`
and `internal/config/config.go:1791-1799`'s "(internal/context.Allocate guards the same
way, for the same reason)" — neither comment needs an edit, only the code beneath them.
Remove the first bullet of the `ISSUES.md` open-defects section (the
`internal/context.Allocate` one, currently `ISSUES.md:30-32`) and record the fix under
`[Unreleased]` in `CHANGELOG.md`.

**Files:** `internal/context/budget.go`, `internal/context/budget_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** In `internal/context/budget_test.go`, matching the file's conventions
(table-driven, anonymous struct with `name`, `t.Run`, plain `t.Errorf`, no
`t.Parallel()`; `math` is already imported):
- Add a `math.NaN()` row to `TestAllocate_ReservePrecedence` (`:88`): window set,
  reserve 0, fraction NaN → `wantReserve = int(0.20 * window)` (the default), not 0 and
  not garbage.
- Add a row pinning that an explicit token reserve still outranks a NaN fraction
  (reserve > 0, fraction NaN → the explicit reserve is honoured; the fraction branch is
  never entered).
- Add a NaN row to `TestAllocate_ReserveHonouredAndPartsSum` (`:14`) so the parts-sum
  invariant is asserted for the NaN input too.

**Acceptance:** `go build ./internal/context/ && go test ./internal/context/ -run TestAllocate -v`

**Commit:** `fix(context): treat NaN as unset in Allocate's fraction guard`

## 2. Pin the settings kind projection independently for all nine kinds

**What:** Test-only. In `cmd/apogee/settingsrows_test.go`, the kind-projection map at
`:409-417` inside `TestSettingsRowsProjectRegistryMetadata` lists 7 of the 9
`config.Kind` values; complete it with the three missing edges, stated independently of
`settingKind` (the production projection at `cmd/apogee/settingsrows.go:238-266`):
- `config.KindFloat: tui.SettingInt` (the float kind deliberately reuses the caret-buffer
  idiom; there is no `tui.SettingFloat`),
- `config.KindText: tui.SettingText`,
- `config.KindScheme: tui.SettingEnum`.

Rewrite the map's comment at `:405-408`: it claims a "closed projection" and names only
one many-to-one edge (`KindStringList → SettingString`), while production has three
(`KindStringList`, `KindFloat → SettingInt`, `KindScheme → SettingEnum`) — name all
three. Add an exhaustiveness guard in the same test: iterate `config.KeyRegistry`,
collect each key's `Kind`, and fail if any kind in use is absent from the literal map —
so a future kind cannot silently join the uncovered set (there is no `allKinds` slice in
the repo; the registry is the only enumeration). Add one concrete row-level assertion in
the style of `:130-137`: `byPath["response-reserve"].Kind == tui.SettingInt` (the one
`KindFloat` row, `internal/config/registry.go:269-274`) — this is the direct pin the
tautological row-level loop at `:392-404` cannot provide (its kind clause computes the
expectation via `settingKind` itself; leave that loop as-is — its non-kind clauses
compare against the registry, which is independent). Remove the second bullet of the
`ISSUES.md` open-defects section (the kind-projection one) and record the change under
`[Unreleased]` in `CHANGELOG.md`.

**Files:** `cmd/apogee/settingsrows_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the item is its own tests (above).

**Acceptance:** `go test ./cmd/apogee/ -run TestSettingsRows -v`

**Commit:** `test(settings): pin the kind projection independently for all nine kinds`

## 3. `RebindSpec` carries the response-reserve share (engine half)

**What:** In `internal/agent/rebind.go`, add `ResponseReserveFraction *float64` to
`RebindSpec` (fields at `:66-100`), mirroring the `MaxOutputTokens *int` ceiling field at
`:89` exactly: pointer, nil = the spec says nothing (the current share stands), stated
value = replace, stated 0 = the absent state (Allocate then applies apogee's own 0.20
default). Apply it in `Rebind` beside the ceiling's apply at `:156-158`:
`if spec.ResponseReserveFraction != nil { next.Context.ResponseReserveFraction = *spec.ResponseReserveFraction }`
— written onto the `next` copy, committed atomically with the rest. Update the struct
doc at `:54-62`, which names `MaxOutputTokens` "the stated exception to per-model": the
reserve share is now the second exception, for the same reason (the share has no engine
setter of its own; a live edit's only door is the atomic rebind commit). Record the
change under `[Unreleased]` in `CHANGELOG.md`. (The `ISSUES.md` bullet is removed by
item 5, which completes the finding.)

**Files:** `internal/agent/rebind.go`, `internal/agent/rebind_test.go`, `CHANGELOG.md`

**Tests:** `TestRebindCarriesTheResponseReserveShare` in
`internal/agent/rebind_test.go`, mirroring `TestRebindCarriesTheReplyCeiling` (`:431`),
three states asserted through `a.budget().ResponseReserve` the way
`internal/agent/switchupstream_test.go:324,335` does:
- stated (e.g. `0.35` on a known window → the reserve is `int(0.35 * window)`),
- silent (nil → the previous share still governs),
- stated zero (→ the default `0.20` share governs).

**Acceptance:** `go build ./internal/agent/ && go test ./internal/agent/ -run TestRebind -v`

**Commit:** `feat(agent): RebindSpec carries the response-reserve share`

## 4. A bound-entry `response-reserve:` edit rides the beat rebind (TUI half)

Depends on item 3.

**What:** In `cmd/apogee/wire_settings.go`:
- `liveSettings.setServers` (`:342-360`) currently compares exactly two fields to decide
  whether an edit `moved` the bound entry — the resolved context window and the entry's
  reply cap (`:345`, `:359`) — and deliberately excludes the reserve (comment at
  `:350-354`, whose stated premise, "a rebind carries no share", item 3 deletes).
  Snapshot the entry reserve beside the other two (`reserve := s.entryReserve` before the
  re-derive) and add the third comparison to the return: `|| s.entryReserve != reserve`.
  Rewrite the `:350-354` comment to say the reserve now rides, citing the `RebindSpec`
  field.
- `rebindSpecFor` (`:1023-1067`) states the resolved reserve on the spec:
  `liveSettings.rebindInputs` (`:439`) already returns the resolved share (`:455`, the
  same value `cmd/apogee/schedule.go:109` consumes), so set
  `ResponseReserveFraction: &reserve` beside the `MaxOutputTokens: &outputCap` fill at
  `:1064`. The resolver always has something to say, so on this path the field is always
  stated — a drop arrives as a stated 0, exactly the ceiling's contract.

Record the change under `[Unreleased]` in `CHANGELOG.md`. (The `ISSUES.md` bullet is
removed by item 5.)

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_test.go`, `CHANGELOG.md`

**Tests:** In `cmd/apogee/wire_test.go`:
- A reserve twin of `TestApplySettingServersRidesTheRebindForTheBoundEntrysReplyCap`
  (`:4360`): editing only the bound entry's `response-reserve:` triggers the ride and the
  engine ends up allocating with the new share.
- A reserve twin of `TestApplySettingServersDoesNotRebindForACapEditThatMovesNothing`
  (`:4442`): a `servers:` commit that changes no bound-entry field (reserve included)
  still does not rebind.
- One test pinning the existing, intended refusal: a `/set` of the top-level
  `response-reserve` key (registry row `Editable: true`,
  `internal/config/registry.go:269-274`) falls to `cannotApply`
  (`wire_settings.go:718-719`) — the key is file-only for a running session, per the
  `pinnedReserve` field doc. This pins current behaviour; it changes nothing.

**Acceptance:** `go test ./cmd/apogee/ -run 'TestApplySettingServers|TestSetting' -v`

**Commit:** `feat(config): a bound-entry response-reserve edit rides the beat rebind`

## 5. Reserve-specific tests at the remaining arrival sites

Depends on items 3 and 4 (it removes the `ISSUES.md` bullet those items complete).

**What:** Test-only. Give every remaining `response-reserve` arrival site the
reserve-specific test its `context-window:` / `max-output-tokens:` analogue already has:
- **Scheduled Firing** (`cmd/apogee/schedule.go:109`): a twin of
  `TestScheduleFiringIsBoundedByTheEntryTheSessionMovedOnto`
  (`cmd/apogee/schedule_test.go:333`) — set `ResponseReserve` on the moved-onto entry
  (e.g. `0.35`) and on the launch options, drive `w.fire`, assert
  `stub.spec.Config.Context.ResponseReserveFraction` carries the moved-onto entry's
  share, not the launch server's. Same structure as the analogue: `runOnce` stub swap
  with `t.Cleanup`, no `t.Parallel()`, `live.followEntry(...)` to simulate the move.
- **Delegation target** (`cmd/apogee/delegation.go:450`): extend the existing pair
  `TestResolveDelegationTargetPinsOutrankTheBeat` (`cmd/apogee/delegation_test.go:81`)
  and `TestResolveDelegationTargetObservesWhatIsNotPinned` (`:124`) — or add twins in
  their style — asserting `ResponseReserveFraction` is carried as written from the entry
  (the site is deliberately rank-free: no top-level fallback; a raw copy).
- **Headless** (`cmd/apogee/headless.go:399-400`): extend
  `TestHeadlessBudgetsAgainstTheBoundEntrysPins` (`cmd/apogee/headless_test.go:482`) with
  a `wantReserve` column (or a twin) asserting
  `stub.spec.Config.Context.ResponseReserveFraction`.
- **Sub-agent spawn** (`internal/agent/subagent.go:258-260`, the `> 0` guard): a test in
  `internal/agent/subagent_test.go` pinning both arms — a target stating a share (e.g.
  `0.35`) overrides the parent's for the child, and a target stating none (0) leaves the
  parent's share in place.

Remove the third bullet of the `ISSUES.md` open-defects section (the `response-reserve:`
rebind one) — with items 3–4 landed and these tests in place, the whole finding is
closed — and, since that empties the "Run residuals — open (2026-08-15, …)" subsection,
remove the now-empty subsection heading too. Record the coverage under `[Unreleased]` in
`CHANGELOG.md`.

**Files:** `cmd/apogee/schedule_test.go`, `cmd/apogee/delegation_test.go`,
`cmd/apogee/headless_test.go`, `internal/agent/subagent_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** the item is its own tests (above).

**Acceptance:** `go test ./cmd/apogee/ -run 'TestScheduleFiring|TestResolveDelegationTarget|TestHeadlessBudgets' -v && go test ./internal/agent/ -run TestSubAgent -v`
(adjust the `internal/agent` run pattern to the actual new test name if it does not
match `TestSubAgent`).

**Commit:** `test(reserve): reserve-specific coverage at every arrival site`

## Suggested version bump

Patch-level micro-bump (e.g. `v0.14.9` → `v0.14.10`) once the plan lands: one behaviour
fix (NaN guard), one small feature completing a landed one (the reserve rides the beat
rebind like the reply cap already does), and test hardening. The bump is the owner's
call; no item in this plan changes `VERSION` or `CHANGELOG` release headings.
