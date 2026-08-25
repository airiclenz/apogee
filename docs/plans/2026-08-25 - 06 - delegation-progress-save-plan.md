# Delegation progress save — the record shows a running delegation

**Goal:** a session record no longer stalls at the previous tool call while a `sub_agent`
delegation runs. The TUI re-persists the record mid-Turn — the last *quiescent-boundary* engine
snapshot paired with the *live* transcript — when a delegation is issued and as its child crosses
tool boundaries, so a reader of the record mid-run (a second session, a reviewer, `apogee headless`
tooling) sees the assistant message that delegated, the prompt it carried and the child's progress.
The engine is untouched: no snapshot is ever taken inside a Step, and a resumed record re-attempts
the delegating Turn exactly as a cancelled one does.

**Date:** 2026-08-25
**Status:** ready — unexecuted
**Sized for:** ~200k-context host
**Source defect:** `ISSUES.md` § *A running delegation — and the whole Turn around it — is absent
from the session record until the Turn ends* (observed 2026-08-25 in session
`20260825T164640Z-ca180f38`).

**Authoritative sources (precedence: these over any item text that disagrees):**

- [ADR 0007](../adr/0007-step-turn-and-the-quiescent-boundary.md) — a snapshot is valid only at the
  quiescent boundary between Steps; a cancel rolls the whole Turn back to its pre-request boundary.
  **Not amended by this plan.**
- [ADR 0022](../adr/0022-sessions-persist-per-turn-as-dual-representation-records.md) — decisions 1
  (per-Turn save cadence, "a crash loses at most one Turn"), 2 (dual representation: engine envelope
  + TUI-owned transcript blob), 6 (restore leaves the view honest), 8 (a child Session is never a
  record). Amended by item 4 with a dated addendum.
- [ADR 0039](../adr/0039-delegations-fan-out-concurrently-bounded-by-the-servers-parallel-agents-cap.md)
  decision 5 — child events carry the spawning call-ID (`domain.EventBase.CallID`, empty at depth 0).
- Code seams: `internal/tui/worker.go:162-166` (the only save trigger today, `turnSnapshotMsg` after
  `StatusTurnComplete`); `internal/tui/model.go:856` (its fold → `persist`);
  `internal/tui/sessionsave.go:34-97` (`savePayload`, `snapshotPayload`, `persist`, `saveAtIdle`) and
  the single-flight record-write queue below it; `internal/tui/model.go:777` (`eventMsg` fold);
  `internal/tui/fold.go:33` (`foldEvent`); `internal/tui/transcriptcodec.go:496-500`
  (`fromWireEntry` — the firing `!done` replayed-closed precedent) and `internal/tui/schedule.go:459`
  (`closeInterruptedFiring`); `internal/tui/model.go:581` (`replayScrollback`, `interruptedNote` at
  `internal/tui/sessions.go:91`); `internal/agent/loop.go:222-226` (the assistant tool-call message
  is committed, then `dispatchTools` runs children inline — the whole reason the Turn stays open).

**Ratified design calls (owner, 2026-08-25, via AskUserQuestion during plan writing):**

1. **Shape — progress save, engine boundary unchanged.** No engine change; ADR 0007 stands. The
   Model caches the last boundary `domain.Session` and re-persists it with a fresh transcript blob.
   Rejected: "accept the bound, document only" (second-session readers still see nothing) and "an
   engine Step boundary at delegation / nested stepping" (supersedes part of ADR 0007, touches
   ADR 0039 and bench comparability — its own grill if ever wanted).
2. **Cadence — issue + every child tool boundary.** A progress save fires on the depth-0
   `sub_agent` `ToolCallEvent`, on every `ToolResultEvent` at depth ≥ 1, and on every
   `SubAgentPhaseEvent` with `Phase == SubAgentFinished`. No timer; the existing single-flight
   latest-wins queue collapses bursts.
3. **Scope — delegations only.** A long leaf tool (`terminal`, `console_read`) keeps today's
   behaviour. Generalising is a later one-predicate change, not this plan.
4. **First Turn — idle snapshot before every worker launch.** The Model takes `m.eng.Snapshot()` at
   idle immediately before each worker launch (after any `AbortExchange`) and caches it;
   `turnSnapshotMsg` refreshes the cache. A delegation in a session's first Turn therefore pairs
   with the pre-prompt engine state. A post-Submit snapshot is never persisted — it would carry
   `pendingInput`, which the TUI cannot resume (`Submit` refuses with `ErrInputPending`,
   `/continue` refuses because `InExchange` is false).
5. **Replay — close every open call as interrupted, plus one note.** At replay, any tool-call entry
   still `!done` is closed with an interrupted summary that `failedSummary` recognises, and
   `replayScrollback` adds one ephemeral note when it did so. Mirrors the firing rule; also covers
   today's cancelled-Turn records that carry open calls.

**Write-time calls (plan author, 2026-08-25 — mechanical consequences of the above):**

- ADR 0022 gets a dated addendum, not a new ADR: cadence, record shape and versions are unchanged;
  only *when* the transcript half may be written moves.
- No `session.Meta` field, no `RecordVersion` / transcript-blob-version bump: an open sub_agent head
  in the blob IS the in-flight marker, and the replay rule derives "interrupted" from it. The
  browser needs no new column.
- The engine snapshot paired with a progress save is by construction one the worker (or the Model
  at idle) took at a quiescent boundary — the record's engine half never changes meaning.

**Standing requirements:**

- `skills: coding-standards` (forwarded by default).
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- The Bubble Tea `Model` is copied by value on every `Update` (ADR 0011): the cached snapshot is a
  plain `domain.Session` value on the Model — never a pointer to shared mutable state, never a
  `strings.Builder`.
- No version identifier changes (see the closing note).

**Out of scope (deliberately):**

- Any engine change: `Step`, `Snapshot`, `dispatchTools`, the fan-out pool, `agentState`.
- Persisting a child's own Session (ADR 0022 §8 non-goal stands).
- A `Meta` in-flight flag or a browser marker for "running".
- Progress saves for leaf tools (call 3).
- The sibling ISSUES.md defect *Resuming a stored session leaves the outgoing conversation's
  Consoles running* — a different door; not touched.
- `internal/run` / headless / daemon Drivers: they compose `Snapshot`/`Encode` themselves
  (ADR 0001) and are unaffected.

---

## 1. Cache the boundary snapshot on the Model and add the progress-save entry — ✅ DONE (2026-08-25)

NOTES (2026-08-25): the `boundary`/`hasBoundary` field pair is declared on the `Model` struct in
`internal/tui/model.go` (where the struct lives) rather than in `internal/tui/sessionsave.go`; the
methods that own it (`cacheBoundary`, `cacheBoundaryAtIdle`, `progressSave`) are in
`sessionsave.go` as the item asks, and both files are in the item's Files list.

NOTES (2026-08-25): the item names three worker launches in `commandrun.go` but its line anchors
(`:83`, `:246`, `:262`) point at the prompt path and BOTH `/continue` launches (the resume and the
canned turn), while its third label names `/compact` (`:374`). All four launches now cache the
boundary, which satisfies both the anchors and the labels and matches the item's rule ("every point
the engine is at a quiescent boundary the Model can see"). A `/compact` worker can never trigger a
progress save (it drives no tools), so the extra site is inert until a later Turn re-caches.

**What:** give the TUI a second save entry that needs no fresh engine snapshot.

- In `internal/tui/sessionsave.go` add to `Model` a `boundary domain.Session` value plus a
  `hasBoundary bool` (one field pair; a zero `domain.Session` is a legal envelope, so the bool is
  the presence flag). Add `(m *Model) cacheBoundary(sess domain.Session)` (the single writer) and
  `(m *Model) progressSave() tea.Cmd`, which returns `m.persist(m.boundary)` when `hasBoundary` is
  set and `nil` otherwise. `persist`'s existing gates (wired host, `hasPrompt`) stay the whole gate —
  do not duplicate them.
- Populate the cache at every point the engine is at a quiescent boundary the Model can see:
  - `internal/tui/model.go:856` — the `turnSnapshotMsg` fold calls `m.cacheBoundary(msg.Sess)`
    before `m.persist(msg.Sess)`.
  - Immediately before each of the three worker launches in `internal/tui/commandrun.go` (`:83`
    the prompt path, `:246` `/continue`, `:262` `/compact`), take `m.eng.Snapshot()` at idle and
    cache it on success; on error leave the cache as it was (a progress save then pairs with the
    previous boundary, which is still a valid one). The snapshot is taken AFTER any
    `AbortExchange` the launch path performs (`internal/tui/model.go:1456` scraps a stale open
    Exchange before Submit) so it reflects the state the launch lands on, and BEFORE `Submit` so it
    never carries `pendingInput` (ratified call 4). Add one helper the three sites share rather than
    three inline snapshots.
  - After a successful in-TUI restore (`internal/tui/sessions.go:549`'s fold): cache the restored
    record's engine payload (`msg.rec.Session` or an idle `m.eng.Snapshot()` — the former, since it
    is already in hand and identical).
- Clear the cache (`hasBoundary = false`) in `startNewSession` (`internal/tui/commandrun.go:122`)
  — a stale boundary must never be paired with a rotated session's transcript.
- Doc comments: `progressSave` states the pairing rule in one paragraph — the engine half is the
  last boundary snapshot, the transcript half is live, a resume re-attempts the open Turn as a
  cancel does (ADR 0007), and the reason no snapshot is ever taken mid-Step.

**Files:** `internal/tui/sessionsave.go`, `internal/tui/model.go`, `internal/tui/commandrun.go`,
`internal/tui/sessions.go`, `internal/tui/sessionsave_test.go` (new)

**Tests** (`internal/tui/sessionsave_test.go`, new — table-driven where two cases share a shape):

- `progressSave` with no cached boundary schedules nothing.
- `progressSave` after a `turnSnapshotMsg` fold schedules a save whose `savePayload.sess` is that
  message's `Sess` and whose transcript blob decodes to the CURRENT entries (add an entry after the
  fold, then call it).
- A launch through the prompt path caches an idle snapshot: with a fake `Engine` whose `Snapshot`
  returns a marker Session, the cache holds it before the worker Cmd is returned, and the fake's
  `Submit` is observed to run AFTER `Snapshot`.
- `startNewSession` clears the cache; a restore fold sets it to the restored record's payload.

**Acceptance:**

```
go build ./internal/tui/ && go vet ./internal/tui/
go test ./internal/tui/ -run 'ProgressSave|CacheBoundary|BoundarySnapshot' -count=1
go test ./internal/tui/ -run 'TestModelNoBuilderByValue|Session|AutoTitle' -count=1
```

**Commit:** `feat(tui): cache the boundary snapshot and add a progress-save entry that pairs it with the live transcript`

---

## 2. Fire the progress save at delegation boundaries — ✅ DONE (2026-08-25)

Depends on item 1.

NOTES (2026-08-25): the predicate's per-variant answers are registered as a `wantProgressSave` field
on `fold_test.go`'s existing `foldCase` table rather than in a table of its own, which is what keeps
`TestFoldEventCoversEveryEventVariant` holding the new switch to the same standard as the folds (the
item's "register the predicate's answers the way the other folds do"). The three discriminating
rows the item names — a depth-0 `sub_agent` call, a depth-1 `ToolResultEvent`, the finished
`SubAgentPhase` — were added to that table beside their existing depth-0 / started kin, and
`TestProgressSaveTriggerAnswersEveryVariant` runs it through the predicate.

NOTES (2026-08-25): the cadence test also folds the CHILD's own `ToolCallEvent` (depth 1) and
asserts it schedules nothing — not named by the item, but it is the event that sits between the two
triggers in a real stream, so pinning it proves the depth-0 arm of the `ToolCallEvent` rule.

**What:** wire the cadence of ratified call 2 into the event fold.

- In `internal/tui/fold.go` add a pure predicate `progressSaveTrigger(e domain.Event) bool` that is
  true for exactly: a `domain.ToolCallEvent` with `Depth == 0` and `Call.Tool == subAgentToolName`
  (the constant in `internal/tui/subagentblock.go:15` — reuse, do not re-declare); a
  `domain.ToolResultEvent` with `Depth >= 1`; a `domain.SubAgentPhaseEvent` with
  `Phase == domain.SubAgentFinished`. Every other variant, and a depth-0 `ToolResultEvent`, is
  false — the Turn-end `turnSnapshotMsg` already covers depth-0 results and a leaf tool's Turn is
  out of scope (call 3). Document in the predicate's comment why each arm is there and why the
  `SubAgentStarted` phase is NOT one (the head's `ToolCallEvent` already fired the save; under a
  fan-out a queued child's start adds nothing the record does not show).
- In `internal/tui/model.go:777` (`case eventMsg`): after `m.foldEvent` and `m.layout()`, if
  `progressSaveTrigger(msg.Event)` return `m, m.progressSave()` instead of `m, nil`. The save is
  scheduled AFTER the fold so the encoded transcript contains the event that triggered it.
- `TestFoldEventCoversEveryEventVariant` (`internal/tui/fold_test.go:166`) reads
  `internal/domain/events.go` to check every variant is answered by the folds; if its mechanism
  covers new switches on Events, register the predicate's "deliberately nothing" answers the way
  the other folds do — do not weaken that test.

**Files:** `internal/tui/fold.go`, `internal/tui/model.go`, `internal/tui/fold_test.go`,
`internal/tui/sessionsave_test.go`

**Tests:**

- `fold_test.go`: a table over every `domain.Event` variant pinning the predicate's answer,
  including depth-0 vs depth-1 `ToolResultEvent`, a depth-0 non-`sub_agent` `ToolCallEvent`
  (false), and both `SubAgentPhase` values.
- `sessionsave_test.go`: drive a Model with a wired fake `SessionHost` through `turnSnapshotMsg`
  → `eventMsg{ToolCallEvent sub_agent, Depth 0}` → `eventMsg{ToolResultEvent, Depth 1}`; assert
  each of the two events schedules a save (a non-nil Cmd) whose payload pairs the cached boundary
  with a transcript that decodes to hold the open `sub_agent` head; assert a `TokenEvent` at depth 1
  schedules none.
- A coalescing check: two triggers folded while a save is in flight leave exactly one pending
  write (the existing `pendingSave` latch), not two.

**Acceptance:**

```
go build ./internal/tui/ && go vet ./internal/tui/
go test ./internal/tui/ -run 'ProgressSave|FoldEvent|Trigger' -count=1
go test ./internal/tui/ -count=1
```

**Commit:** `feat(tui): save the record when a delegation is issued and at each child tool boundary`

---

## 3. Replay closes every open tool call as interrupted and says so — ✅ DONE (2026-08-25)

Depends on item 2 (the shape of the record it must handle), independent in files.

NOTES (2026-08-25): the item's "the painted run shows no running marker" is pinned by rendering the
replayed run at BOTH star phases (`renderView(th, 80, false)` vs `…, true`) and asserting the lines
are identical — the blink phase is the only place a live block differs in the plain paint, since
`blockState.star` returns ✦ for live and settled alike at the settled phase.

NOTES (2026-08-25): the firing round-trip the item asks for landed as its own test
(`TestTranscriptCodecInterruptedPassLeavesAFiringBlockAlone`) rather than as a tail of the main codec
test — it asserts a different rule (the two replay rules staying apart) and reads better named for
it; both are in `transcriptcodec_test.go` beside the existing firing test.

**What:** implement ratified call 5 so a progress-saved record never paints a dead child as running.

- Add `interruptedSummary = "interrupted — the run did not finish"` beside the other outcome words
  in `internal/tui/toolview.go:111-113`, and make `failedSummary` (`internal/tui/toolleader.go:303`)
  recognise it, so `subAgentFinished` (`internal/tui/subagentblock.go:325`) reports false for a
  head closed this way and the run paints as not-finished, never as reported-OK.
- Add a replay pass in `internal/tui/transcriptcodec.go` next to `fromWireEntry`'s firing rule:
  `closeInterruptedCalls(entries []entry) (closed int)` — for every `entryToolCall` with
  `done == false` set `done = true` and `tool.Summary = namedSummary(detailLine{Text:
  interruptedSummary})`; an entry the firing rule already closed is skipped (it is `done`). Call it
  from `replayScrollback` (`internal/tui/model.go:581`) on the decoded entries BEFORE
  `m.transcript.replay(entries)`, and when `closed > 0` add one ephemeral note:
  `progressSavedNote = "this record was saved while a delegation was still running — that unfinished work was not kept; /continue re-runs the step that started it, a new message discards it"`.
  When the record is also `inExchange`, `interruptedNote` still follows as today (two notes, each
  saying its own thing). Keep the rule in the codec layer as a post-decode pass, not inside
  `fromWireEntry` per entry: the note needs the count, and the firing rule's per-entry placement is
  about ONE kind.
- Do not touch the live paint path: `hasOpenToolCall`, `subAgentFramed` and the blink marker are
  unchanged — the rule is about a fact that changed between the write and the read, exactly as the
  firing rule's comment says.

**Files:** `internal/tui/transcriptcodec.go`, `internal/tui/toolview.go`,
`internal/tui/toolleader.go`, `internal/tui/model.go` (only `replayScrollback`),
`internal/tui/sessions.go` (the note constant beside `interruptedNote`),
`internal/tui/transcriptcodec_test.go`, `internal/tui/sessions_test.go`,
`internal/tui/subagentblock_test.go`

**Tests:**

- `transcriptcodec_test.go`: encode a transcript holding an open `sub_agent` head at depth 0 with
  an open child call at depth 1 beneath it; decode + `closeInterruptedCalls`; assert both are
  `done` with `interruptedSummary`, the count is 2, and a closed leaf call elsewhere is untouched.
  Round-trip a firing `!done` entry through the same pass and assert it keeps
  `scheduleInterruptedSummary` (the firing rule wins, the general pass skips it).
- `subagentblock_test.go`: a head carrying `interruptedSummary` → `subAgentFinished` false,
  `subAgentReported` per its existing contract, and the painted run shows no running marker.
- `sessions_test.go`: resuming a record whose blob holds an open head adds `progressSavedNote`
  once; with `inExchange` true both notes appear in that order; a record with no open call adds
  neither (extend `TestSessionBrowserResumeInterruptedNote`'s fixture pattern).

**Acceptance:**

```
go build ./internal/tui/ && go vet ./internal/tui/
go test ./internal/tui/ -run 'Interrupted|TranscriptCodec|SubAgent|SessionBrowserResume' -count=1
go test ./internal/tui/ -count=1
```

**Commit:** `fix(tui): replay closes every open tool call as interrupted and notes the unfinished delegation`

---

## 4. Record the decision: ADR 0022 addendum, manual, CONTEXT.md, ISSUES.md

Depends on items 1–3 (documents what landed). Docs only — no code, no `make check` needed per the
repo's docs-only convention.

**What:**

- `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`: append
  `## Addendum (2026-08-25) — a delegating Turn is progress-saved; the engine half keeps the boundary`.
  State, in this order: the gap (a Turn holding a delegation — 15+ minutes, a whole fan-out batch
  under ADR 0039 — kept the record at the previous tool call; "at most one Turn" was unbounded in
  wall-clock and tokens for such a Turn); the rule (the TUI re-persists the record on the depth-0
  `sub_agent` call, on every child tool result, and on every child finish, pairing the LAST
  quiescent-boundary snapshot — the idle snapshot taken before the worker launched, or the latest
  per-Turn one — with the live transcript); what does not move (ADR 0007's boundary rule: no
  snapshot inside a Step; decision 1's cadence for the engine half; the record shape and all three
  versions; decision 8's child-Session non-goal); the resume semantics (a resumed progress-saved
  record re-attempts the delegating Turn exactly as a cancelled Turn does — the open calls are
  closed as interrupted at replay and one note says the unfinished work was not kept); the bound
  as now stated ("a crash loses at most one Turn of ENGINE state; of the SCROLLBACK it loses at most
  the work since the last child tool boundary"); and the three options considered with the two
  rejected (document-only; an engine Step boundary at delegation / nested stepping — deferred to
  its own grill, ADR 0007's "snapshot schema leaves room for a suspended sub-agent" is the door).
- `docs/manual/sessions.md`: amend lines 3–6 ("costs at most the turn in flight") and the bullet at
  line 30 so they say: a running delegation is saved as it progresses, a resumed record shows it
  as interrupted, and `/continue` re-runs the step that started it.
- `CONTEXT.md` § **Session** / **Session record** (line 187): add one sentence naming the
  **progress save** — the transcript half written mid-Turn while a delegation runs, paired with
  the last boundary snapshot — so the term exists where the concept map lives.
- `ISSUES.md`: remove the whole entry *A running delegation — and the whole Turn around it — is
  absent from the session record until the Turn ends* (lines 32–58, heading through its `---`); no
  narration stays behind. `CHANGELOG.md` `[Unreleased]` already carries items 1–3's entries (the
  verifier writes those per item); this item adds nothing there unless a line is missing.

**Files:** `docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md`,
`docs/manual/sessions.md`, `CONTEXT.md`, `ISSUES.md`

**Tests:** none (prose). Verifier reads the addendum against ratified calls 1–5 above and checks
`ISSUES.md` no longer contains the heading.

**Acceptance:**

```
grep -c "Addendum (2026-08-25)" docs/adr/0022-sessions-persist-per-turn-as-dual-representation-records.md   # → 1
grep -c "A running delegation" ISSUES.md   # → 0
grep -c "progress save" CONTEXT.md docs/manual/sessions.md   # each ≥ 1
```

**Commit:** `docs(sessions): record the delegation progress save — ADR 0022 addendum, manual, CONTEXT, ISSUES`

---

## Suggested version bump

Patch-level (`v0.17.2`): user-visible behaviour change in what a saved session shows mid-run and on
resume, no API change, no record-version change. The owner decides; nothing in this plan bumps.
