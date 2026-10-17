---
title: ADR 0022 decision
---

**1. Save cadence is per-Turn, asynchronous, and soft on failure.** The worker snapshots the
engine between Steps — at each `StatusTurnComplete` quiescent boundary, the only point a
snapshot is valid (ADR 0007) — and the Model
persists asynchronously through a single-flight, latest-wins save so a burst of Turns collapses
to one in-flight write plus one pending. A crash therefore loses **at most one Turn**, never the
whole session (the owner chose maximum crash-safety over the cheaper per-Exchange cadence). A
save failure **never interrupts the conversation**: it is noted once on the ok→fail transition
(and recovery noted on fail→ok) and the run continues — parity with apogee-code's swallowed
`session_save_failed`. Empty sessions are never written — a session is saved only once its
transcript holds a committed **user prompt** (sharpened from "an entry the record would actually
keep" by the second 2026-08-01 addendum below; neither the start-up box nor an ephemeral notice
ever qualified under either reading).

The ordering that makes an async per-Turn save safe is load-bearing and is stated in the
worker's doc comment: every event of a Turn is delivered to the Model *synchronously inside*
the Step, so a snapshot the worker sends *after* the Step returns is ordered strictly after that
Turn's events — the transcript the Model holds when the snapshot folds in is consistent with it.

**2. Persistence is dual-representation.** The on-disk `Record` wraps **two opaque payloads**
beside its metadata: the untouched engine `domain.Session` envelope **and** the versioned
scrollback blob. Resume repaints the scrollback exactly — sub-agent `Depth`, tool
cards, notes, presented documents — instead of showing a bare box over a remembering engine. The
transcript blob is **owned by `internal/session` and versioned independently**, opaque to the
store, mirroring how `Session.State` is engine-owned and opaque to `domain`. *(Owner amended
2026-08-31: the blob was TUI-owned until the codec became Driver-neutral —
`internal/session/transcript.go` — so that any Driver, not the TUI alone, can write and replay a
scrollback.)* The store never decodes either payload;
it wraps them with browsable `Meta` (title, timestamps, workspace, model, user-message count,
last context fill).

**3. Browsing is workspace-scoped.** `Meta.Workspace` records the resolved workspace root; the
`/sessions` browser lists the **current workspace by default** with a toggle (`^a`) to all
workspaces. Resuming a foreign-workspace session is allowed and labelled. Legacy records (no
recorded workspace) appear only under the all-workspaces view.

**4. `/clear` and `/new` close into history and rotate — both verbs KEEP.** Neither verb
deletes. Each does a final Save of the outgoing session (with per-Turn saves it is already on
disk; the final save just captures post-last-turn notes) → `Rotate` (the next Save mints a fresh
id) → the existing reset body. The outgoing session remains in history; **discarding is the
browser's `^d`**, nothing else. The save runs *before* `ClearContext`, so the inherited
snapshot-before-clear ordering falls out; on a `ClearContext` error the view is left untouched
and the completed save is harmless (the session was closing anyway).

**5. Each layer rejects or degrades only its own version.** Three independent schema versions,
three sentinels, each owned at its own boundary — the layer-ownership-of-versions rule. *(Amended
2026-08-31: the wrapper and the scrollback blob are both `internal/session`'s since the codec
became Driver-neutral, and they still version independently of each other.)*

| version | owner | sentinel on a newer-than-this-build payload |
|---|---|---|
| `session.RecordVersion` (the store wrapper) | `internal/session` | `ErrRecordVersion` |
| `session.TranscriptVersion` (the scrollback blob) | `internal/session` | `session.ErrTranscriptVersion` |
| `domain.SessionVersion` (the engine envelope) | the engine | `ErrSessionVersion` |

A layer never reaches into another's payload to version-check it. A future wrapper is refused by
the store; a future transcript blob degrades the TUI to no-replay-plus-a-note; a future engine
envelope is refused on restore — each at its own boundary.

**6. On any restore error the view is left untouched and the failure is noted.** The same rule
`/clear` pins: a fresh-looking view must never lie about the engine. A corrupt or future-version
snapshot leaves the live conversation exactly as it was; a corrupt transcript blob degrades to
"resumed, no scrollback recorded — the model still remembers" over an otherwise-fresh view,
never a fatal error. In-TUI live restore swaps into a *temporary* decoded state and commits only
on full success, so a bad snapshot cannot half-replace a running conversation.

**7. Concurrency is last-write-wins per file, documented, not locked.** Ids are per-instance
unique (a UTC-second stamp plus random suffix), so clobbering requires the *same* session
resumed into two instances. That is documented and accepted; a cross-instance file lock is a
recorded `ISSUES.md` follow-on, not built here.

> **Superseded 2026-09-19 (`apogee-3b3`, plan `2026-09-19 - 01` items 10 and 11).** The lock is
> built: a live session is **held** by the instance that has it open. `session.Store.Hold` takes
> the kernel's advisory lock (`platform.AcquireLock`, the daemon's own mechanism — ADR 0034 D7) on
> `<sessions>/<id>.lock` beside the record; there is no stale state to reap, because the lock dies
> with the process. The hold is taken at the record's *birth* — the first Save, or construction on
> a `--resume`/`--continue` record — never at the mint, so a run that never saves touches no disk;
> it moves with the identity (`Rotate` releases, `Activate` re-holds, `Close` lets go) and is kept,
> not re-taken, across every Save of the id already held. Every door refuses a held record with one
> line, `session <id> is open in another apogee (pid N) — fork it to work alongside` (`(pid N)`
> omitted when the lock file carried none): `--resume` and `--continue` probe-and-release at the
> door so the host takes the one real hold at construction, and `--continue` never skips to the
> workspace's next record; `/sessions` ⏎ and `^d` are refused by the host's `Load` and `Delete`,
> which the browser notes verbatim with the view and the active session untouched. The `/sessions`
> resume parks its hold: `Load` takes it, `Activate` adopts it once the live restore has succeeded,
> and an unadopted one — the restore failed — is released by `Rotate`, `Close`, `Delete` of that
> id or the next `Load`. The hold guards only against *other* instances: a `Load` of the active id
> or of the id already parked neither probes nor parks, and `Activate` of it keeps the live hold.
> `Delete`/`Prune` hold first, remove the JSON, release, and only then unlink `<id>.lock` — the one
> stated exception to `AcquireLock`'s "never removed" rule, benign because the record is already
> gone. `/fork` is the sanctioned way to work alongside a live session: the child is a new record
> nobody holds.
>
> **Note (2026-09-24, `apogee-firing-holds-no-record`).** An unattended Firing holds its record
> too: `run.Once` takes `Store.Hold` on `Spec.RecordID` once the Agent is built and releases it on
> return, whenever the run has both a Store and a record id — the record is born at the run's
> start rather than its first Save, because the run's undo journal is written under that id from
> the first Exchange. A Firing whose id another instance holds is refused with the same
> `*session.HeldError`; a run with no Store or no id takes no hold and touches no disk.
>
> **Amended 2026-10-17 (ADR 0103 D10, D12, D14).** Two doors now watch a held record instead of
> refusing it: `--resume <id>` and the browser's ⏎ open it in Watch mode, which reads the record
> and its Event journal and never loads, holds, saves or deletes it. Rename, delete (`^d`) and
> `--continue` still refuse with the line above. The interactive TUI's hold timing changes: it is
> taken when the first prompt is submitted, before Turn 1, rather than at the first Save (hold at
> the first prompt, never at the mint), so a watcher can find a session before its first Turn
> ends. A session that never sends a prompt still touches no disk. `Store.hold` absorbs a
> probe-length lock contention, so a watcher's probe never makes the owner refuse.

**8. What is deliberately NOT session state.** The record carries the conversation and the
scrollback and nothing else about the live host. **Agent mode, the allow-for-session approval
cache, confinement, and MCP connections are not serialized** — they are live host state,
re-established or re-confirmed on resume:

- Approvals and MCP: re-confirmed / reconnected fresh per
  ADR 0008 — an external effect is
  non-forkable, and a resumed session is a fork across time; the allow-for-session cache is a
  safety decision that must be re-made, not inherited from disk.
- Mode and confinement: host posture chosen at launch, not a property of the conversation.
- **Sub-agent sessions stay ephemeral.** A child's effects live in the parent's conversation and
  transcript (which persist); the child `Session` itself is never a separate record.

> **Amended 2026-09-27 (ADR 0087 D5).** A Workflow item's conversation is saved in its workflow
> folder under the session's Scratch dir, for `/workflows` inspection. It is still never a Session
> record, and a plain `sub_agent` child stays ephemeral.

**9. The in-TUI resume primitive is a live-restore method, not an Agent rebuild.**
`(*Agent).RestoreSession` swaps a snapshot into the **live** Agent at a quiescent boundary, so
tools, Mechanisms, and MCP wiring stand; `(*Agent).InExchange` reports whether a restored
snapshot was captured mid-Exchange, so the host can offer the step-only `/continue` drive that
finishes an interrupted task. `apogee.Resume` (rebuild-at-startup) is unchanged and still serves
`--resume`/`--continue`.
