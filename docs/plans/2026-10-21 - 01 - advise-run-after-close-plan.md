# Plan — advise run after Close

**Goal:** a `session-started` advise entry armed by a live swap after `Agent.Close` starts no advise run, and the session-advise tests bound every wait on a run's start through `internal/testtime`. Closes the gap deferred from the user-session-advice plan (item 6, commit `39bd22c2`).
**Date:** 2026-10-21
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 4b290a8b
**Closes:** advise-run-after-close

**Regression check (2026-10-21, 4b290a8b):**
- 1: guard folded — Close shuts the window for both lanes; the test pins the observe half too.
- 2: guard folded (writer decision) — item 2 touches only `internal/agent/sessionadvise_test.go`.

**Sources:**
- `.capcom/issues/advise-run-after-close.md`
- `docs/plans/archived/2026-10-21 - 00 - user-session-advice-plan.md` (item 6)
- ADR 0076 (`capcom know show "ADR 0076"`); `docs/design/test-drivers.md` §Waiting

**Ratified design calls:**
- **Fix site:** `Close` shuts the session window; the advise lane keeps its current `extend` contract (owner, 2026-10-21, via the issue's first acceptance option).

**Standing requirements:**
- skills: coding-standards
- This machine: never race-run a whole heavy package; one package and one test at a time with `GOMEMLIMIT=2GiB`; never two test runs at once.

**Out of scope:**
- A Session boundary announced after `Close` (reopening the window) — no Driver does it.
- Any change to `sessionAdviseLane` semantics beyond what item 1 needs.

## 1. Close shuts the session window — ✅ DONE (2026-10-21)

NOTES (2026-10-21): the test arms the observe entry through SetReactions (armObserve) before installing the advise probe, because installGeneration installs only Floor/Bypass/Sync/ContextFillNotice and never the observe lane; the plan's "install a generation arming … an observe entry" read as one installGeneration call would not reach the Runner on the base tree. The test fails on the base tree on both the waiting() and the armedCalls() assertions.
NOTES (2026-10-21): no CLOSES line — the plan header's `advise-run-after-close` needs item 2's bounded `<-started` wait as well (its second Acceptance bullet), so item 1 alone does not resolve it.

**What:**
**Goal:** after `Agent.Close` returns on a top-level Agent, a live swap that arms a new `session-started` advise entry (the `fireNewlyArmed` path `SetReactions` takes) leaves the session-advise lane with no pending run and launches no handler.
**Approach (assumed at the header base):** fix for the bug deferred from commit `39bd22c2`: `Agent.Close` stops `sessionAdvice` / `compactionAdvice` but never calls `sessionWindow.shut`, so `sessionWindow.admit` still fires and `extendSessionAdvice` → `sessionAdviseLane.extend` mints a fresh context nothing cancels. In `Close`'s non-delegate branch, call `a.window.shut()` BEFORE `a.sessionAdvice.stop()`: `admit` runs its `fire` under the window lock, so shutting first waits out an in-flight admit, and the lane stop that follows cancels whatever it enqueued. Update `Close`'s and `sessionWindow`'s doc comments so every comment listing what shuts the window names `Close` (rule: grep `window` / `shut` in `internal/agent/agent.go` and `internal/agent/sessionadvise.go`).
**Regression guard.** The window gates BOTH lanes (`fireNewlyArmed` feeds `extendSessionAdvice` and `ObserveRunner.SessionStartedFor` from one `admit`), so after the fix an observe `session-started` entry armed after `Close` no longer reaches the Runner either: state it in `Close`'s and `sessionWindow`'s doc comments (no production Driver swaps after `Close` — `cmd/apogee/wire.go` closes `hooks` first). `Close`'s "it does not end the Agent" line (agent.go:850-852) stays — shutting the window is consistent with it — but the new wording must not imply a later swap still arms the closed session.
**Files:** internal/agent/agent.go, internal/agent/sessionadvise_test.go
**Read first:** internal/agent/agent.go — Close, fireNewlyArmed, sessionWindow.admit, sessionWindow.shut; internal/agent/sessionadvise.go — waiting;
internal/agent/sessionadvise_test.go — liveAdviceAgent, blockedUntilCancel; internal/agent/sessionstart_test.go — startRecordingRunner
**Tests:** new `TestSessionAdvise_ALiveArmAfterCloseStartsNoRun` in `internal/agent/sessionadvise_test.go`: build an agent with `liveAdviceAgent(..., true)`, `Close` it, install a generation arming a `blockedUntilCancel` probe on `session-started` in the advise lane and an observe `session-started` entry, call `fireNewlyArmed`, then assert deterministically — `a.sessionAdvice.waiting()` is empty, `probe.calls()` is zero, and the `startRecordingRunner`'s `armedCalls()` is empty — never by sleeping. It must fail on the base tree.
**Acceptance:**
- `go build ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionAdvise_' ./internal/agent/`
- `go vet ./internal/agent/`
**Commit:** `fix(agent): Close shuts the session window so a later live swap starts no advise run`

## 2. Bound the session-advise tests' waits on a run's start

**What:**
**Goal:** no test in `internal/agent/sessionadvise_test.go` receives from a `blockedUntilCancel` `started` channel without a `internal/testtime` bound; each such wait fails with a message naming what did not start.
**Approach (assumed at the header base):** the bare `<-started` receives (three at the base, including the one in `TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary`) become a `select` on `started` and `time.After(testtime.Budget(10 * time.Second))` with `t.Fatal`, the idiom the same file already uses for `ended`; a small shared helper is fine if it reads like the file. Rule for scope: every bare receive on a `started` channel in the file (`grep -n '<-started' internal/agent/sessionadvise_test.go`).
**Regression guard.** The cmd/apogee test holds no channel receive at the base, so item 2 touches only internal/agent/sessionadvise_test.go.
**Depends on item 1** (same test file).
**Files:** internal/agent/sessionadvise_test.go
**Read first:** internal/agent/sessionadvise_test.go — blockedUntilCancel, TestSessionAdvise_EscDuringTheJoinEndsTheWait, TestSessionAdvise_ABoundaryCancelsThePreviousRun, TestSessionAdvise_ALiveArmedRunEndsAtTheNextBoundary
**Tests:** the touched tests themselves.
**Acceptance:**
- `test -z "$(grep -nE '^\s*<-started\s*$' internal/agent/sessionadvise_test.go)"`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSessionAdvise_' ./internal/agent/`
**Commit:** `test(agent): bound session-advise waits on a run's start with testtime`
