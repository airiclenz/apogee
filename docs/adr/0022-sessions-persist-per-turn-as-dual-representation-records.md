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
`session_save_failed`. Empty sessions are never written — a session is saved only once its
transcript holds a committed **user prompt** (sharpened from "an entry the record would actually
keep" by the second 2026-08-01 addendum below; neither the start-up box nor an ephemeral notice
ever qualified under either reading).

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
recorded `ISSUES.md` follow-on, not built here.

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
- **A user's title always wins (never-clobber).** Any user-initiated rename (the browser's `^r`,
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
  plan. There is no auto-pruning: manual `^d` only.
- **The bench is untouched.** `session.Store`'s new API stays embeddable and the bench keeps
  composing `Snapshot`/`Encode` directly (ADR 0001) — no bench code depends on the store.

## Addendum (2026-08-01) — the scrollback splits into persisted and EPHEMERAL entries

Decision 2 says resume "repaints the scrollback exactly … sub-agent `Depth`, tool cards, notes,
presented documents". That was read as *every* entry persists, and it produced a real defect
(`ISSUES.md`): the `resumed: <title>` notice was itself a persisted note, so a record collected
another copy of it on every reopen — five resumes, five stored `resumed:` lines — and the
`context: …` notice did the same at every startup, `/clear` and resume. Decision 2 is unchanged in
substance; what is added is the distinction it left implicit.

- **What qualifies as ephemeral: any entry RE-DERIVED at startup or resume time.** The test is
  whether the entry is recomputed from live state each time the view is rebuilt, not whether it
  looks incidental. A notice derived from the record being opened (`resumed: <title>`, its
  no-scrollback degrade variant, the interrupted-mid-exchange note) or from the workspace as it is
  right now (the `context: …` files notice — session-scoped prompt data re-read on restore, per
  [ADR 0026](0026-workspace-context-files-are-session-scoped-prompt-data.md)) adds nothing on the
  way back in and accumulates a duplicate on the way out. An entry that records something that
  actually *happened* in the session — a cancellation, a failed save, a server switch — is earned
  by the conversation and stays persisted.
- **The mechanism generalizes the `entryStartup` precedent rather than minting a kind.** The
  start-up box was already excluded from persistence by being absent from `entryKindNames`; the
  general form is an `entry.ephemeral` flag that `encodeTranscript` skips, with kind, styling and
  position in the scrollback untouched. Only persistence differs, so rendering and the save gate
  need no per-kind duplication — the gate (`hasPrompt`, per the second 2026-08-01 addendum below)
  agrees with the blob a fortiori: a scrollback of nothing but ephemeral notices holds no prompt,
  so it is still an empty session. A `context: …` notice in a repo that has context files, or a
  degrade note over a blob that would not decode, does not on its own turn a quit into a stored
  record holding an empty scrollback.
- **The wire format does not move.** Skipping is **encode-side only** — nothing ephemeral ever
  reaches the blob, so decode needs no counterpart, no kind name is added, and the transcript
  blob's version under Decision 5 is *not* bumped. An older record keeps decoding identically, and
  a record written by this build is readable by an older one.
- **Decision 6's degrade wording is now emitted ephemerally.** "resumed, no scrollback recorded —
  the model still remembers" is still shown on a corrupt or future-version transcript blob, exactly
  as specified; it is simply not written back into the record, which is the point — a record that
  failed to decode must not accumulate notes about having failed to decode.

## Addendum (2026-08-01, second) — the save gate is "a prompt was sent"

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

## Addendum (2026-08-02) — the single flight covers every record write, not only saves

Decision 1's "a crash loses **at most one Turn**" was not true under the default config. The
single-flight latch covered `Save` alone, but a session record has three writers, not one: the
per-Turn `Save` replaces the record wholesale, `Store.Rename` (the only writer of a stored title —
the Addendum of 2026-07-31 put an automatic naming call on that path, on by default) is a
read-modify-write of the *whole* record, and the browser's `Delete` removes it. Nothing serialized
them against each other, and `saveComplete` in fact `tea.Batch`ed the title flush beside the
coalesced save, whose members run on separate goroutines. A rename that read the record before a
save replaced it wrote the pre-save version back — reverting engine state and scrollback together
by a whole Turn. A probe over `internal/session.Store` lost the newer payload in 25% of runs
(audit 2026-08-01). Nothing about the cadence, the record shape or the failure posture moves.

- **One queue for all three writes.** The TUI's latch is now "a record write is in flight", and
  `Save`/`Rename`/`Delete` all queue behind it. Saves still coalesce latest-wins, so Decision 1's
  "one in-flight write plus one pending" is unchanged for the save path; renames and deletes keep
  their order, being distinct instructions rather than restatements of one. A fold may never batch
  two record writes.
- **The store serializes the same three as a floor.** `internal/session.Store` holds a mutex across
  `Save`, `Delete` and — crucially — the whole of `Rename`'s read-modify-write, so any caller that
  does not come through the fold still cannot interleave. Readers stay unguarded: the atomic write
  means a reader sees a whole record either way. Which layer *owns* serialization long term is
  deliberately still open (roadmap C7); today the fold owns ordering and the store owns atomicity.
- **A title that could not be written is retried, not dropped.** The apply path branches on
  `ActiveID()`, which the host mints at the *start* of the first `Save`, before the atomic write
  lands — so a title answering in that window renamed a record that did not exist yet and was
  discarded silently. It is now put back on the pending-title stash and applied at the next
  successful save, under the same never-clobber rule the stash always obeyed.

## Addendum (2026-08-02, second) — the closing flushes join the queue, and the exit waits for it

The addendum above put every record write on one queue and left one caller outside it: the
synchronous closing flush a clean quit and `/clear`|`/new` shared (`saveSession`, now folded into
`saveAtIdle`). It wrote the record straight from the `Update` goroutine, so two windows survived the
single flight. First, the host reads its active session — id, `CreatedAt`, **title** — at the top of
`Save`, and `Rename` mirrors a new title onto that identity only *after* its store write returns; a
flush landing between the two wrote the pre-rename title back over the name the human had just
chosen. Second, `Rotate` ran straight through beside it, so a save still in flight (or waiting) when
`/clear` landed reached an already-inactive host and minted a **second id** for the outgoing
conversation — a duplicate record the fresh session then kept updating as its own. Decision 1's
cadence, Decision 4's "final Save → `Rotate` → reset" order, and the record shape are all unchanged;
only the mechanism by which that order is guaranteed moves.

- **The flush is a queued save like any other.** Both closing paths now schedule through
  `saveAtIdle` instead of calling the host, so a flush can never overlap an in-flight `Rename` or
  `Delete`, and a stale pending save is superseded by it under the same latest-wins coalescing.
- **`Rotate` is the queue's fourth write kind.** It writes no file, but it retires the id every
  save resolves against, so ordering against that one stream is the whole of what it needs: queued
  behind its own flush, it can no longer overtake it. Decision 4's "no rotate on a `ClearContext`
  error" is unchanged — nothing is queued on that path.
- **A clean quit defers its exit to the drain.** With the flush asynchronous, `tea.Quit` moves to
  the fold that finds the queue empty (the `quitting` latch ADR 0011's C4 deferral already used for
  a busy quit, now also armed at idle). Writes asked for on the way out therefore land before the
  program exits, where before they were abandoned mid-flight. A quit with nothing to flush and an
  idle queue still exits on the keypress.

## Addendum (2026-08-02, third) — the queue orders WHICH record, not only what is written to it

Two more `SessionHost` calls were still made straight from the `Update` goroutine, both from the
`/sessions` overlay: the `Rotate` that retires the live conversation's id when the human deletes its
file, and the `Activate` a resume uses to adopt the loaded record. Neither writes a file, which is
why both were read as "not a record write" — but both move **which record every later `Save`
resolves against**, so running them outside the queue reordered the stream exactly as the closing
`Rotate` did before the addendum above. Deleting the active session while a save was in flight
retired the id under that save, so it landed as a **second record** for the live conversation and the
delete then removed the wrong one of the two. A resume applied its `Activate` ahead of everything
queued, so the outgoing conversation's coalesced save was written into the **record just resumed** —
the loaded session's transcript and engine state replaced by the conversation the human was leaving
(audit 2026-08-01, action item 1). The seam is unchanged: same calls, same preconditions, same
"activate only after a confirmed restore" rule; only where they are issued from moves.

- **Retargets are queued writes.** `Rotate` and `Activate` are the queue's two *retargeting* kinds.
  The browser's active-record delete queues its rotate ahead of its own delete, and `resumeLoaded`
  queues its activate, so everything already pending against the outgoing record lands first. The
  view does not wait on either: a resume repaints from the record already in hand, so the human sees
  the resumed session immediately while the redirect settles behind them.
- **Saves do not coalesce across a retarget.** Latest-wins was previously "supersede the one save
  in the queue", which is right only while every queued save describes the same record. A save
  queued before a retarget and one queued after it describe different records, so coalescing across
  the line would write the incoming conversation into the outgoing record *and* lose the outgoing
  one's final state. Coalescing is now confined to the queue's last segment — the whole queue
  whenever no retarget is waiting, which is the ordinary case and is unchanged.

## Addendum (2026-08-10) — a committed tool result carries its own verdict, and it rides the snapshot

*(Ratified 2026-08-02 with the fix; recorded here from the amendment in the archived
`docs/design/archived/hook-mutation-api.md` §5, whose design draft is no longer a live document.)*

**The loss was at the commit seam, not on the wire.** `ToolResult.IsError` is authoritative on the
**live** seam — the tool reports it, and `PostToolResult` receives the `*ToolResult` — but the commit
into history copied only `Content`. So every **cross-Turn** Mechanism asking "did that earlier call
fail?" had nothing to read but the text, and fell back to the string matching apogee-sim was forced
into because a proxy only ever saw text. Over a successful `read_file` that text is *the file*.
`read_loop` therefore classified any source file containing `error:` or "does not exist" as a failed
read, and told the model to write the file it had just finished reading — a persisted record that
was honest about the bytes and wrong about what happened.

**The verdict is a field, stamped once.** The committed message carries
`domain.Message.ToolOutcome`, a tri-state — `""` unrecorded / `"ok"` / `"error"` — projected from
`IsError` by `domain.ToolOutcomeOf` at the **one** seam every tool result crosses into history
(`internal/agent` `appendToolResult`), so no route commits an unmarked result: a plain call, a
refusal, a gate denial, a sub-agent delegation. Classification of *what kind* of error (syntax,
import, missing file) stays mechanism-internal and is not a field on the type.

**It persists as an `omitempty` sibling, and needs no version bump.** `tool_outcome` rides the
engine envelope beside `interjected`, on the same terms: it is Apogee-owned and process-local (the
wire projection maps fields explicitly, so the marker never reaches a provider), and `omitempty`
keeps it off every non-tool message. That is what keeps `domain.SessionVersion` where it is under
decision 5's layer-ownership rule — an older snapshot simply lacks the key and decodes as
`ToolOutcomeUnrecorded`, and an older binary round-trips the key as an unknown sibling rather than
dropping it.

**Unrecorded is a route, not a gap.** A record snapshotted before the marker existed still resumes
into Mechanisms that must judge it, so the text sniffing survives — but only as the fallback that
`ToolOutcomeUnrecorded` selects, and only anchored to the result's **first line**, which is where a
tool's error text lives and where a file's contents do not answer for it. A restored pre-marker
session therefore degrades to the old heuristic on its old messages while every result committed
after the restore carries the real verdict, which is the same per-payload posture decision 5 takes
everywhere else: each layer reads what it owns, and an older payload degrades rather than lying.

## Addendum (2026-08-25) — a delegating Turn is progress-saved; the engine half keeps the boundary

**The gap: one Turn is not one bounded unit of loss.** A `sub_agent` call runs synchronously inside
the Turn that issued it (`internal/tools/sub_agent.go`, dispatched from `internal/agent/loop.go`),
so a Turn holding a delegation stays open for as long as the child runs — 15+ minutes in the
session that surfaced this (`20260825T164640Z-ca180f38`), and a whole fan-out batch under
[ADR 0039](0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md).
For that entire time the record on disk ended at the *previous* tool call: the assistant message
that delegated, the prompt it carried and every child event were nowhere on disk while the TUI
painted the run. A reader of the record mid-run — a second session, a reviewer, `apogee headless`
tooling — concluded that no sub-agent had been dispatched at all. Decision 1's "a crash loses at
most one Turn" held as written and still bounded nothing useful: for such a Turn it is unbounded in
wall-clock and in tokens.

**The rule: the TUI re-persists mid-Turn, pairing the last boundary snapshot with the live
transcript.** A **progress save** fires on the depth-0 `sub_agent` `ToolCallEvent` (the delegation
being issued), on every `ToolResultEvent` at depth ≥ 1 (a child crossing a tool boundary), and on
every `SubAgentPhaseEvent` reporting `SubAgentFinished`. Its engine half is not freshly taken: the
Model caches the last **quiescent-boundary** snapshot — the idle `Snapshot()` taken immediately
before each worker launch (after any `AbortExchange`, before `Submit`, so it can never carry
`pendingInput` the TUI cannot resume), refreshed by each Turn's own `turnSnapshotMsg` and by a
restored record's payload, dropped when `/clear` rotates the session. Its transcript half is
**live**. Bursts collapse in the existing single-flight latest-wins write queue, so the cadence
costs at most one in-flight write plus one pending, exactly as decision 1's saves do. Delegations
only: a long *leaf* tool (`terminal`, `console_read`) keeps the per-Turn behaviour — generalising is
a later one-predicate change, not a decision taken here.

**What does not move.** [ADR 0007](0007-step-turn-and-the-quiescent-boundary.md)'s boundary rule is
untouched: no snapshot is ever taken inside a Step, and the engine half of a progress-saved record
is by construction one that was taken at a quiescent boundary, so the record's engine half never
changes meaning. Decision 1's cadence for that engine half is unchanged — it still advances only
per-Turn; only *when the transcript half may be written* moves. The record shape and all three
schema versions stand (`RecordVersion`, the transcript blob's version, `domain.SessionVersion`):
an open `sub_agent` head in the blob **is** the in-flight marker, so no `Meta` flag and no browser
column were added. Decision 8's non-goal stands: the child's own `Session` is still never a record.

**Resume semantics: a progress-saved Turn re-attempts, exactly as a cancelled one does.** A record
written mid-delegation holds an open Turn, and restoring it is the same act as restoring a
cancelled Turn — the engine rolls back to the pre-request boundary and `/continue` re-runs the Step
that started the delegation, while sending a new message discards it. Because the transcript half
may now hold calls that were live at write time and are dead at read time, replay closes **every**
tool-call entry still open as *interrupted* and adds one note saying the unfinished work was not
kept. That rule also repairs the records cancelled Turns were already leaving behind, and it is a
replay-time rule only: the live paint path is unchanged, so a running delegation still paints as
running while it runs.

**The bound, as now stated.** A crash loses at most one Turn of **engine** state; of the
**scrollback** it loses at most the work since the last child tool boundary.

### Considered options

- **Progress-save from a cached boundary snapshot (chosen).** Costs no engine change and no schema
  change, and the record's two halves keep their existing meanings — the engine half is a boundary
  snapshot, the transcript half is what the human saw.
- **Accept the bound and document it only.** Rejected: it leaves a reader of the record mid-run
  concluding that no delegation was issued, which is the actual harm — the wall-clock bound was
  only how we found it.
- **An engine Step boundary at the delegation (nested stepping).** Rejected *here*, not denied: it
  would supersede part of ADR 0007, reach into ADR 0039's fan-out and change what a bench arm
  compares, so it needs its own grill rather than a clause in this addendum. ADR 0007's
  "the snapshot schema leaves room for a suspended sub-agent" is the door it would come through.
