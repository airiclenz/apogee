---
title: ADR 0088 decision
---

**A cancel stops work and keeps everything that finished; it never rewinds.** Undoing a file change
is git's job. Rewinding a conversation is `/undo`'s job, and `/undo` is unchanged.

> **Note (2026-10-02, verified against code):** `/undo` does not rewind a conversation; it reverts
> files only. `confirmUndo` (`internal/tui/undo.go`) calls `Agent.UndoRevert`
> (`internal/agent/agent.go`), which is a generation check and `undo.Journal.Revert`: it restores the
> pre-images and removals of the per-Exchange file journal and leaves the conversation as it is
> (`internal/undo/doc.go`, package doc). ADR 0074 rejects a conversation-restoring checkpoint and
> leaves that to the session record (`/sessions`, ADR 0022), and ADR 0086 D3's table says `/undo`
> "reverts files, not the conversation". Both agree with the code. Read the sentence above as: a
> cancel rewinds nothing. A human who wants an Exchange's file writes back uses `/undo`, which is
> unchanged; anything beyond its reach is git's job, and a conversation is rewound through
> `/sessions`.

**D1 — The Turn in flight is settled, not rolled back.** If the cancelled Turn issued tool calls,
its reply stays in the conversation and every call gets a result:
- a finished call keeps its real result;
- a running call ends through its `ctx` and gets `cancelled by the user while it ran`;
- a call that never started gets `not run: cancelled by the user`.

The Exchange then closes without an answer, with the `[engine — cancelled]` marker Stage A already
places. A Turn cancelled before its reply finished streaming issued nothing that ran, and is dropped
as today. An Exchange with no finished Turn is still scrapped (`Agent.AbortExchange`).

> **Amended 2026-09-29.** A recipe launch is the one exception to "an Exchange with no finished
> Turn is still scrapped". A `/<id>` line runs its Workflow before the model's first request, and
> the Exchange's opening user message then carries the user's line and the Workflow's result lines.
> A cancel of that launch keeps the opening, with the stopped result lines —
> `stopped by the user: K of N done` among them — and the `[engine — cancelled]` marker on it,
> however far the launch's first Turn got; no model request follows. Scrapping it would leave the
> model's next request knowing nothing of a run whose finished items stay on disk (D4). A launch
> the engine refused before any Workflow ran carries no result lines and is still scrapped
> (`turnLifecycle.settle`).

**D2 — A delegation pool keeps its finished children.** Under a cancel:
- a finished child's report is its result;
- a running child is **stopped** exactly as `^x` stops it (ADR 0086 D4): it gets a fold, a non-error
  partial result, and retention under its name, so the parent can `continue:` it;
- a queued child gets the not-started result.

The stop's summary is bounded: each running child gets **20 seconds** to fold, and one that cannot is kept with `summary unavailable`; a second `esc×2` while folds run skips the rest at once. A cancel that is a quit or a daemon shutdown (a distinct context cause) skips the folds entirely, so neither ever waits on a child (owner, 2026-09-27).

The retained set is no longer restored to its Turn-start value; what a finished or stopped child
left is kept. This closes Stage B of `apogee-2un`.

**D3 — A workflow is stopped and kept.** A blocking `fan_out` under a cancel stops its running
items and answers the call with how many items finished and the report-so-far path. A later
`fan_out` with the same brief, items and inputs resumes it (ADR 0087 D4). `esc` never touches a
**Background workflow** (ADR 0089); the `/workflows` view and the `workflow` tool stop those.

> **Amended 2026-09-29.** A stopped Workflow's answer now ends on the line that says how to re-run
> it, keeping its finished items: `` to resume: re-run `<line>` — finished items are kept `` for a
> recipe the user typed, `` to resume: run `/<id>` again with the same inputs — finished items are
> kept `` for one launched with its inputs bound, and
> `to resume: call fan_out again with the same arguments — finished items are kept` for a
> `fan_out` call. A recipe launch also carries that command on its started phase, and its stopped
> block's footer shows it to the user (ADR 0090 D1, amended 2026-09-29). A recipe launch re-run the
> same way resumes the stored Workflow by its plan hash exactly as a `fan_out` does (ADR 0087 D4);
> its cancel keeps the launch's opening (D1, amended 2026-09-29).

> **Amended 2026-09-30.** The re-run resumes the stored Workflow only once no background run
> (ADR 0089) is still driving its folder: a blocking re-issue onto a folder a background run still
> drives is refused, naming that run, and nothing is created or spawned (ADR 0087 D4, amended
> 2026-09-30).

**D4 — Finished work written to disk stays written.** Nothing apogee does on a cancel reverts a
file. A settled Turn's results tell the model what happened, so its next request starts from the
truth rather than from a conversation that forgot work the workspace still shows.
