# Open defects — the four entries deferred on 2026-08-24

**Goal:** close every entry under `ISSUES.md` § *Open defects* as of 2026-08-25: pin the daemon's
startup scratch sweep, bring `CONTEXT.md`'s persistence wording in line with ADR 0052 §5, make the
`internal/tools` file map lead a reader to the undo write funnels, and settle — by documented
exception, not by mirroring — the one editable key whose live edit an in-session Firing does not
follow.

**Date:** 2026-08-25
**Status:** unexecuted
**Sized for:** ~200k-context host

**Authoritative sources:**

- `ISSUES.md` § *Open defects* (the four entries, with their `file:line` evidence) — the scope.
- [ADR 0052](../adr/0052-diff-bodies-render-as-split-diffs-fed-by-tool-recorded-edit-regions.md)
  §5 — the codec mirrors the edit-region facts (item 2).
- [ADR 0051](../adr/0051-undo-is-a-per-exchange-in-memory-pre-image-journal.md) §3 and
  `internal/tools/path_safety.go` — the two write funnels (item 3).
- [ADR 0037](../adr/0037-every-settings-edit-applies-to-the-running-session.md), amendment of
  2026-08-24 — "a Firing sees exactly what the session sees; the mode is the one deliberate
  exception" (item 4).
- [ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) —
  `confine-to-workspace` is Auto's blast-radius flag (item 4).
- `cmd/apogee/headless_test.go:570` `TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes` — the
  headless twin the daemon test mirrors (item 1).

**Ratified design calls:**

- **`/confine off|on` is NOT mirrored onto `liveSettings`; an in-session Firing keeps the fence the
  session booted with** (owner, 2026-08-25, via AskUserQuestion). A blast radius loosened
  interactively is a per-session act taken by a human who is watching; the unattended run raised
  beside it keeps the configured fence. This becomes the second named exception to ADR 0037's
  "a Firing sees exactly what the session sees", beside the mode (ADR 0033 decision 3). Item 4
  therefore ships an ADR amendment, a code comment at the composer, a pinning test and a manual
  sentence — no holder field, no apply change.

**Standing requirements:**

- `skills: coding-standards`
- Every item closes its `ISSUES.md` entry the register's own way: the entry is REMOVED and the
  change is recorded under `CHANGELOG.md` `## [Unreleased]` (no "done" narration stays in
  `ISSUES.md`).
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- Go test names describe the behaviour claimed (house style — see the neighbouring tests named in
  each item); table tests only where the item says so.

**Out of scope:**

- Mirroring `confine-to-workspace` onto `liveSettings` or any change to `/confine`'s behaviour in
  the session itself (denied by the ratified call above).
- Anything under `ISSUES.md` § *Parked / deferred work* — those carry their own deferral records
  and, where picked up, need their own grill; see the handoff
  `docs/handoffs/2026-08-25 - 00 - backlog-after-open-defects.md`.
- The v0.17.0 release cut — a version act, the owner's; see the closing note.
- The repo's full-suite gate per item — `make check` runs once at the closeout.

---

## 1. Pin the daemon's startup scratch sweep — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the test is named `TestDaemonStartupSweepsStaleScratchDirs` so the item's own
acceptance regexp (`-run 'TestDaemon|TestHeadlessRunGetsItsOwnScratchDir'`) actually selects it.
NOTES (2026-08-25): pinning proof done and reverted — with `gcScratchDirs` commented out in
`cmd/apogee/daemonfire.go` the test FAILS ("a stale scratch dir survived the daemon's startup");
the call was restored, so no product code change lands (`git diff` on daemonfire.go is empty).

**What:** `newDaemonWiring` (`cmd/apogee/daemonfire.go:102`) runs
`gcScratchDirs(roots.scratch, time.Now())` once at startup because a host driven only by a daemon
never passes the TUI's boot sweep (`cmd/apogee/wire.go:84`). Nothing asserts it — the daemon suite
stays green with the call deleted. Add ONE test to `cmd/apogee/daemonfire_test.go`, the daemon twin
of `TestHeadlessRunGetsItsOwnScratchDirAndSweepsStaleOnes` (`cmd/apogee/headless_test.go:570`):
plant a stale scratch dir (named like `2026-01-01T00-00-00-stale`, mtime aged past
`scratchMaxAge` by an hour via `os.Chtimes`) under the config dir's scratch root BEFORE
`newDaemonWiring` runs, construct the wiring, and assert the dir is gone. Note
`newDaemonFireHarness` mints `opts.ConfigDir = t.TempDir()` itself, so the test either gives the
harness a pre-made config dir (the smaller change: let the harness keep a caller-set `ConfigDir`
when non-empty) or builds the wiring the way the harness does with the same three seams stubbed;
either is mechanical — pick the one that leaves `newDaemonFireHarness`'s existing callers untouched.
Use `resolveRoots(configDir, "")` to derive the scratch root exactly as the wiring does; never
hard-code the subdirectory name. A firing is not needed: the claim is about the START, so assert
right after construction. Then remove the `ISSUES.md` entry *The daemon's startup scratch sweep is
unpinned* and record the close under `CHANGELOG.md` `[Unreleased]` (one bullet, `### Fixed` or the
plan's existing sub-heading style — match neighbouring entries).

**Files:** `cmd/apogee/daemonfire_test.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** the new test in `cmd/apogee/daemonfire_test.go`. Prove it pins: temporarily comment out
the `gcScratchDirs` call, run the test, confirm it FAILS, restore the call (no product code changes
land).

**Acceptance:**

```
go build ./cmd/apogee/... && go test ./cmd/apogee/ -run 'TestDaemon|TestHeadlessRunGetsItsOwnScratchDir' -count=1
go vet ./cmd/apogee/
```

**Commit:** `test(cmd): pin the daemon's startup scratch sweep`

---

## 2. `CONTEXT.md`'s persistence wording follows ADR 0052 §5 — ✅ DONE (2026-08-25)

**What:** two entries in `CONTEXT.md` say the opposite of the code they govern. **Tool summary**
(`CONTEXT.md:998`) reads "A summary is **never persisted** and never sent to the model"; **Edit
regions** (`CONTEXT.md:1009`) reads "display data — never sent to the model, never in the session
record". ADR 0052 §5 ratifies the transcript codec MIRRORING the edit-region facts into the session
record (`internal/tui/transcriptcodec.go:158-173, 461-478`), and the 2026-08-24 architecture-review
plan's item 1 already removed the "never persisted" wording from the two Go comment sites
(`internal/domain/toolsummary.go` and the ADR). Rewrite both `CONTEXT.md` sentences to state the
actual rule: a summary is never SENT to the model; it is display data the view consumes, and the
edit-region facts are the one part of it the session codec mirrors into the record so a resumed
session renders the same split diffs (cite ADR 0052 §5 inline, the way the entry already cites ADR
0002). Keep the entries' voice and their `_Avoid_` lines; change no other entry. Read
`internal/domain/toolsummary.go`'s current doc comment first and keep the two in agreement — the Go
comment is the ground truth the domain language must not contradict. Then remove the `ISSUES.md`
entry *`CONTEXT.md`'s persistence wording still contradicts ADR 0052 §5* and record the close under
`CHANGELOG.md` `[Unreleased]`.

**Files:** `CONTEXT.md`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none (docs only). Verification is by reading: the two `CONTEXT.md` sentences, the
`toolsummary.go` doc comment and ADR 0052 §5 say the same thing.

**Acceptance:**

```
grep -n 'never persisted' CONTEXT.md            # expect no hit in the Tool summary entry
grep -n 'never in the session record' CONTEXT.md # expect no hit in the Edit regions entry
grep -n 'ADR 0052' CONTEXT.md                   # expect the two rewritten entries to cite it
```

**Commit:** `docs(context): tool summary and edit regions state the codec's mirroring per ADR 0052 §5`

---

## 3. The `internal/tools` file map names the two undo write funnels

**What:** the package spine in `internal/tools/doc.go` (`# The package spine, one line each`,
`doc.go:233-240`) describes `path_safety.go` as the thin alias layer onto `internal/security` plus
the approved escape's tools-side read (ADR 0049). Since the 2026-08-24 deepening plan the same file
also holds BOTH undo write funnels — `safeWriteFile` (`path_safety.go:70`, the content verbs) and
`journaledMutation` (`path_safety.go:129`, the multi-path verbs) — which are the only callers of
`capturePreImage` / `commit` / `commitReadBack` (ADR 0051 §3). Extend the `path_safety.go` line
(keep it ONE entry in the one-line-per-file spine; a second sentence on the same entry is fine) so
it names both funnels, says which verb family goes through each, and cites ADR 0051 §3 as the
reason they are the only writers of the pre-image. Also re-check the spine's opening count ("Seven
files register no tool") still matches the files it lists — fix the number only if it is wrong.
Then remove the `ISSUES.md` entry *`internal/tools/doc.go`'s `path_safety.go` file-map entry names
neither write funnel* and record the close under `CHANGELOG.md` `[Unreleased]`.

**Files:** `internal/tools/doc.go`, `ISSUES.md`, `CHANGELOG.md`

**Tests:** none (comment only); `go vet` catches a malformed doc comment.

**Acceptance:**

```
grep -n 'safeWriteFile\|journaledMutation' internal/tools/doc.go   # both named in the spine
go build ./internal/tools/ && go vet ./internal/tools/
```

**Commit:** `docs(tools): the file map names both undo write funnels in path_safety.go`

---

## 4. `confine-to-workspace` is a named exception to the Firing's live-settings rule

**What:** ratified above — the boot value stays. `/confine off|on` moves Auto's blast radius on the
live engine (`internal/tui/confine.go:44` → `cmd/apogee/wire_engine.go:286`), nothing mirrors it
onto `liveSettings`, so `options()` (`cmd/apogee/wire_settings.go:542`) projects `s.boot`'s
`ConfineToWorkspace` and the in-session Firing (`cmd/apogee/schedule.go:113` →
`cmd/apogee/wire_firing.go:195`) is fenced by the boot value. That is now the intended behaviour;
make it a documented, pinned exception rather than an accident. Four edits, no behaviour change:

1. **ADR 0037** — in the *Amendment (2026-08-24)* section, the closing paragraph says "The mode is
   the one deliberate exception". Amend it (a short dated note, 2026-08-25, appended to that
   paragraph or as a new one directly after it) to name `confine-to-workspace` as the second: a
   `/confine off|on` in the session is a per-session, human-watched act on Auto's blast radius
   (ADR 0012); an unattended Firing keeps the fence the session was configured with. State that
   `liveSettings` deliberately carries no mirror for it and that `/confine off --save` — which
   writes the host entry — is how a Firing raised from a LATER session runs unfenced. Do not
   touch ADR 0012 or ADR 0033.
2. **`cmd/apogee/wire_firing.go:193-195`** — the comment above `Confiner:` /
   `ConfineToWorkspace: in.opts.ConfineToWorkspace` says "posture as the session's". Rewrite it to
   say the posture is the session's CONFIGURED one — the boot value, not a `/confine` toggled since
   — and cite ADR 0037's 2026-08-25 note. One comment; the code line stays.
3. **Pinning test** in `cmd/apogee/schedule_test.go`, beside
   `TestScheduleFiringFollowsLiveSettingsEdits` (`schedule_test.go:525`): boot the session with
   `ConfineToWorkspace: true` through the same runner seam that test uses, flip the live engine
   with `SetConfineToWorkspace(false)` (the seam `/confine off` calls — see
   `cmd/apogee/confinement_e2e_test.go:117` for the call shape), fire the in-session Schedule, and
   assert the composed `Config.ConfineToWorkspace` is still `true`. Name it for the claim, e.g.
   `TestScheduleFiringKeepsTheBootFenceAfterConfineOff`. Its doc comment names the ratified call
   and ADR 0037's note, so a future reader does not re-file this as a bug.
4. **Manual** — `docs/manual/configuration.md:562-566` describes `/confine off` as running Auto
   unconfined "for this session". Add one sentence there: a Schedule fired from inside that session
   still runs with the fence the session started with; `--save` is the route for later sessions'
   Firings.

Then remove the `ISSUES.md` entry *`/confine off|on` is not mirrored onto `liveSettings`, so a
Firing fences by the boot value* and record the close under `CHANGELOG.md` `[Unreleased]` — as a
ratified exception (say so), not a fix.

**Files:** `docs/adr/0037-every-settings-edit-applies-to-the-running-session.md`,
`cmd/apogee/wire_firing.go`, `cmd/apogee/schedule_test.go`, `docs/manual/configuration.md`,
`ISSUES.md`, `CHANGELOG.md`

**Tests:** the new pinning test. The existing `TestScheduleFiringFollowsLiveSettingsEdits` and the
`confinement_e2e_test.go` suite must stay green untouched.

**Acceptance:**

```
go build ./cmd/apogee/... && go test ./cmd/apogee/ -run 'TestScheduleFiring|Confine' -count=1
go vet ./cmd/apogee/
grep -n 'confine-to-workspace' docs/adr/0037-every-settings-edit-applies-to-the-running-session.md   # the amendment names it
```

**Commit:** `docs(adr): confine-to-workspace is a named exception to the Firing's live-settings rule, pinned by test`

---

## Suggested version bump

None required by these items (one test, three doc/comment changes, one ratified exception). The
`[Unreleased]` section already carries ~26 entries since `v0.16.3` while `VERSION` reads `0.16.8`;
cutting **v0.17.0** after this plan is a release act the owner decides — not part of this plan.
