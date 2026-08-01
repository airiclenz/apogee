---
Status: accepted
---

# Sessions persist per-Turn as dual-representation records

## Context

Snapshot/resume is an engine capability from the start:
[ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md) makes the
loop steppable and snapshottable, [ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)
pins the quiescent boundary a snapshot is valid at, and `domain.Session` is the versioned
`{Version, State}` envelope the engine round-trips. But the *product* had none of the surface
that turns "the engine can resume" into "the user has sessions":

- The store (`internal/session.Store`) was a **write-only, quit-only stub** — one `Save`
  method that wrote a fresh `<UTC-timestamp>.json` bare envelope, no IDs, no `List`/`Load`/
  `Delete`, no update-in-place. The TUI saved **only on a clean quit**, and `--resume <path>`
  read one file directly.
- On resume the engine remembered but **the view started empty** — nothing replayed the
  scrollback, so a resumed session showed a fresh box over a conversation the model still held
  in mind. A fresh-looking view lying about a remembering engine is the exact failure the
  `/clear`-reset work pinned as forbidden.
- A run that died mid-task lost everything since the last quit (i.e. everything), and there
  was no in-TUI way to start a new session or browse old ones — the standing
  `TODO.md` P1 "Session management UI" gap, and the successor the `/clear`-reset plan
  explicitly deferred to.

The oracle is `../apogee-code` (the retired VS Code extension): one `<id>.json` per session,
saved **after every completed turn** (never on quit — in-flight turns were lost), a **dual
representation** on disk (UI entries + provider messages) so resume repaints the scrollback, a
first-user-message title heuristic with inline rename, and a history list with click-to-resume
and delete. Its VS-Code-specific plumbing (webview messages, `globalState`) does not carry
over; the shape of *what persists and when* does.

This ADR records the design the owner ratified 2026-07-24. It **extends ADR 0001** (the
live-restore variant of the snapshot/resume feature) and **supersedes nothing**.

## Decision

**1. Save cadence is per-Turn, asynchronous, and soft on failure.** The worker snapshots the
engine between Steps — at each `StatusTurnComplete` quiescent boundary, the only point a
snapshot is valid ([ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)) — and the Model
persists asynchronously through a single-flight, latest-wins save so a burst of Turns collapses
to one in-flight write plus one pending. A crash therefore loses **at most one Turn**, never the
whole session (the owner chose maximum crash-safety over the cheaper per-Exchange cadence). A
save failure **never interrupts the conversation**: it is noted once on the ok→fail transition
(and recovery noted on fail→ok) and the run continues — parity with apogee-code's swallowed
`session_save_failed`. Empty sessions (nothing past the start-up box) are never written.

The ordering that makes an async per-Turn save safe is load-bearing and is stated in the
worker's doc comment: every event of a Turn is delivered to the Model *synchronously inside*
the Step, so a snapshot the worker sends *after* the Step returns is ordered strictly after that
Turn's events — the transcript the Model holds when the snapshot folds in is consistent with it.

**2. Persistence is dual-representation.** The on-disk `Record` wraps **two opaque payloads**
beside its metadata: the untouched engine `domain.Session` envelope **and** the TUI's own
versioned scrollback blob. Resume repaints the scrollback exactly — sub-agent `Depth`, tool
cards, notes, presented documents — instead of showing a bare box over a remembering engine. The
transcript blob is **TUI-owned and versioned independently**, opaque to the store, mirroring how
`Session.State` is engine-owned and opaque to `domain`. The store never decodes either payload;
it wraps them with browsable `Meta` (title, timestamps, workspace, model, user-message count,
last context fill).

**3. Browsing is workspace-scoped.** `Meta.Workspace` records the resolved workspace root; the
`/sessions` browser lists the **current workspace by default** with a toggle (`a`) to all
workspaces. Resuming a foreign-workspace session is allowed and labelled. Legacy records (no
recorded workspace) appear only under the all-workspaces view.

**4. `/clear` and `/new` close into history and rotate — both verbs KEEP.** Neither verb
deletes. Each does a final Save of the outgoing session (with per-Turn saves it is already on
disk; the final save just captures post-last-turn notes) → `Rotate` (the next Save mints a fresh
id) → the existing reset body. The outgoing session remains in history; **discarding is the
browser's `d`**, nothing else. The save runs *before* `ClearContext`, so the inherited
snapshot-before-clear ordering falls out; on a `ClearContext` error the view is left untouched
and the completed save is harmless (the session was closing anyway).

**5. Each layer rejects or degrades only its own version.** Three independent schema versions,
three owners, three sentinels — the layer-ownership-of-versions rule:

| version | owner | sentinel on a newer-than-this-build payload |
|---|---|---|
| `session.RecordVersion` (the store wrapper) | `internal/session` | `ErrRecordVersion` |
| the transcript blob's version | `internal/tui` | `ErrTranscriptVersion` |
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
recorded `TODO.md` follow-on, not built here.

**8. What is deliberately NOT session state.** The record carries the conversation and the
scrollback and nothing else about the live host. **Agent mode, the allow-for-session approval
cache, confinement, and MCP connections are not serialized** — they are live host state,
re-established or re-confirmed on resume:

- Approvals and MCP: re-confirmed / reconnected fresh per
  [ADR 0008](0008-stateless-tools-and-non-forkable-external-effects.md) — an external effect is
  non-forkable, and a resumed session is a fork across time; the allow-for-session cache is a
  safety decision that must be re-made, not inherited from disk.
- Mode and confinement: host posture chosen at launch, not a property of the conversation.
- **Sub-agent sessions stay ephemeral.** A child's effects live in the parent's conversation and
  transcript (which persist); the child `Session` itself is never a separate record.

**9. The in-TUI resume primitive is a live-restore method, not an Agent rebuild.**
`(*Agent).RestoreSession` swaps a snapshot into the **live** Agent at a quiescent boundary, so
tools, Mechanisms, and MCP wiring stand; `(*Agent).InExchange` reports whether a restored
snapshot was captured mid-Exchange, so the host can offer the step-only `/continue` drive that
finishes an interrupted task. `apogee.Resume` (rebuild-at-startup) is unchanged and still serves
`--resume`/`--continue`.

## Considered options

- **Keep saving only on quit.** Rejected: a crash or `kill -9` mid-task loses the whole session,
  and "sessions" that only exist after a clean exit are not sessions. Per-Turn is what makes the
  interrupted-task `/continue` pay off.
- **Persist the engine envelope alone (no transcript).** Rejected: resume would leave the view
  empty over a remembering engine — the forbidden "fresh view lying about the engine" — so the
  scrollback must persist too. Re-deriving the scrollback from the conversation was rejected as a
  second renderer that would inevitably drift from the real one.
- **One shared schema version for the whole record.** Rejected: it couples three layers that
  ship and evolve independently. The engine envelope is already `domain`-owned and opaque to the
  store; forcing the store to bump when the TUI changes its scrollback shape (or vice versa) is
  exactly the coupling ADR 0010's layering forbids.
- **Fork-on-resume (a resumed session writes a new file).** Rejected: with per-Turn saves it is
  silent data sprawl — every resume spawns a duplicate that then diverges. Resume continues the
  session *in place*; `/clear`/`/new` is the explicit "start a fresh one".
- **A cross-instance file lock now.** Deferred: ids are per-instance unique so the only clobber
  is the same session opened twice, a narrow case. Documented as last-write-wins with a recorded
  TODO rather than shipping a lock nobody has yet needed.
- **LLM-generated titles.** Rejected for v1: the first-user-message heuristic (≤50 chars,
  word-boundary truncate) costs no tokens and is renameable inline. An LLM title is an additive
  follow-on, not a dependency of the feature.

### Addendum (2026-07-31) — LLM titles adopted as a cosmetic out-of-band call

The **LLM-generated titles** rejection immediately above is **reversed**, ratified with the owner
in the 2026-07-31 grill session. Nothing in the Decision changes: the first-user-message heuristic
still stamps the first Save and remains the fallback, and the record's shape, save cadence, and
per-layer versioning are untouched. What is added is a *naming call* that renames a record once,
shortly after it is born.

- **It is a cosmetic call — a category of its own.** The naming completion is neither a
  **Mechanism** (it fires at no Hook point and never shapes the primary call, so it is exempt from
  Bypass reasoning and from the [ADR 0009](0009-the-ab-decision-rule.md) non-inferiority gate) nor
  structural like compaction (nothing breaks without it — the heuristic title stands). It is
  **not a Turn**: it emits no `TokenEvent` or `UsageEvent`, never enters the transcript, and
  never moves the context gauge. It lives entirely in the TUI and the composition root, so the
  **bench and embedder path is untouched** — anything that does not construct `tui.Options`
  cannot fire it
  ([ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)).
- **It fires at first-prompt submit, in parallel with the main call.** Under the single-slot
  server posture
  ([ADR 0024](0024-the-heartbeat-observes-upstream-and-rebind-applies-at-the-boundary.md)) it
  therefore queues behind Turn 1 and runs between Turns 1 and 2 — the cheapest KV-eviction point
  in the session, because context is at its smallest there. Its request timeout is correspondingly
  generous: waiting out the whole first stream is the expected case, not a fault.
- **It never goes through the Agent.** The Agent is single-goroutine
  ([ADR 0011](0011-tui-is-a-thin-renderer-over-a-worker-goroutine-engine.md)), so the call rides a
  **nil-able func seam on `tui.Options`** backed by its own out-of-band `provider.Client` built
  from the session's *current* server + model binding — the `probe.Chat` pattern. There is no
  separate naming endpoint or naming model: a session names itself with the model it is running. A
  nil seam means automatic naming never fires and the on-demand form reports unavailability —
  never an error.
- **`Rename` is the only writer.** `Save` ignores its title argument after the first call, so a
  generated title lands through the existing `Rename` path; a result that arrives before the first
  Save has minted an id is **stashed and applied at the first save-complete**.
- **A user's title always wins (never-clobber).** Any user-initiated rename (the browser's `r`,
  or `/rename <text>`) marks the title as touched, and a late-landing *automatic* title is then
  dropped. An explicitly requested regeneration (bare `/rename`) is the exception — the user
  asked for it, so it applies and leaves the mark set. Naming fires once per new Session record,
  including after the `/clear` / `/new` rotation of Decision 4, and never on a resumed session.
- **The two naming forms read different amounts of the session** (added 2026-08-01). The automatic
  call reads the first prompt — the only one that exists when it fires — while an explicitly
  requested regeneration (bare `/rename`) reads a bounded, budget-capped window of the user side
  of the transcript: the opening request plus the most recent, filled inward from the newest. That
  form is instructed to name the **dominant thread, biased recent**, because a session that moved
  on must be findable by what it moved to.
- **The gate is a config key, default on.** `auto-title` (flat, config-file only, nil ⇒ true)
  gates only the *automatic* firing; the seam stays wired so `/rename` regenerates on demand even
  when it is off. The automatic path fails **silently** to the heuristic title — a cosmetic
  maintenance nicety must never nag.

## Consequences

- **`internal/session` grows from a stub into the storage layer**: id-addressed `Record`s with
  `List`/`Load`/`LoadPath`/`Delete`/`Rename`, atomic writes (temp file + rename), update-in-place,
  and a **legacy sniff** that wraps pre-plan bare-envelope files so old `~/.apogee/sessions/`
  files still list, load, and resume (workspace-unknown, no scrollback).
- **The engine's public surface gains `RestoreSession` and `InExchange`** — the live-restore
  variant of the ADR 0001 snapshot/resume feature. Because `apogee.Agent` is a type alias for
  `agent.Agent`, they are public with no facade delegator.
- **A fourth persisted surface's story is now settled** alongside config, library, validated, and
  probe records under `~/.apogee/` — owner-private (`0o700`/`0o600`), versioned, soft on every
  defect, individually deletable.
- **The user gets continuous autosave, a `/sessions` browser** (list · relative time · N msgs,
  resume/delete/rename, workspace toggle), **`--continue`** (most-recent in this workspace) and
  **`--resume <id-or-path>`**, **scrollback replay on resume**, and **interrupted-task
  `/continue`** — closing the P1 "Session management UI" parity gap.
- **Retention/pruning and a cross-instance lock are recorded TODOs**, deliberately out of this
  plan. There is no auto-pruning: manual `d` only.
- **The bench is untouched.** `session.Store`'s new API stays embeddable and the bench keeps
  composing `Snapshot`/`Encode` directly (ADR 0001) — no bench code depends on the store.
