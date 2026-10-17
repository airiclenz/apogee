---
title: "Event journal: the per-session Event lines file a Driver writes while it holds a record"
---

**Event journal**:
The per-session file `<sessions>/<id>.events.jsonl` a **Driver** writes beside a **Session
record** while it holds that record: the TUI, `apogee headless` and `apogee daemon` alike. Its
lines are **Event lines** in the `v:2` envelope, bracketed by one `run_started`/`run_finished`
pair, with token and reasoning deltas merged over a window of about 30 ms without losing text. Its
`run_started` adds `title` and `entries`, the record's transcript entry count when the journal
was born. It also carries a Driver-written `prompt` line for each top-level prompt and
Interjection, because prompts are not Events. It is born when the record is held and the session
has a prompt, never under `--no-save`, and it is removed after the final save, before the hold is
released. The startup sweep removes an orphan whose lock is free. **Watch mode** follows it. The
engine never sees it: the Driver composes it as a sink. See ADR 0103.
_Avoid_: "log" (it is deleted at release and nothing replays it; the record is the history),
"event log", "stream file".
