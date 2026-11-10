---
title: Race detector flags rowCountMemo shared across parallel tui subtests
status: open
type: bug
priority: 2
plan: 2026-11-10 - 00 - open-bug-fixes-plan
created: 2026-11-10T06:21:28Z
---

`rowCountMemo.rows` (`internal/tui/prompteditor.go:488`) writes its memo in place. `newPromptEditor` puts it behind a pointer (`rowCount *rowCountMemo`, line 77/225), so every copy of the value-copied Model shares one memo. Parallel subtests that share a Model then write it at the same time. `go test -race` reports it in about 1 run in 3, in `TestWorkflowsViewClickIsSwallowedByTheNameRow` and about 8 more tests.

Came in with f65794ae (prompt-box scrollbar plan, `docs/plans/archived/2026-11-09 - 03 - prompt-box-scrollbar-plan.md`). Found on 2026-11-09 during the side-panel visual alignment run, which does not touch this code.

Not yet decided: whether only the tests share a Model across goroutines (then fix the tests), or the shipped binary can also reach the memo from two goroutines (then fix the memo).

## Why

A race report fails `make check` at random, which hides real failures and makes every run slower to trust.

## Acceptance

- The cause is named: a test sharing the memo, or a production path doing it
- `GOMEMLIMIT=2GiB go test -race -count=10 -run 'TestWorkflowsViewClickIsSwallowedByTheNameRow' ./internal/tui/` passes, and so do the other tests that reported the race
