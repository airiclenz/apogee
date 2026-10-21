---
title: "ADR 0076 amendment of 2026-10-21 (second): the compaction-finished notice and its advise slot"
---

Decided by the owner on 2026-10-21 (plan for issue `session-start-advise`). Decisions 1 and 6
stand as first written; on these points this amendment extends them for a Compaction:

- **`compaction-finished` is a notice, fired once after every fold that ran.** Decision 1's notice
  list gains it: the estimate-driven fold, either emergency fold and `/compact` each fire it once
  the `Replace` has landed; a fold that faulted, was cancelled or replaced nothing fires none, and a
  capped delegate's report summary is not a fold. Like `session-started` it fires on the top-level
  agent only — a delegate never fires it. Its payload carries `fold`, one of
  `auto | emergency | manual`. It is published through the Driver's `ObserveRunner` as
  `session-started` is, so ADR 0018 decision 6's "no new Event variant" holds.
- **Its advise slot is the next user-role message after the fold.** The runs start when the fold
  begins, so the summary call covers their latency, and are joined after the `Replace`: the
  answer rides fenced ahead of the text of the next user-role message — the Exchange's opening for a
  fold between Exchanges, the overflow bridge (ADR 0018 decision 4) for a fold mid-Exchange. Never
  the assistant summary, never the system prompt. Ephemeral, capped, Bypass-off and fail-open
  exactly as the session-start slot; a fold that discards its runs discards their advice.
- **A fresh briefing replaces its own stale one.** A span that attaches at `compaction-finished`
  strips the earlier advice spans of the *same reaction id* from the protected prefix's first user
  message, so the model never reads a stale briefing beside the fresh one. Spans of other reactions,
  and every span when the run fails or times out, stay. The fold has already broken the cache after
  the prefix, so this costs only that one message's cache.
- **The user cell is admitted under `adr-0076-amend-1021`'s structural bar**, which this slot meets.
