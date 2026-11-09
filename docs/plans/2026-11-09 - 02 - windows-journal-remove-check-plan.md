# Windows: RemoveJournal under a watcher's open journal — plan

- **Goal:** prove on the Windows CI job that `Store.RemoveJournal` returns nil while a watcher holds the Event
  journal open, that the sweep collects the leftover, and narrow the removal classifier to the one errno that case
  produces.
- **Date:** 2026-11-09
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 1eaef158
- **Sources:** issue `windows-journal-remove-check`; `docs/plans/archived/2026-10-17 - 03 - live-session-watch-plan.md`
  item 10; Go 1.26 `syscall.Open` on Windows (share mode `FILE_SHARE_READ|FILE_SHARE_WRITE`, no
  `FILE_SHARE_DELETE`) and `os.Remove` (clears a read-only attribute and retries itself).
- **Ratified design calls** (owner, 2026-11-09):
  - **Classifier:** `isRemoveSharingError` accepts `ERROR_SHARING_VIOLATION` only, decoupled from
    `isRenameSharingError` (which keeps `ERROR_ACCESS_DENIED` for `MoveFileEx`). A real ACL refusal then surfaces
    as the owner's "session journal stopped" notice; the journal stays and the sweep retries.
  - **Evidence:** the Windows CI job is the Windows host; the test logs the errno; the issue closes once that log
    line from a green CI run is recorded with `capcom log`. No two-process e2e (the PTY driver is a Windows stub
    and `cmd/apogee` is outside the Windows job); the unit test drives the watcher's own open call.
- **Standing requirements:**
  - skills: coding-standards
  - This box is Linux: verify with `GOOS=windows go vet ./internal/session/`; never run the full suite here.
  - Tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; pass this rule to every sub-agent.
- **Out of scope:** running all of `internal/session` on Windows CI (a follow-up issue); `cmd/apogee` on Windows CI;
  a raw Win32 probe telling ACL refusals apart.
- **Regression check (2026-11-09, 1eaef158):**
  - 1: guard folded (Goal restated to what Linux checks; real `-run` subset; `make actionlint`; `test-windows`
    comment; ADR 0103 amendment blockquote).
  - 2: recast (errno 5 restores `ERROR_ACCESS_DENIED` with the residual documented and blocks closeout, ADR 0101).
  - 1 (re-check): guard folded (the watcher test logs the errno before asserting it, so a red run carries it).
  - 2 (re-check): guard folded (stale "stop and report" removed; `gh run watch` on the run whose `headSha` holds item
    1; red-for-another-reason branch; errno 5 → one fix commit moving item 1's test, ADR blockquote and CHANGELOG
    line, committed before the issue closes rather than blocking the closeout).
  - 2: rejected — add CHANGELOG.md to Files; plan-lint refuses a Files line naming CHANGELOG.md (the entry travels in
    the item's sidecar).

## 1. Pin RemoveJournal under a watcher on Windows, narrow the classifier — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the watcher test asserts only that SweepJournals returns a non-nil error and 0 removed while the watcher is open, not which errno; the errno is pinned once, on the raw os.Remove.

**What:** fix(session) — the ERROR_ACCESS_DENIED acceptance copied from the rename predicate hides a real
permission failure on every run.
**Regression guard.**
- The Goal is what this item can check on Linux (the test file's assertions, `GOOS=windows` vet, the classifier
  body, the `ci.yml` step); whether the tests pass on Windows is item 2's evidence.
- Acceptance runs real Linux tests: no `TestRemoveJournal*`/`TestSweepJournals*` exists (a vacuous "no tests to
  run"), so `-run '^TestStore(Journal|SweepJournals|Live$|DeleteAndPruneRemoveTheJournal)'`; and it lints with
  `make actionlint` (pinned, what CI runs) — this box has no `actionlint` on PATH and no PyYAML.
- The `test-windows` comment ("These five trees rather than `./...`", "The five trees run as two steps") is updated
  to name the third step, why it runs only `TestWindows|TestAtomicWriteRetries` (the rest of `internal/session` is
  unproven on Windows, a follow-up issue), and its `!cancelled()`.
- `adr-0103-amend-1017` ("Only `GOOS=windows go vet` checks this path; no test runs it, and the CI Windows job does
  not cover `internal/session`") gains a dated `> **Amended 2026-11-09 (docs/plans/2026-11-09 - 02 -
  windows-journal-remove-check-plan.md).**` blockquote under that paragraph — CI runs the watcher case, the
  classifier accepts `ERROR_SHARING_VIOLATION` only, an ACL refusal surfaces — named on `adr-0103`'s Status line.
**Goal:** a `//go:build windows` test in `internal/session` that holds the journal open with the watcher's own
open call and asserts: raw `os.Remove` fails with `ERROR_SHARING_VIOLATION` (logged before that assertion as
`RemoveJournal watcher case: os.Remove = <err> (errno <n>)`); `RemoveJournal` returns nil and the file remains;
once the hold is released `Live` excludes the id; `SweepJournals` fails to remove it while the watcher is open and
removes it (count 1, nil error) once the watcher closes. A sibling test asserts a read-only journal is removed by
`RemoveJournal`. Both vet under `GOOS=windows`; `isRemoveSharingError` tests only `ERROR_SHARING_VIOLATION`; the
`test-windows` job carries the step that runs them. Their Windows outcome is item 2's.
**Approach (assumed at the header base):** new `internal/session/journal_windows_test.go` with
`TestWindowsRemoveJournalUnderAWatcher` and `TestWindowsRemoveJournalOfAReadOnlyJournal`, reusing `holdStore`,
`liveJournal` and `journalExists` from `store_test.go`; the watcher handle is a plain `os.Open(JournalPath)` as
`followedJournal` (`internal/tui/watch.go`) opens it, closed by `t.Cleanup` before the temp dir goes.
`rename_windows.go` `isRemoveSharingError` tests `errors.Is(err, windows.ERROR_SHARING_VIOLATION)` and its
comment and `RemoveJournal`'s comment (`store.go`) state the residual: an ACL refusal is reported, not hidden.
`.github/workflows/ci.yml` job `test-windows` gains a third step (`if: ${{ !cancelled() }}`):
`go test -race -count=1 -v -run 'TestWindows|TestAtomicWriteRetries' ./internal/session/`.
**Files:** internal/session/journal_windows_test.go; internal/session/rename_windows.go; internal/session/store.go; .github/workflows/ci.yml; .capcom/knowledge/decisions/adr-0103/adr-0103-amend-1017.md; .capcom/knowledge/decisions/adr-0103.md
**Read first:** internal/session/rename_windows.go — isRemoveSharingError, isRenameSharingError; internal/session/store.go — RemoveJournal, SweepJournals, Live, JournalPath; internal/session/store_test.go — holdStore, liveJournal, journalExists, TestStoreSweepJournals;
internal/session/rename_windows_test.go — TestAtomicWriteRetriesThroughASharingViolation; internal/tui/watch.go — followedJournal; cmd/apogee/wire_firing.go — firingJournalFile.End, firingJournal;
internal/platform/lock_windows.go — lockFile, probeLockFile; .github/workflows/ci.yml — test-windows
**Tests:** the two new Windows tests; `TestAtomicWriteRetriesThroughASharingViolation` now runs on CI too; the Linux
journal tests (`TestStoreJournalLifecycle`, `TestStoreSweepJournals`, `TestStoreLive`,
`TestStoreDeleteAndPruneRemoveTheJournal`) stay green.
**Acceptance:**
- `GOOS=windows go vet ./internal/session/`
- `go vet ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run '^TestStore(Journal|SweepJournals|Live$|DeleteAndPruneRemoveTheJournal)' ./internal/session/`
- `make actionlint`
- `awk '/^func isRemoveSharingError/,/^}/' internal/session/rename_windows.go` shows `ERROR_SHARING_VIOLATION` and no `ERROR_ACCESS_DENIED`
- `grep -n "TestWindows|TestAtomicWriteRetries" .github/workflows/ci.yml` matches the new `test-windows` step
- `grep -n 'Amended 2026-11-09' .capcom/knowledge/decisions/adr-0103/adr-0103-amend-1017.md`
**Commit:** `fix(session): accept only a sharing violation as a watcher-held journal`

## 2. Record the CI evidence and close the issue (owner-gated)

**What:** Recast at the regression check (2026-11-09). manual — needs a push, which only the owner asks for. Depends on
item 1. After the owner pushes `main`, take the newest `ci.yml` run on `main` whose `headSha` contains item 1's commit
(`gh run list --workflow ci.yml --branch main --json databaseId,headSha,status`; `git merge-base --is-ancestor <item 1
sha> <headSha>`), wait on it with `gh run watch <id> --exit-status`, then read
`gh run view <id> --log | grep 'RemoveJournal watcher case'`:
- Green with errno 32: `capcom log windows-journal-remove-check "<log line> — CI run <url>, commit <sha>"`, then close
  it naming both tests.
- Red with errno 5: the errno-5 path in the Regression guard.
- Red for any other reason (a `Live` or `SweepJournals` assertion, another errno, a failure before the log line):
  `capcom log` the failing line, leave the issue open, report to the owner.
The issue closes by hand with the evidence (no `**Closes:**` line). Until the owner pushes, the executor leaves it open
with one `capcom log` line saying the evidence awaits the CI run.
**Regression guard.** Errno 5 is a regression (ADR 0101): `capcom log` it, then one fix commit restores ERROR_ACCESS_DENIED
in `isRemoveSharingError` (residual — an ACL refusal left for the sweep — in its and `RemoveJournal`'s comments), moves
`TestWindowsRemoveJournalUnderAWatcher` to the observed errno (still logged first), supersedes item 1's `adr-0103-amend-1017`
blockquote and corrects its CHANGELOG line via the sidecar; the issue stays open until the next green run is logged.
**Files:** .capcom/issues/windows-journal-remove-check.md; on errno 5 also internal/session/rename_windows.go; internal/session/store.go; internal/session/journal_windows_test.go; .capcom/knowledge/decisions/adr-0103/adr-0103-amend-1017.md
**Read first:** internal/session/rename_windows.go — isRemoveSharingError; internal/session/store.go — RemoveJournal, SweepJournals; .capcom/issues/windows-journal-remove-check.md — Acceptance;
.github/workflows/ci.yml — on.push.paths (excludes .capcom/**), test-windows; .capcom/knowledge/decisions/adr-0103/adr-0103-amend-1017.md; .capcom/knowledge/decisions/adr-0103.md — Status line
**Tests:** none on errno 32 (records CI evidence); on errno 5, `TestWindowsRemoveJournalUnderAWatcher` asserting
ERROR_ACCESS_DENIED, `GOOS=windows go vet ./internal/session/` over the restored classifier and the test, and the
next green `test-windows` run as the evidence.
**Acceptance:**
- `capcom show windows-journal-remove-check | grep -n 'RemoveJournal watcher case\|awaits the CI run'`
**Commit:** `chore(issues): record the Windows RemoveJournal CI evidence`; on errno 5, before it, `fix(session): accept ERROR_ACCESS_DENIED again for a watcher-held journal`
