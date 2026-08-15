# Quiet qualifier, single clock — plan

**Goal:** The stall guard's status-line surface currently renders two clocks —
`thinking · 2m 59s · quiet 2m 59s` — which read as redundant in the common stall case
(no events at all since launch, so both clocks are equal). Reshape it to a single clock
with `quiet` as a bare qualifier: `thinking · quiet · 2m 59s`. Also pin, with an explicit
regression test, the already-true invariant that the quiet qualifier never shows while
the model is actually emitting thinking tokens (a streaming reasoning channel restamps
the silence clock on every event).

**Date:** 2026-08-15
**Status:** not started
**Sized for:** ~200k-context host
**Authoritative sources:** `internal/tui/model.go` (`statusLeft`, `runningPhrase`,
`quietSuffix`), `internal/tui/activity.go` (`activity.quiet`, `noteEngineHeard`),
`layout.md` §status line (the `quiet` suffix paragraph, currently showing
`thinking · 21m 03s · quiet 3m 10s`), `CHANGELOG.md` `[Unreleased]` stall-guard entries.
**Skills:** coding-standards

**Ratified design calls** (owner via AskUserQuestion, 2026-08-15):

- **Single clock = the ACTIVITY clock.** The one duration shown is `activity.elapsed`
  (how long the phrase's activity has run), counting continuously; `quiet` is a bare
  qualifier inserted before it. The clock never jumps backward when the qualifier
  appears or clears. The silence *length* is no longer displayed anywhere.
- **Amber tint covers `· quiet` only.** The inserted qualifier fragment wears the
  scheme's `statusWarning` amber; the clock stays in the plain `statusBar` style — it is
  the activity's clock, not the guard's.

**Out of scope:**

- No change to *when* the guard fires: `activity.quiet`'s three gates (thinking or
  responding only; `ui.stall-after` as threshold; 0 disables) and the
  restamp-on-every-Event rule stay exactly as they are.
- No change to `ui.stall-after` config, its default, or its docs beyond the example
  string.
- No new config knob for the qualifier's form or tint.

## 1. Render the stall guard as a `quiet` qualifier before a single activity clock — ✅ DONE (2026-08-15)

NOTES (2026-08-15): the CHANGELOG text above REPLACES the existing `[Unreleased]` bullet that opens
`- **The status line stops claiming "thinking" when nothing is coming.**` (~line 33) IN PLACE — it
is not an additional entry; the old two-clock form never shipped, so nothing in the changelog may
describe it. The implementer never writes CHANGELOG.md itself (run protocol), which is why this
arrives as replacement text rather than as an edit.
NOTES (2026-08-15): one more never-shipped mention of the old form sits in the sibling `warning`-role
bullet (~line 32): "it is the tint the stall guard's quiet-time suffix will wear" should read "the
stall guard's quiet qualifier will wear". Same in-place amendment, verifier's to apply.
NOTES (2026-08-15): `runningPhrase` now returns the STYLED slot (it has to: the amber covers the
inserted ` · quiet` alone, and lipgloss cannot tint a fragment inside an outer Render), taking
`quiet bool` and composing phrase / qualifier / clock as three styled runs. `statusLeft` therefore
composes the running slot BOTH ways and the width test picks one — that is what drops the qualifier
whole instead of truncating it, at the same priority as before (queued count still last).
`quietSuffix` is gone; the fragment is the `quietQualifier` const.
NOTES (2026-08-15): files beyond the plan's list, all compelled by the reshape. `internal/tui/doc.go`
— its stall-guard paragraph linked the deleted `[Model.quietSuffix]` and described the two-clock
form. `internal/tui/activity_test.go` — its three `runningPhrase` assertions took the new signature
and now strip styling (`strip(...)`), beside the quiet-table change the plan named.
`internal/config/defaults/config.yaml`, `internal/config/config.go`, `internal/tui/settings.go`,
`internal/config/config_test.go` — the seeded config template and the `ui.stall-after` refusals
carried the old `· quiet 3m 10s` example and the word "suffix"; the example strings are the
carve-out the plan's Out-of-scope names, and the refusals are user-facing help text for behaviour
that changed. No behaviour, default or key was touched.
NOTES (2026-08-15): the `silentFor` test helper now backdates `m.act.since` as well as
`m.lastEvent` — with one clock on the row the assertion `thinking · quiet · 3m 10s` needs the
activity clock to read the same span, which is exactly the incident's shape (nothing arrived since
the phrase went up).

**What:** Reshape the running status line's stall surface in `internal/tui/model.go`
from a duration-carrying suffix appended *after* the clock to a bare qualifier inserted
*between* the phrase and the clock:

- Rendered form while quiet is owed: `thinking · quiet · 2m 59s` — activity phrase,
  then ` · quiet`, then ` · ` and the `activity.elapsed` clock (`formatElapsed`). The
  ` · quiet` fragment (separator included) is rendered through `th.statusWarning`; the
  phrase and clock keep their current styles. When quiet is not owed the line is
  unchanged: `thinking · 2m 59s`.
- Restructure the seam accordingly: `runningPhrase` (or a successor composition in
  `statusLeft`) now owns placing the qualifier between phrase and clock;
  `quietSuffix` as a trailing-suffix producer disappears or shrinks to the qualifier
  fragment. Keep `now` passed in so composition stays testable off the wall clock.
- **Width give-way rule is preserved with the same priority:** on a row too narrow for
  both, the qualifier is dropped WHOLE (never truncated into `· quie…`), one rung
  before the phrase itself gives way — the row falls back to `thinking · 2m 59s`
  exactly as if quiet were not owed. The queued-count remains the last thing given up
  (`statusLeft`'s existing order).
- Simplify `activity.quiet` in `internal/tui/activity.go` to return only the owed
  `bool` — the silence duration is no longer rendered by anyone, and a dead return
  value invites a caller to re-grow the second clock. Update its doc comment (it
  currently promises "how long"), the `noteEngineHeard` comment that names
  "statusLeft's quiet suffix", and the `Model.lastEvent` field comment in `model.go`
  (~line 243) to describe the qualifier form.
- Update the existing tests in `internal/tui/model_test.go` to the new form:
  `TestStatusLineQuietSuffix` asserts `thinking · quiet · 3m 10s` (single clock — the
  literal `quiet 3m 10s` and the second duration must be gone), the tint sub-test
  asserts the `statusWarning`-rendered fragment is exactly the ` · quiet` qualifier
  (clock NOT inside the tinted region), and `TestStatusLineQuietSuffixGivesWayFirst`
  asserts the narrow row drops the qualifier whole and keeps `thinking` plus its
  clock. Update `internal/tui/activity_test.go`'s quiet-clock table for the
  bool-only signature (the threshold/gate cases all stay; only the duration
  assertions go).
- Docs owned by this item: rewrite the `layout.md` stall-guard paragraph (~line 1129)
  to the new example `thinking · 21m 03s` → `thinking · quiet · 21m 03s` wording, and
  amend the existing `CHANGELOG.md` `[Unreleased]` stall-guard entry (~line 35) IN
  PLACE to show the new form — the old format never shipped in a release, so the
  changelog must not describe it; do not add a second entry narrating the reshape.

**Files:** `internal/tui/model.go`, `internal/tui/activity.go`,
`internal/tui/model_test.go`, `internal/tui/activity_test.go`, `layout.md`,
`CHANGELOG.md`

**Tests:** the updated `TestStatusLineQuietSuffix` sub-tests (form, tint boundary,
event-clears-it, never-on-tools/stopping/human-waits, fresh-exchange, guard-off) and
`TestStatusLineQuietSuffixGivesWayFirst`; the updated `activity.quiet` table test.

**Acceptance:**

```
go build ./...
go test ./internal/tui/
grep -n "quiet · " layout.md          # new example present
! grep -n "· quiet [0-9]" layout.md   # old duration-suffix example gone
```

**Commit:** `feat(tui): render stall guard as quiet qualifier on a single activity clock`

## 2. Pin the invariant: streaming thinking tokens keep the quiet qualifier off

Depends on item 1.

**What:** The invariant already holds structurally — every `eventMsg` (any Event, any
depth, including `domain.ReasoningEvent` thinking chunks) calls `noteEngineHeard`
*unconditionally, before the fold* (`model.go` `case eventMsg`), so a streaming
reasoning channel restamps the silence clock faster than `ui.stall-after` can elapse —
and `TestStatusLineQuietSuffix`'s "an arriving event takes it straight back off"
sub-test already proves a single `ReasoningEvent` clears an owed qualifier. What is NOT
yet pinned is the *sustained-stream* statement: that a model actively emitting thinking
tokens never shows `quiet` at any point across a long turn. Add one sub-test to
`TestStatusLineQuietSuffix` in `internal/tui/model_test.go`:

- Simulate a long thinking turn as repeated rounds: backdate `lastEvent` by an interval
  just BELOW `ui.stall-after` (via the existing `silentFor` helper), assert the status
  line carries no `quiet`, then step a `domain.ReasoningEvent` through `eventMsg`
  (restamping the clock) — several rounds, so the total simulated elapsed is many
  multiples of the threshold while no single gap crosses it. After each round assert
  the line still reads `thinking` with no `quiet` anywhere.
- Name and comment the sub-test as the invariant's pin: quiet reports the *absence* of
  events, so a streaming reasoning channel — thinking tokens actually arriving — must
  never surface it; only a genuine gap longer than `ui.stall-after` may.

No production code changes in this item.

**Files:** `internal/tui/model_test.go`

**Tests:** the new sub-test itself.

**Acceptance:**

```
go test ./internal/tui/ -run TestStatusLineQuietSuffix -v
```

**Commit:** `test(tui): pin that streaming thinking tokens never surface the quiet qualifier`

---

**Suggested version bump:** none needed — both the stall guard and this reshape sit
under `[Unreleased]`; the next release's bump covers them together. The owner decides.
