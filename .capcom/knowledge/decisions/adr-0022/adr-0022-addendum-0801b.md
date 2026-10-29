---
title: "ADR 0022 addendum of 2026-08-01 (second): the save gate is \"a prompt was sent\""
---

Decision 1's "empty sessions are never written" was implemented as *any* entry the record would
keep, and the addendum above narrowed that only by the ephemeral flag. Both readings were still too
loose: plenty of **persisted** entries legitimately precede any prompt — a `/confine` status note,
the `/skills` catalogue, a `/model` actuation note, the `/sessions` browser's "no saved sessions"
notice, an error note — so a launch spent poking at slash commands and then quitting (or `/clear`)
filed a record titled `Session <date>` reading `0 msgs`. The owner's directive of 2026-08-01: a
completely new session must not appear in history until a prompt has been sent. Nothing else in
this ADR moves — the record shape, the per-Turn cadence, and the per-layer versioning are untouched.

- **One predicate, all three gates.** The gate is now `hasPrompt` — true iff the transcript holds at
  least one committed user entry — and `saveSession` (clean quit, and the `/clear`|`/new` rotation
  of Decision 4 — both are `saveAtIdle` since the second 2026-08-02 addendum), `persist` (the
  per-Turn fold) and `saveAtIdle` all read it. Keeping the
  "worth saving?" rule in one predicate costs the per-Turn paths nothing: a Turn only exists
  because a prompt opened it, so those two gates were already unreachable before the first prompt.
- **The policy stays at the TUI gate.** `internal/session` and the host seam remain policy-free —
  the first `Save` still mints the id and creates the file. Which sessions deserve a record is a
  question about the conversation, so it is answered where the conversation is held.
- **Interjections are deliberately excluded.** An interjection rides an Exchange that a user entry
  opened, and a restored scrollback carries that opening entry, so counting `entryInterjected`
  could never change the answer — mirroring the exclusion rationale on the user-text accessors and
  keeping the gate exactly "a prompt exists" ⇔ "a user entry exists".
- **Accepted consequence: legacy no-blob resumes skip the quit-flush.** Resuming a pre-plan bare
  envelope (no transcript blob) leaves the scrollback with no user entry, so quitting without
  prompting saves nothing. Nothing is lost — that record is already on disk; only a cosmetic
  context-fill/`UpdatedAt` refresh is missed.
- **Records already written by earlier builds are left alone.** There is no retro-pruning of the
  empty `Session <date>` records those builds filed; the browser's `^d` deletes them, consistent
  with "no auto-pruning, manual `^d` only" in the Consequences above.

> **Amended 2026-10-29 (verified against code).** The single predicate is now `worthSaving`
> (`internal/tui/sessionsave.go`): `hasPrompt` **or** a background workflow still running
> (`workflowsRunning`). This has held since 2026-09-28 (commit `ad3fee2b`, ADR 0087/0089).
> A session whose only work is a running Background workflow is saved at quit, because its
> snapshot entry is what a resume restarts the workflow from. For a session with no workflow
> running, the gate is still exactly "a prompt exists" (`hasPrompt`, unchanged). `saveSession`
> is gone: the clean quit and `/clear`|`/new` both schedule through `saveAtIdle`.
> `persist`, `saveAtIdle` and `progressSave` all answer to `worthSaving`.
