---
title: ADR 0103 decision
---

Every later item of the plan is bound by these decisions.

1. **Each Driver composes the journal as a sink.** The TUI, `apogee headless` and the daemon
   each wrap their Event sink in a journal writer. The engine learns nothing of it and stays
   wire-silent (ADR 0031 rule 1). The writer never sits inside `reactions.Runner`. Where headless
   also writes `--format json` to stdout, that encoder stays the outermost sink (ADR 0075 D9).
2. **The file and its envelope.** The journal is `<sessions>/<id>.events.jsonl`, beside the record
   it belongs to. Its lines are Event lines in the `v:2` envelope (ADR 0075 D3, D10). Token and
   reasoning deltas are merged over a window of about 30 ms, losslessly: the merged text equals
   the concatenated deltas, and no other line kind is merged. This coalesced form is a named
   variant of Event lines, pinned by its own golden. The writer is synchronous. On a write error
   it reports once and stops writing, and the run goes on (ADR 0075 D9).
3. **One frame pair per journal.** A journal holds exactly one `run_started` and one
   `run_finished`, and it spans one hold of one record id. `run_started` already carries
   `workspace` (`eventjson.RunStarted.Workspace`). The journal's own frame adds only `title` and
   `entries`: the number of transcript entries in the record when the journal was born. These two
   members are the journal's alone. Headless stdout frames do not change.
4. **Lifecycle.** The journal is born when the record is held and after the session has a prompt
   (`hasPrompt`). A run under `--no-save` writes none. The Driver removes it after the final save
   and before it releases the lock. The startup sweep removes an orphan journal whose lock is
   free, and it walks the sessions directory for that even when no retention policy is set. This
   supersedes the note that the unconfigured default is free because `Prune` walks no directory
   (`cmd/apogee/wire.go`, `gcSessions`; `internal/session/store.go`, `Retention`). The record stays
   the only history: nothing replays a finished journal.
5. **Watch mode is not a Driver.** It drives no loop and holds an inert Engine. It never calls
   `Store.Load`, `Hold`, `Save`, `Delete` or `Prune`, and it never takes a lock. It paints the
   record's first `entries` entries, then follows the journal from its start. Everything it reads
   is untrusted: text reaches the view only through the existing escape stripping
   (`internal/tui/sanitize.go`). Thinking is shown in the `/thinking` pop-up only, never inline.
   The prompt box reads `watching <title> · read-only · esc back`. esc returns to the session the
   watcher left, or to a fresh one after `--resume`. Watching parks the watcher's own session and
   leaves it untouched.
6. **A tailed file is not IPC** (ADR 0034 D2). The owner never learns a watcher exists, nothing
   flows back to it, and neither process depends on the other running.
7. **A watched home is a read-only source, never a second home** (ADR 0100). apogee lists and
   watches the sessions of a home named under `sessions.watch-homes`, and nothing else. Nothing in
   it is swept, written, held or configured from. Its rows are never resumable.
8. **`share-group` is the one opt-in exception to the owner-private modes.** With
   `sessions.share-group: <group>` set, the owner's records and journals are readable by that OS
   group. Both keys are global-only (ADR 0096 D6). This is OS-group read access, not RBAC, which
   stays a non-goal (ADR 0031).
9. **The Tool summary gains a wire mirror owned by `internal/eventjson`.** The mirror carries the
   facts of each variant. A nil summary writes `"summary":null`, by the encoder's no-omitempty
   rule (ADR 0075 D3). The mirror is decoded only for live watching and never on a record replay
   path, so the record and its codec do not change (ADR 0052 D5, ADR 0083 part 1).
10. **The interactive TUI holds its record at the first prompt.** The hold is taken when the first
    prompt is submitted (`hasPrompt`), before Turn 1: hold at the first prompt, never at the mint.
    A watcher can then find the session before its first Turn ends. This amends the timing in
    ADR 0022 D7's 2026-09-19 note (the first Save) and the "hold at BIRTH, never at the mint" rule
    documented on `internal/session/live.go`. A session that never sends a prompt still touches no
    disk.
11. **The journal carries a Driver-written `prompt` line.** Each top-level prompt and each
    Interjection writes one `prompt` line, because ADR 0025 keeps prompts off the Event stream. The
    Driver writes it; the engine emits nothing new.
12. **A watcher's probe never makes the owner refuse.** A watcher may probe a record's lock to tell
    whether the session is live. That probe is short and shared, and `Store.hold` absorbs a
    contention of that length instead of refusing. The retry lives in `Store.hold`, not in
    `platform.AcquireLock`.
13. **`SubAgentGroupEvent` writes a `sub_agent_group` line.** The Driver's encoder writes it,
    additively within `v:2`, so the engine stays wire-silent. This supersedes "writes no NDJSON
    line" in ADR 0025 D2's 2026-10-05 amendment.
14. **The doors.** `--resume <id>` and the browser's ⏎ on a held record open it in Watch mode
    instead of refusing. Rename, delete and `--continue` still refuse with the held-record line.
    The browser keeps one list: a live row gets a `● live` cell, and rows from watched homes are
    mixed in by time, tagged `· <label>` and read-only. When the owner ends, a banner says so and
    the view stays read-only. In the watcher's own home ⏎ then resumes through the normal door. A
    watched home is never resumable.

**What this amends.** ADR 0022 D7 and its consequences, ADR 0025 D2 (2026-10-05 amendment),
ADR 0034 D2, ADR 0052 decision 1 and amend-0824, ADR 0075 D5 and D13, and ADR 0100 each carry a
dated note pointing here.
