# Daemon residuals — fix the five deferred findings of the daemon plan

**Goal:** resolve all five open defects the 2026-08-22 daemon run recorded in `ISSUES.md`
(`docs/plans/archived/2026-08-22 - 02 - daemon-plan.md` residuals): the latent `stop`
ordering, the untruthful reload summary, the untested shutdown-grace expiry, the missing
event-kind drift guard, the non-atomic supervisor-unit write, and the stale parked-entry
trigger.

**Date:** 2026-08-22 · **Status:** ready · **sized for:** ~200k-context host

**Authoritative sources:** the five "residual of … daemon-plan" entries under
`ISSUES.md` → *Open defects* as of commit `69f4697d` (each item names its entry);
ADR 0033 (scheduler library), ADR 0034 (single instance, all-or-nothing reload);
`docs/plans/archived/2026-08-22 - 02 - daemon-plan.md` for the shipped design.

**Ratified design calls (owner, 2026-08-22, via AskUserQuestion at write time):**

- **Truthful reload summary:** `daemon.Apply` prunes names whose `Add` failed from the
  `Reload` it returns, so `reloadSummary` names only what actually reached the clock; the
  failure line follows it. Chosen over merely swapping the two log lines.
- **Real kind drift guard:** `internal/schedule` exports its own event-kind set
  (`EventKinds`, declared next to the kind consts); the daemon's test iterates it and
  asserts `notify` logs a line for every kind. Chosen over dropping the doc-comment claim.
- **Re-park the `schedule`-tool entry on demand:** the parked *model-facing `schedule`
  tool* entry stays parked, its trigger rewritten to "until a real need for a model-facing
  schedule tool appears" — the daemon existing is necessary, not sufficient. Chosen over
  daemon-maturity re-parking and over unparking.

**Standing requirements:**

- skills: coding-standards
- Each item that resolves an `ISSUES.md` entry REMOVES that entry (or its bullet) in the
  same item and adds one `CHANGELOG.md` bullet under `[Unreleased]` → *Fixed* — the
  changelog is the sole closed trail; never leave done narration in `ISSUES.md`.
- `ISSUES.md` may still hold the owner's uncommitted pop-up-scrolling entry (the first
  `- [ ]` bullet under *Open defects*, hand-typed 2026-08-22). If the tree is dirty with
  it at execution start, Phase 0's dirty-tree consult decides; whatever the resolution,
  no item of this plan edits or removes that bullet.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**

- The owner-run live checks (Windows/darwin lock behaviour, live daemon run) recorded in
  the CHANGELOG's owner-run verification note.
- The two `97c26064` config-defaults findings (template header/CHANGELOG gap; built-in
  defaults vs template divergence) — separate register entries, not daemon residuals.
- The TUI pop-up scrolling defect (owner's own draft entry).
- Implementing the model-facing `schedule` tool itself (item 6 only re-files its entry).
- Any `VERSION`/release-heading change.

## 1. `stop` keeps a schedule's id until the scheduler confirms it is off the clock — ✅ DONE (2026-08-22)

NOTES (2026-08-22): no `### Fixed` subsection exists under `[Unreleased]` yet — place it after the `### Changed` block (keep-a-changelog order) and put the bullet there.
NOTES (2026-08-22): the plan's second test case (Stop returning ErrNotFound still clears the map entry, no error) was already covered by TestApplyTreatsAForgottenScheduleAsStopped, which asserts exactly that and still passes; the failure case extends TestApplyReportsAStopFailure rather than adding a duplicate test.
NOTES (2026-08-22): also appended "and one whose Stop failed keeps its id" to Apply's doc comment's id-map sentence — the map contract it documents changed with the reorder; item 2 owns Apply's Reload-contract doc update, untouched here.

**What:** In `internal/daemon/diff.go`, `stop` currently deletes the name→id entry
(`:149`) BEFORE calling `Scheduler.Stop` (`:150`), so a `Stop` failing with anything
other than `schedule.ErrNotFound` would strand a schedule: still on the clock, but gone
from the map that `adoptedEntries` and every later reload consult. Latent today (the
library returns only `ErrNotFound`), but the ordering is the defect. Reorder: call
`scheduler.Stop(id)` first; delete `ids[name]` only when `Stop` returns nil or
`ErrNotFound`; on any other error return it with the entry still in the map, so the
schedule remains stoppable. Update `stop`'s doc comment to state the invariant: the map
drops a name only once the scheduler no longer runs it. Remove the resolved `ISSUES.md`
entry (*The daemon's `stop` forgets a schedule's id…*) and add the CHANGELOG bullet.

**Files:** `internal/daemon/diff.go`, `internal/daemon/diff_test.go`, `ISSUES.md`,
`CHANGELOG.md`

**Tests:** extend `diff_test.go` with a fake `Scheduler` whose `Stop` returns a
non-`ErrNotFound` error: `Apply` must return the joined error AND leave the name in the
id map (assert the map still holds the id). A second case: `Stop` returning
`ErrNotFound` still removes the map entry and yields no error (today's behaviour,
preserved).

**Acceptance:** `go build ./... && go test ./internal/daemon/`

**Commit:** `fix(daemon): drop a schedule's id only after the scheduler confirms the stop`

## 2. `Apply` returns what actually reached the clock, making the reload summary truthful — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the CHANGELOG bullet goes under the existing `### Fixed` subsection of `[Unreleased]`.
NOTES (2026-08-22): `adoptSchedules`/`reloadSchedules` now accept the `daemon.Scheduler` interface (which `daemon.Apply` already takes and `*schedule.Scheduler` satisfies) instead of the concrete type — the file's own validation enforces every constraint the scheduler's does, so no real save can make one `Add` fail selectively; the plan's reload-log test needs a wrapping fake at that seam.
NOTES (2026-08-22): pruning extended beyond the item's literal `Replaced`/`Added` wording in two composing ways the ratified contract ("the returned `Reload` describes what actually happened") implies: a replaced entry whose `Stop` failed also has its add-half SKIPPED (adding over the still-running old id would re-create the strand item 1 fixed), and a removed entry whose `Stop` failed is pruned from `Removed` (it is still on the clock). Both covered by tests.
NOTES (2026-08-22): dropped the now-false "— not the returned Reload —" clause from `adoptSchedules`'s doc comment; the id-map rationale it carried is unchanged.

**What:** Per the ratified call: in `internal/daemon/diff.go`, when an `add` fails during
`Apply` (the loops over `reload.Replaced` and `reload.Added`, `:130-140`), prune that
name from the returned `Reload`'s `Replaced`/`Added` slice — the returned `Reload`
describes what actually happened, not what the diff planned. (A `Replaced` entry whose
add-half failed is neither running nor replaced; the joined error already names it.)
Update `Apply`'s and `Reload`'s doc comments to state this contract. In
`cmd/apogee/daemon.go`, `reloadSchedules` keeps its line order — summary first, then
`"some of the edit did not take: %v"` — because the summary is now the true line; update
the comment above the summary log call (`:322-323`) accordingly. Depends on item 1
(same file and functions — its `Stop`-failure ordering must land first so the pruning
composes with it; a name whose `stop` failed stays in the map and is likewise not
reported replaced).

**Files:** `internal/daemon/diff.go`, `internal/daemon/diff_test.go`,
`cmd/apogee/daemon.go`, `cmd/apogee/daemon_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** `diff_test.go`: an `Apply` where one added and one replaced entry fail their
`Add` returns a `Reload` without those names (and with the survivors intact) plus the
joined error. `daemon_test.go`: a reload log assertion — with one entry's `Add` failing,
the summary line does NOT name it and the following line does. Remove the resolved
`ISSUES.md` entry (*The reload's accepted-swap summary is logged before the failures…*)
and add the CHANGELOG bullet.

**Acceptance:** `go build ./... && go test ./internal/daemon/ ./cmd/apogee/`

**Depends on item 1.**

**Commit:** `fix(daemon): reload summary reports only the schedules that actually took`

## 3. Test the shutdown grace expiring — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the CHANGELOG bullet goes under the existing `### Fixed` subsection of `[Unreleased]`, after the reload-summary bullet.
NOTES (2026-08-22): the entry's intro line still says "two gaps in the same file's coverage" — the item's text scopes this item to deleting the FIRST bullet only; item 4 deletes the entry's remainder and with it that intro.

**What:** The `<-grace.C` branch of `daemonShutdown` (`cmd/apogee/daemon.go:405-406`) is
the one shutdown path nothing exercises. Add
`TestDaemonGraceExpiryCancelsTheFiringInFlight` to `cmd/apogee/daemon_test.go`, modelled
on `TestDaemonSecondSignalCancelsTheFiringInFlight` (`:462`): a schedules file with a
short `shutdown-grace` (tens of milliseconds), a firing that blocks until its context is
cancelled, one stop signal, then assert the log contains the
`"grace expired — cancelling the firing in flight"` line and the daemon stops. No
production code changes. In `ISSUES.md`, delete only the FIRST bullet (the grace-expiry
gap) of the entry *The shutdown grace EXPIRY has no test, and the notify switch's drift
guard does not exist* — item 4 removes the remainder — and add the CHANGELOG bullet.

**Files:** `cmd/apogee/daemon_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new test itself; it must fail if the grace branch's log line is removed.

**Acceptance:** `go test ./cmd/apogee/ -run TestDaemon`

**Commit:** `test(daemon): cover the shutdown-grace expiry branch`

## 4. The notify switch's drift guard exists and the doc claim becomes true — ✅ DONE (2026-08-22)

NOTES (2026-08-22): the CHANGELOG bullet goes under the existing `### Fixed` subsection of `[Unreleased]`, after the shutdown-grace-expiry bullet.
NOTES (2026-08-22): reworded the `notify` doc comment's claim to name the drift-guard test and `schedule.EventKinds` — the item allows a wording adjustment where the test's name makes it clearer; the claim itself is unchanged.

**What:** Per the ratified call: in `internal/schedule/schedule.go`, export
`EventKinds` — a slice listing every `EventKind`, declared IMMEDIATELY next to the kind
consts (`:119-132`) with a comment binding the two ("a new kind is added to both, on
this screen"). In `cmd/apogee/daemon_test.go`, add a drift-guard test that iterates
`schedule.EventKinds`, feeds `notify` one event of each kind (populating the fields each
kind's line renders), and asserts every kind produces a log line — a kind falling
through the switch produces none and fails the test. Keep
`TestDaemonNotifyLinesArePinned` (`:845`) as the exact-wording table; the new test is
the completeness guard. The doc comment at `cmd/apogee/daemon.go:598-599` stays, now
true — adjust its wording only if the test's name makes it clearer. In `ISSUES.md`,
delete the now-empty remainder of the entry item 3 started (*The shutdown grace EXPIRY
has no test…*) and add the CHANGELOG bullet.

**Files:** `internal/schedule/schedule.go`, `internal/schedule/schedule_test.go`,
`cmd/apogee/daemon.go`, `cmd/apogee/daemon_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the daemon-side drift-guard test above; library-side, a small test in
`schedule_test.go` asserting `EventKinds` contains every kind the scheduler can emit
(the lifecycle kinds an existing full-lifecycle test already enumerates) and no
duplicates.

**Acceptance:** `go build ./... && go test ./internal/schedule/ ./cmd/apogee/`

**Depends on item 3** (shares `cmd/apogee/daemon_test.go` and finishes the same
`ISSUES.md` entry).

**Commit:** `fix(daemon): derive the notify drift guard from the library's own kind set`

## 5. `daemon install` writes the supervisor unit atomically

**What:** `writeDaemonUnit` writes the rendered unit with a plain `os.WriteFile`
(`cmd/apogee/daemoninstall.go:326`) — a crash or full disk mid-write leaves a truncated
unit where a valid one was, and a supervisor reads that file. Adopt the repo's
temp-file-plus-rename idiom (as in `internal/library/store.go:323`,
`internal/session/store.go:413`, rationale at `internal/platform/winlabel/journal.go:192`):
write the exact bytes `daemonUnitBytes` produced (the Windows unit stays UTF-16LE+BOM)
to a temp file beside the target with mode `0o600`, then rename over the target. Keep
the existing unchanged-content short-circuit and the `existed`/`changed` return
contract untouched.

**Files:** `cmd/apogee/daemoninstall.go`, `cmd/apogee/daemoninstall_test.go`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** extend `daemoninstall_test.go`: after a write, no stray temp file remains in
the unit's directory; the written bytes still match the golden (the existing golden
tests must keep passing unmodified — byte identity is the acceptance the plan that
shipped them established). Remove the resolved `ISSUES.md` entry (*`apogee daemon
install` writes the supervisor unit without the repo's temp-file-plus-rename idiom*)
and add the CHANGELOG bullet.

**Acceptance:** `go test ./cmd/apogee/ -run TestDaemonInstall` (fall back to
`go test ./cmd/apogee/` if the install tests use another name prefix)

**Commit:** `fix(daemon): install writes the supervisor unit via temp file and rename`

## 6. Re-park the model-facing `schedule`-tool entry on demand

**What:** Per the ratified call: in `ISSUES.md`, the parked entry *A model-facing
`schedule` tool — daemon-era, not v1* is parked on "the daemon itself is unbuilt"
(`ISSUES.md:696-697`) — now false. Rewrite ONLY the trigger sentence: the entry stays
in *Parked / deferred work* with all its recorded design intact, re-parked "until a
real need for a model-facing schedule tool appears — `apogee daemon` shipping
(2026-08-22) made it possible, not needed" (wording to that effect; date the re-park
and attribute it to the owner call of 2026-08-22). Then remove the now-resolved *Open
defects* entry that flagged it (*The parked `schedule`-tool entry's own trigger has now
fired*) and add a CHANGELOG bullet under `[Unreleased]` → *Changed* or *Fixed* noting
the re-park. Docs-only; touch nothing else in either file.

**Files:** `ISSUES.md`, `CHANGELOG.md`

**Tests:** none (docs-only).

**Acceptance:** `grep -c "daemon itself is unbuilt" ISSUES.md` prints `0`;
`grep -c "The parked \`schedule\`-tool entry" ISSUES.md` prints `0`; the parked entry's
heading still exists under *Parked / deferred work*
(`grep -c "A model-facing \`schedule\` tool" ISSUES.md` ≥ 1).

**Commit:** `docs(issues): re-park the schedule-tool entry on demand, not daemon existence`

## Suggested version bump

None on its own — these are fixes to unreleased work. Fold them into the
**0.16.0 → 0.17.0 minor bump** already suggested for the daemon feature itself; the
bump remains the owner's call and no item of this plan touches `VERSION` or the
CHANGELOG's release headings.
