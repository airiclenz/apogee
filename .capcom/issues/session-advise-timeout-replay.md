---
title: A timed-out session-start advise run replays after an aborted Exchange
status: open
type: bug
priority: 2
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-19T18:51:04Z
---

The session-start advise lane's join (`joinSessionAdvice`, `internal/agent/sessionadvise.go:212`) reports a run that missed its deadline (`internal/agent/sessionadvise.go:267`) and still takes it into `delivered` (`take`, `internal/agent/sessionadvise.go:89`). When that Exchange aborts, `restoreDelivered` (`internal/agent/sessionadvise.go:98`) puts the run back ahead of `pending`, so the next opening joins it again: it reports the same timeout a second time, or, when the handler has finished in the meantime, attaches its late answer to the next prompt.

Found deferred at item 7 of the session-started-reactions plan.

## Acceptance

- A run reported as timed out at a join is never reported again nor attached at a later opening, whether or not that Exchange aborts.
- A test in `internal/agent/sessionadvise_test.go` drives a timeout, an aborted Exchange and the next opening, and pins one report and no attached advice.
