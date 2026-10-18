---
title: Check on Windows that RemoveJournal tolerates a watcher's open journal
status: open
type: task
priority: 3
created: 2026-10-18T05:31:31Z
---

`Store.RemoveJournal` (`internal/session/store.go`) ignores a Windows removal error when a watcher has the Event journal open. Go opens files without FILE_SHARE_DELETE, so the owner's `os.Remove` fails while a watcher is following the journal. `isRemoveSharingError` (`internal/session/rename_windows.go`) treats ERROR_SHARING_VIOLATION **and** ERROR_ACCESS_DENIED as "a watcher holds it". The leftover journal then has a free lock, so `Live` skips it and the next startup sweep (`SweepJournals`) collects it.

This landed with plan item 10 of `2026-10-17 - 03 - live-session-watch-plan` (commit `8ae3b466`). It was only cross-compiled (`GOOS=windows go build` / `go vet`) and has never run on Windows.

Two open questions:
- Does the removal really fail with one of these two errors while a watcher holds the journal, and does the owner then exit cleanly with the journal left for the sweep?
- Is ERROR_ACCESS_DENIED too broad? It also hides a real permission failure, such as a read-only file or an ACL refusal, so a journal that can never be removed is silently left behind on every run.

## Acceptance

- On a real Windows host, with one apogee session owning a live session and a second one watching it: the owner ends, exits without an error, and the next startup's sweep removes the leftover journal.
- The errors observed are recorded in this issue. If ERROR_ACCESS_DENIED never appears in the watcher case, `isRemoveSharingError` stops accepting it. If it does appear, the case is told apart from a real permission failure, or the residual risk is written down in the code comment.
- Either way, a test or a recorded manual run covers the case.
