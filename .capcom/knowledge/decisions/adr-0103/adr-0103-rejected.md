---
title: ADR 0103 considered options
---

- **An `apogee watch` subcommand.** Rejected. `/sessions` and `--resume <id>` are the doors to a
  session already, and a third door would repeat their rules.
- **A socket or other channel from the owner.** Rejected. It would make the owner aware of
  watchers and give apogee a network or IPC surface to secure (ADR 0034 D2). A file the owner
  writes anyway costs neither.
- **Tailing the record.** Rejected. The record is saved per Turn and holds no streamed text, no
  tool card in flight and no thinking.
- **Keeping the journal after release, for replay.** Rejected. The record is the one history,
  and a kept journal would be a log file with a retention policy, which apogee does not own.
- **One line per token delta.** Rejected. A long answer writes thousands of lines to disk. The
  coalesced form loses no text.
- **Thinking inline in the watched transcript.** Rejected. Thinking stays in the Thinking pane,
  which a watcher fills live.
- **Reading config or skills from a watched home.** Rejected. A watched home is a read-only
  source of sessions, never a second home (ADR 0100).
- **Per-user access lists.** Rejected. That is RBAC, a non-goal (ADR 0031). An OS group is
  enough.
