---
title: "apogee undo: 'nothing to undo' names neither the cause nor the home it read"
status: open
type: bug
priority: 2
plan: 2026-10-31 - 00 - empty-answers-name-their-cause-plan.md
created: 2026-10-30T21:46:47Z
---

`apogee undo <session-id>` prints the same bare line, `nothing to undo for session <id>` (`undoNothingToDo`, `cmd/apogee/undo.go`), for every way the verb can come up empty:

- the home it read has no undo index for the session (`snapshot.ErrNoIndex`), because the id is wrong, `--config`/`APOGEE_CONFIG` points at a different apogee home than the run used, or the run was recorded with `undo-snapshots` off or no git;
- the session's journal holds no step, because the run wrote no files or every exchange has been undone already.

The line does not name the home or the snapshot dir it looked in either. The TUI's empty answer already names its reason (`undo.NothingLines`, ADR 0074 decision 2), and so does this verb when the store reports one. The case with no index, though, gets nothing.

`apogee undo --help` also says a session recorded without snapshots "has nothing to revert here, and says so". The verb doesn't say so: that case reaches the same bare line through `ErrNoIndex`. The help gives the store as `~/.apogee/snapshots` even though the verb reads `<home>/snapshots` for whatever home `--config` or `APOGEE_CONFIG` picks.

Seen in Seqcom's dogfood runs on v0.29.0 (seqcom runs 20261030T1208Z and 20261030T1738Z). The headless agent ran `apogee headless` under one apogee home and `apogee undo` under another, and got the bare line back. It spent four headless runs and 13 `strings $(which apogee) | grep snapshot` calls finding out where the snapshot store lives. A message that named the home it read and said there was no index for the session there would have pointed it straight at the cause.

## Why

An unattended Driver (headless, daemon, a supervising agent) has only this line to act on. Each cause needs a different fix (another --config, another id, snapshots turned on, nothing to do), and a bare 'nothing to undo' sends the caller off to reverse-engineer the store.

## Acceptance

- When no undo index exists, the verb's error names the apogee home (or snapshot dir) it read and says there is no index for the session there, so it reads differently from a journal that is present but empty.
- When the journal is present but empty, the verb words its answer as undo.NothingLines does, so the TUI and the verb agree (ADR 0074 decision 2).
- apogee undo --help matches the verb: it gives the store as <home>/snapshots, with --config/APOGEE_CONFIG choosing the home, and its claim about sessions recorded without snapshots holds.
- Tests in cmd/apogee pin both messages: no index under a given --config home, and a journal walked back to its start.
