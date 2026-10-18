# Plan: watch mode fixes

**Goal:** Fix the two defects left by the live-session-watch plan: watching a running session no longer defers this session's `/schedule` Firings, and a watch restarts at byte 0 when the followed journal is truncated and regrown past the follower's offset within one poll.
**Date:** 2026-10-18
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** daf9f806

**Sources:**
- `capcom show watch-defers-firings`; `capcom show watch-journal-reopen-missed`
- `docs/plans/archived/2026-10-17 - 03 - live-session-watch-plan.md` (items 9, 9a)
- ADR 0011, ADR 0033, ADR 0103 (`capcom know show "ADR NNNN"`)
- Glossary: Watch mode, Event journal, Firing

**Ratified design calls (writer, 2026-10-18 — no user-visible choice):**
- **Reopen detection:** fingerprint the journal's existing first line (`run_started`, unique per open through its `time`); no generation id, no wire or golden change.
- **Own activity:** only `Model.quiescent` reads the parked state; every other reader of `m.state` keeps the mirrored watch state.

**Standing requirements:**
- skills: coding-standards
- Each `go test` names one package and a `-run` pattern, prefixed `GOMEMLIMIT=2GiB` (AGENTS.md; never a multi-package race run).

**Out of scope:** a watched owner contending with a Firing on a shared single-slot server (ADR 0033 gates on this session only); `windows-journal-remove-check`; every other open issue (owner-deferred, grill/ADR first, or apogee-sim work).

**Regression check (2026-10-18, daf9f806):**
- 2: guard folded (replaced-journal test variant; only a short read or byte mismatch restarts at 0)

## 1. Watch mode reports this session's own activity to the schedule Gate — ✅ DONE (2026-10-18)

**What:** Fix (defect from the live-session-watch plan): while watch mode follows a running session, `reportActivity` publishes the watched owner's busy state, so this session's due Firings are deferred until esc.
**Goal:** While watching, `Options.ReportActivity` reflects this session's own activity; a watched run going busy reports nothing, and the watch view still shows the run as running.
**Approach (assumed at the header base):** in `Model.quiescent` (`internal/tui/schedule.go`, value receiver), when `m.watch.isActive`, set the local copy's `m.state = m.watch.parked.state` before `m.engineHolds().quiescent()`, with a comment naming why. Note it in `engineHolds.quiescent`'s doc (`internal/tui/engineholds.go`). Do not change `engineHoldSources` or `Model.busy`: autocomplete, esc-stop, `quitSession`, `watchRunning`, `foldWatchBatch`, `foldForOwner`, `commandRunnable`, `canLaunchBg` keep reading the mirrored state.
**Files:** internal/tui/schedule.go, internal/tui/engineholds.go, internal/tui/watch_test.go
**Read first:** internal/tui/schedule.go — Model.quiescent, reportActivity; internal/tui/engineholds.go — engineHolds.quiescent, engineHoldSources; internal/tui/watch.go — watchState.parked;
internal/tui/schedule_test.go — activityModel; internal/tui/watch_test.go — newWatchFixture, pollWatch (enterWatchOn builds its own testOpts model, so call m.enterWatch on activityModel's model)
**Tests:** `TestWatchReportsTheWatchersOwnActivity` in `watch_test.go`, using `activityModel` (`schedule_test.go`) with `newWatchFixture`, `enterWatch`, `f.prompt`, `pollWatch`: after the watched prompt, `m.state == stateRunning` and no activity report was recorded; after esc, still none.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWatchReportsTheWatchersOwnActivity|TestWatch|Activity' ./internal/tui/`
**Closes:** watch-defers-firings
**Commit:** `fix(tui): report the watcher's own activity to the schedule gate`

## 2. Watch restarts on a journal truncated and regrown within one poll — ✅ DONE (2026-10-18)

NOTES (2026-10-18): offset, journal and head are bundled as an embedded `journalCursor` in both `watchState` and `watchBatchMsg` (promoted fields keep `m.watch.offset` / `msg.journal` working); `readJournal` takes the cursor, so its parameter list shrank to four. The head check lives in a new helper, `isHeadChanged`.
NOTES (2026-10-18): `watchFixture` gained a `now` clock passed to the reopened journal, so the same-title variant differs from the original frame only by `time` every time it runs; `TestWatchFollowerPollsOnItsOwn`'s hand-built batch now carries the whole cursor, head included.
NOTES (2026-10-18): the new test checks that the first two variants fail with the head check switched off (confirmed by hand); the replaced-journal variant guards the existing `isSameFile` path.

**What:** Fix (defect from the live-session-watch plan): `readJournal` detects a restart only by file identity (`isSameFile`) or `size < offset`. `Store.OpenJournal` reopens with `O_TRUNC` on the same inode, so a journal regrown past the old offset within one `watchPollInterval` resumes mid-file and misses the new `run_started`.
**Regression guard.** Only a short read (`n < len(head)`, `io.EOF`) or a byte mismatch restarts at 0; any other `ReadAt` error returns the batch at its offset with head kept (readJournal's doc: a journal that will not read yields no items at the same offset — never `clearWatchView`), and every `readJournal` return carries the held head. The Goal's "(or replaced)" clause gets its own test: `os.Remove(f.src.journalPath)` before `f.reopen`, regrown past `m.watch.offset` (new inode, `isSameFile` false).
**Goal:** A watch over a journal truncated (or replaced) and regrown past the follower's offset between two polls restarts at byte 0, folds the new `run_started`, and repaints; a journal that only grows still reads incrementally from its offset.
**Approach (assumed at the header base):** in `internal/tui/watch.go`, the follower remembers the journal's first complete line (`head []byte`, cloned, never mutated — ADR 0011) when it reads from offset 0, carried through `watchBatchMsg` and stored by `foldWatchBatch`. On a later read with `offset > 0` and a head held, when neither existing test fired, `ReadAt` `len(head)` bytes at 0; a short read or mismatch is a truncation (`offset = 0`, head re-captured). Bundle offset, journal and head into one cursor value if `readJournal`'s parameter list would otherwise grow past five.
**Files:** internal/tui/watch.go, internal/tui/watch_test.go
**Read first:** internal/tui/watch.go — readJournal, followedJournal, isSameFile, watchBatchMsg, foldWatchBatch;
internal/tui/watch_test.go — TestWatchRepaintsOnTruncation, watchFixture.reopen, TestWatchFollowerPollsOnItsOwn (hand-built watchBatchMsg with no head)
**Tests:** `TestWatchRepaintsOnATruncationRegrownPastTheOffset`, modelled on `TestWatchRepaintsOnTruncation`: short prompt, poll; `f.reopen("again", 0)` plus a prompt long enough that the new size exceeds `m.watch.offset`; poll; only the new user entry shows and the legend reads "watching again". A same-title variant where only `time` differs. A replaced variant that `os.Remove`s `f.src.journalPath` before `f.reopen` and regrows past `m.watch.offset`: same outcome. The existing truncation and growth tests still pass.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWatch' ./internal/tui/`
**Closes:** watch-journal-reopen-missed
**Commit:** `fix(tui): restart a watch whose journal was reopened within one poll`
