---
title: No test pins isRemoveSharingError refusing ERROR_ACCESS_DENIED
status: open
type: task
priority: 3
labels: [windows]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-09T15:42:01Z
---

Plan `2026-11-09 - 02 - windows-journal-remove-check-plan` item 1 (commit `d6a88372`) narrowed `isRemoveSharingError` (`internal/session/rename_windows.go:40`) to accept ERROR_SHARING_VIOLATION alone, so an ERROR_ACCESS_DENIED from a journal removal is now reported rather than treated as "a watcher holds it". No test bites that narrowing: `internal/session/journal_windows_test.go` covers the watcher case and the read-only case (`TestWindowsRemoveJournalOfAReadOnlyJournal`, line 85), but reverting the classifier to also accept ERROR_ACCESS_DENIED would leave every test green.

## Why

The narrowing is the behaviour change the item shipped; without a test a later edit can silently widen the classifier again and hide a real ACL refusal.

## Acceptance

- A Windows unit test in `internal/session/journal_windows_test.go` asserts `isRemoveSharingError(windows.ERROR_ACCESS_DENIED)` is false (and `isRemoveSharingError(windows.ERROR_SHARING_VIOLATION)` is true), and it fails if the classifier accepts ERROR_ACCESS_DENIED again.
