---
title: "Live session: a Session record another apogee instance holds right now"
---

**Live session**:
A **Session record** that an apogee instance holds right now, as the hold probe sees it: its
`<id>.lock` is taken. Any **Driver** can be its owner. A live session has an **Event journal**,
and the `/sessions` browser marks its row `● live`. From a second instance its doors differ from a
released record's: `--resume` and ⏎ open it in **Watch mode**, while rename, delete and
`--continue` refuse it. See
ADR 0103 and ADR 0022.
_Avoid_: "running session" for the record (the session runs in its owner; this names the record
as the store and a watcher see it), "open session", "locked session".
