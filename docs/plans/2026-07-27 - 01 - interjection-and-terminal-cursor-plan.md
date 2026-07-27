# Plan — Interjections: type-ahead + mid-run delivery, and the real terminal cursor

**Date:** 2026-07-27
**Status:** READY (grilled with the owner 2026-07-27 — six decisions recorded below; ground verified against the working tree same day)
**Source:** ISSUES.md items 1–2 — "I cannot start writing the next prompt when the model is working … scheduled messages … sent when possible even when the model is still working" and "The cursor in the prompt box is blinking … I just want a full static symbol … preferably the terminal's defined cursor (line vs block)".
**Track:** rides **v0.9.0** `[Unreleased]` (current `VERSION` v0.8.7; additive public surface, one deliberate behavioral change: keys type into the input while the model works instead of scrolling the transcript).
**Public API:** additive (ADR 0010): `Agent.Interject` (public automatically via the `apogee.Agent` alias, `apogee.go:52`), exported field `domain.Message.Interjected`, sentinel `domain.ErrNoOpenExchange` (re-exported at root + `example_test.go` guard), config key `cursor-shape`, `tui.Options.CursorShape`. The internal `tui.Engine` seam gains `Interject` (every fake engine in `internal/tui/*_test.go` follows).
**Standing requirement:** `/coding-standards` is forwarded to the implementer and verifier sub-agents.

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Items 1 → 2 → 3 → 4 → 5 run in order; the tree is coherent and green after every item and you may stop after any completed one. Item 6 (cursor) is fully independent — it may run at any point, even first. Item 7 runs last.

**Deviations leave a trail.** Any authorized deviation gets a dated `NOTES (YYYY-MM-DD):` paragraph directly under the item heading.

**Authoritative sources**, in precedence order:
1. This plan (encodes the owner decisions).
2. ADR 0011 (thin renderer; the legal engine-call classes — this plan names a third), ADR 0010 (package layout), ADR 0017 (the Exchange-scoped deferred queue this feature deliberately does NOT reuse), ADR 0014 ("steer" is taken by guided decomposition), ADR 0022 (per-Turn session records).
3. CONTEXT.md domain language (Turn / Exchange / Step / quiescent boundary; the new noun **Interjection** lands in item 7).
4. The code as it stands.

---

## Owner decisions (grill, 2026-07-27)

1. **Delivery: ASAP, into the running exchange.** One queue. A message typed and entered while the model works is delivered INTO the running exchange at the next tool-round boundary (the model sees it mid-task); if the model is already writing its final answer, it arrives at exchange end and starts a new exchange. "Scheduled" in the issue means queue-and-deliver-when-possible, not clock-timed. Typing stays blocked at `stateAwaitingApproval` (a/d/s own the keyboard), `stateAwaitingAsk` (the box already holds the answer), and `stateErrored` (Enter dismisses).
2. **Stop/error holds the queue.** Auto-delivery happens only on natural completion. After Esc or a loop error the staged rows stay put; the next Enter — even on an empty input — sends them, and Backspace on an empty input pops the newest back into the editor. Esc genuinely stops everything.
3. **Queue UI: pending rows above the input box.** Dim ⧖ rows in the bottom chrome (the skill-chips slot), status line shows `N queued`. The transcript records each message only at actual delivery, so it stays an honest record of what the model saw and when.
4. **Cursor: the real terminal cursor, always steady, `cursor-shape` config key.** Bubble Tea v2's renderer must always name a shape (block/underline/bar ± blink) — "inherit the terminal's configured shape" is not expressible (`encodeCursorStyle` never emits the DECSCUSR-0 reset while running) — so the key (`block` default, `underline`, `bar`) is the honest substitute. Never blinking, no blink key.
5. **History: a committed, marked user message.** An interjection becomes a real, durable `RoleUser` message in engine history, carrying a marker the derived-Exchange computation skips. It survives turns, compaction, and session save/restore. The wire consequence (a user message after tool results — OpenAI-legal, but documented in-repo as breaking strict Gemma-class templates) is accepted and recorded; if it ever bites a live template it is a model-profile concern, not a history redesign.
6. **Noun: "Interjection."** A message the human interjects into a running exchange. `Agent.Interject`, interjected messages, pending interjections. Avoids ADR 0014's "steer"; CONTEXT.md gets the entry plus the disambiguation.

---

## The ground (verified 2026-07-27 against the working tree)

**TUI states and the typing block.** `uiState` at `internal/tui/model.go:27-35` (`stateIdle`/`stateRunning`/`stateAwaitingApproval`/`stateAwaitingAsk`/`stateErrored`); `busy()` at `:1068-1072`. The block is pure key ROUTING: `handleKey` feeds the textarea only at `stateIdle`/`stateAwaitingAsk` (`model.go:526-541`) and hands every busy-time key to `scrollViewport` (`:542-544`); Enter's running branch is a no-op (`:463-477`); paste is dropped while busy (`:242-244`); mouse-caret placement is refused (`inputEditable`, `mouse.go:69-74`); the `/sessions` and autocomplete overlays are idle-gated (`:431`, `:438`). The textarea itself is never blurred — the widget stays focused and blinking through every state. Pinning tests: `TestModelSubmitWhileRunningIsNoOp` (`model_test.go:492-511`), `TestModelSeamMessageTransitions` (`:387-486`).

**Submit → worker → terminal fold.** `submit()` (`model.go:586-624`) parses (`submitParse`, `prompteditor.go:85-87`), launches `startExchange` (`worker.go:27-31`), sets `stateRunning`. The worker (`stepToBoundary`, `worker.go:110-130`) loops `eng.Step`; on `StatusTurnComplete` it already calls a SECOND engine method between Steps on the worker goroutine — `eng.Snapshot()` at `worker.go:119` — the in-tree precedent this plan's delivery mechanism extends. Terminal Msgs (`messages.go:19-29` compile-assert block) fold into `finishWorker` (`model.go:853-877`), whose `m.quitting` branch (`:865-869`) is the house pattern for "defer an action to the exchange-terminal fold". `/compact` runs as `stateRunning` via `startCompact` (`worker.go:45-55`) → `compactDoneMsg`.

**Engine input path.** `Submit` refuses mid-exchange (`agent.go:127-133`, `ErrInputPending`); the single-slot `pendingInput` (`agent.go:89`) is consumed at the top of `step()` — `loop.go:63-75`: `openExchange()` (caches `exchangeStart`, `turn.go:150-153`), then `resolveSkillRefs` / `resolveFileRefs` (`loop.go:71-72` — @file refs resolve at delivery time, fresh), then one `conv.Append(RoleUser…)`. The Agent's contract (`agent.go:26-27`): drive from ONE goroutine; the only anytime-goroutine-safe mutators are `SetMode`/`SetConfineToWorkspace` behind sibling mutexes (`agent.go:43-58`). ADR 0011's closing rule: idle-only calls guarded by the state machine, or a new mid-`Step` call only behind a `SetMode`-class mutex. Between-Steps calls by the driving goroutine (the `Snapshot` precedent) are a third, so-far-unnamed class — item 7's ADR names it.

**Why a mid-run user message is non-trivial.** At a tool-round boundary the conversation tail is `assistant(tool_calls), tool…` (`dispatch.go:418-422`). (a) `Request.InjectContext` (`hooks.go:391-402`) documents user-after-tool as breaking strict chat templates and routes around it — but it is request-scoped: never committed, gone after one request. (b) The deferred-injection pipe (`hooks.go:741-766` → drained at `loop.go:574-579`) is Exchange-scoped by contract (F6: `closeExchange` clears it, `turn.go:129-132`) and also request-scoped on delivery. (c) The derived Exchange opening is `lastRoleIndex(c, RoleUser)` (`exchange.go:56-61`), stable today precisely because nothing commits a user message mid-exchange (`exchange.go:3-8`); ~9 mechanism call sites read it (guided_decomposition, decompose, library, cot, empty_response, tool_use_enforcer, filehint, toolfilter). Decision 5 commits anyway and fixes the derivation in one place with a marker.

**Message persistence.** `domain.Message` marshals through `messageJSON` (`hooks.go:107`) with unknown-sibling passthrough (`extra`, `hooks.go:42-53`; `messageKnownKeys` `:92-96`). An exported `Interjected bool` field with an `omitempty` tag round-trips sessions with NO `SessionVersion` bump (old snapshots lack it → false; old binaries preserve it as an unknown sibling). The wire projection `toProviderRequest` (`wire.go:14-46`) maps fields explicitly, so the marker never leaves the process.

**Rollback fates.** A cancelled Turn drops `[t.rollback, len)` (`turn.go:104-106`) — `t.rollback` is set by `armRequest` AFTER the between-Steps window, so an interjection delivered at the boundary survives a same-Turn cancel. `AbortExchange` (`agent.go:178-188`) drops the whole exchange including delivered interjections — accepted, documented fate (the transcript keeps the visual record).

**Cursor.** `Init` returns `m.input.Focus()` (the virtual cursor's blink, `model.go:210-212`); `View()` (`model.go:1184-1255`) never sets `tea.View.Cursor`, so the real terminal cursor stays hidden and the bubbles textarea paints a simulated blinking one. bubbles v2 has first-class support for the switch: `SetVirtualCursor(false)` + `textarea.Cursor()` → a `*tea.Cursor` positioned relative to the widget (`textarea.go:1614-1641`), nil when blurred or virtual. `inputContentRect()` (`mouse.go:80-87`) already computes the textarea content's on-screen origin (the box is bottom-anchored above the three-row footer, so overlays above never move it) — the exact translation the cursor needs. Shape/blink flow from `styles.Cursor` (`textarea.go:1637-1639`). Config precedent for a flat scalar key: the `yaml:"…"` struct at `cmd/apogee/config.go:487-523`; template at `cmd/apogee/defaults/config.yaml`.

---

## Decisions taken (mechanical — grounded, with rationale)

- **TUI owns the staging; the engine owns the commit.** The pending queue is TUI state (display rows + hold-on-stop + pop-to-editor are all UI concerns), delivery is a per-exchange mailbox drained by the worker between Steps, and `Agent.Interject` is the engine half that commits the marked message. No new engine mutex: `Interject` is documented as callable only by the driving goroutine between Steps — the `Snapshot`-at-`worker.go:119` class, named in the ADR — so the boundary IS the synchronization (the heartbeat plan's ethos). The mailbox itself (written by the Update goroutine, read by the worker) carries the one real mutex.
- **1:1 mid-run, joined at idle.** Each staged row delivered mid-run becomes its OWN marked user message (honest 1:1 transcript↔history mapping). An idle flush (natural completion with rows left, or Enter on a held queue) joins all texts with blank lines into ONE unmarked user message — exactly one unmarked user message opens the next exchange, so the derived boundary stays trivially correct.
- **Latest-wins pop, FIFO delivery.** Rows deliver oldest-first; Backspace on an empty input pops the NEWEST row back into the editor (staging stores the raw pre-parse input text so the restore is faithful). Queue pops take precedence over the skill-chip pop (`model.go:529-533`) — chips are idle-staged and the two rarely coexist.
- **Commands never queue.** Enter on a `/command` while busy → transcript note (`commands run at idle — not queued`), input preserved. The `@`-file autocomplete works while running (refs are useful in interjections); the `/` command and `/skill` pickers stay idle-only (offering commands that would be refused misleads).
- **Compaction completion also flushes.** `compactDoneMsg` is a natural completion — rows typed during `/compact` auto-send when it lands. Only `cancelledMsg`/`errMsg` hold.
- **Staged rows are session-ephemeral.** Sessions persist what was committed (ADR 0022); undelivered rows die with the process. `/clear` at idle keeps held rows (they are outgoing input, not context).
- **Delivery failure degrades to held.** If `eng.Interject` errors mid-drain (can't happen in the shipped wiring — the worker only drains while the exchange is open), the worker reports only actually-delivered rows; the rest stay staged and reach the model via the idle flush. No row is ever silently dropped.
- **Interjected transcript blocks are not sticky headers.** They render as user-styled blocks at their delivery position but do not join `userBlocks` (`applyStickyHeader`, `model.go:1262+`) — the exchange's OPENING prompt stays the sticky context.
- **No engine Event for interjections.** The TUI notifies itself (`interjectedMsg`); an `events.go` variant is additive later if embedders want observability (`apogee.go:141`).
- **Cursor visibility follows editability.** `View` attaches the real cursor only when `inputEditable()` says so (idle/ask today, + running after item 4); at `stateAwaitingApproval`/`stateErrored` the cursor is hidden (`v.Cursor` stays nil) — no `Blur()` juggling needed.

---

## 1. Domain — the `Interjected` marker, derivation skip, and persistence

**What.** `internal/domain/hooks.go`: add exported `Interjected bool` to `Message` (`:42-53`) with `json:"interjected,omitempty"` in `messageJSON` and an entry in `messageKnownKeys` (`:92-96`) — session round-trip is then automatic (`MarshalJSON` `:107`), no `SessionVersion` bump (state doc note at `internal/agent/state.go:36-39` gains one line saying why). Doc comment states the contract: *set only by `Agent.Interject`; a mid-exchange user message the derived Exchange opening skips.* `internal/domain/exchange.go`: `CurrentExchange` (`:56-61`) derives from the last NON-interjected `RoleUser` message; the package doc's stability argument (`:3-8`) gains the marker clause. `PrefixEnd` (`hooks.go:666-677`) is untouched (the first user message is never interjected). `internal/agent/wire.go` is untouched — the marker never reaches `provider.Message` (pin it with a test). New sentinel `domain.ErrNoOpenExchange` ("interject requires an open Exchange") beside the existing sentinels; re-export in `apogee.go` (`:490-509` block) + the `example_test.go:23+` completeness guard.

**Tests** (`internal/domain/exchange_test.go`, `hooks_test.go` style):
- `TestCurrentExchangeSkipsInterjected` — history `user, assistant(tools), tool, user(Interjected), assistant`: opening is the FIRST user; a later unmarked user moves it as before.
- `TestMessageInterjectedRoundTripsJSON` — marshal→unmarshal keeps the flag; a payload without the key decodes false; unknown-sibling passthrough still green.
- `TestProviderRequestOmitsInterjected` (`internal/agent/wire_test.go`) — a marked message projects to a `provider.Message` with no trace of the marker.

**Acceptance.** Green gate; `grep -rn Interjected --include='*.go' internal | grep -v _test` hits only `internal/domain` (the setter arrives in item 2).

**commit.** `feat(domain): the Interjected marker — mid-exchange user messages the Exchange derivation skips`

---

## 2. Engine — `Agent.Interject` at the between-Steps boundary

**What.** New `internal/agent/interject.go`:

```go
// Interject commits a user message into the OPEN Exchange at the between-Steps
// boundary. Contract: call it only from the goroutine driving Step, between Steps —
// the class the worker's Snapshot call already occupies (ADR 0025). It is NOT an
// anytime-goroutine-safe mutator (deliberately not the SetMode class): the driving
// goroutine owns the conversation, so the boundary is the synchronization.
// The message lands marked Interjected, so the derived Exchange opening (and the
// mechanisms reading it) do not move. Refs resolve now — delivery-fresh.
func (a *Agent) Interject(in domain.UserInput) error
```

Refuses: `!a.turns.inExchange` → `domain.ErrNoOpenExchange`; all-empty input (no text, refs, skills) → unexported `errEmptyInterjection`. Body mirrors the `pendingInput` consumption (`loop.go:71-73`) minus `openExchange`: `turn := a.turns.index`; `skillBlocks := a.resolveSkillRefs(turn, in.SkillIDs)`; `refs := a.resolveFileRefs(turn, in.FileRefs)`; `a.conv.Append(domain.Message{Role: domain.RoleUser, Content: skillBlocks + refs + in.Text, Interjected: true})`. `AbortExchange` (`agent.go:178-188`) unchanged — delivered interjections share the exchange's fate. Docs: the Agent contract comment (`agent.go:26-27`) and the facade's method enumeration (`apogee.go:49-51`) both gain `Interject`; the alias (`apogee.go:52`) publishes it for free. A doc line on `Run` (`agent.go:157-164`): embedders wanting mid-run interjection drive `Step` themselves.

**Tests** (new `internal/agent/interject_test.go`, fake-responder style, under `-race`):
- `TestInterjectAppendsMarkedUserMessage` — two-turn scripted exchange; `Interject` between Steps; the next captured request carries the message after the tool results, in order; the committed `Message` has `Interjected: true`.
- `TestInterjectRefusedWhenIdle` — no open exchange ⇒ `ErrNoOpenExchange`, history unchanged.
- `TestInterjectResolvesFileRefs` — a ref block lands in the content (fixture file), resolved at call time.
- `TestInterjectSurvivesCancelledTurn` — interject at the boundary, cancel DURING the next Turn: the rollback (`turn.go:104-106`) keeps the interjection (it precedes `t.rollback`).
- `TestAbortExchangeDropsInterjections` — abort after delivery ⇒ gone with the exchange tail.
- `TestInterjectPersistsAcrossSnapshotRestore` — `Snapshot` mid-exchange → `RestoreSession` into a fresh Agent keeps the marker (rides `state.go`'s conversation marshal).

**Acceptance.** `go test -race -run 'Interject' ./internal/agent ./internal/domain` green; green gate.

**commit.** `feat(agent): Agent.Interject — a marked user message committed at the between-Steps boundary`

---

## 3. TUI plumbing — the mailbox, the worker drain, and the seam Msg

**What.** The delivery pipe, fake-testable, not yet reachable from typing (item 4 wires the keys). `internal/tui/tui.go`: the `Engine` interface (`:70-120`) gains `Interject(domain.UserInput) error` with the call-discipline doc ("called only by the worker goroutine between Steps of the exchange it drives"); the compile pin at `cmd/apogee/wire.go:33` picks it up; every fake engine in `internal/tui/*_test.go` grows the method. New `internal/tui/interject.go`: `queuedInterjection{id int; raw string; input domain.UserInput}` (`raw` is the pre-parse editor text, for the Backspace restore) and `interjectBox` — a small mutex-guarded FIFO (`push`, `drainAll`) held BY POINTER (doc.go value-copy rule, `doc.go:215-224`; `TestModelNoBuilderByValue` stays green). `Model` gains `box *interjectBox` (created per exchange) and `pendingInterjections []queuedInterjection` (the display copy — plain slice, value-safe; the two reconcile by id via the fold). `messages.go`: `interjectedMsg{items []queuedInterjection}` + the compile-assert entry (`:19-29`). `worker.go`: `startExchange` (`:27-31`) and `startResume` (`:93-95`) accept the box; `stepToBoundary` (`:110-130`) drains at the top of each iteration — `items := box.drainAll(); for each: eng.Interject(it.input)`; delivered items go out as ONE `notify(interjectedMsg{delivered})` before `eng.Step`; an `Interject` error stops the drain and the remainder stays undelivered (held, item 5). `startCompact` takes no box. `submit()`'s call site (`model.go:615`) passes a fresh box.

**Tests** (new `internal/tui/interject_test.go`; `newTestModel`/`step` harness `model_test.go:29-80`):
- `TestWorkerDrainsBoxBetweenSteps` — fake engine scripted for a two-Turn exchange; box filled after Turn 1 ⇒ `Interject` called with the right `UserInput`, `interjectedMsg` observed before the final Step, FIFO order.
- `TestWorkerEmptyBoxDeliversNothing` — no `Interject` calls, no Msg.
- `TestInterjectBoxRaceClean` — concurrent push/drainAll under `-race`.
- `TestWorkerInterjectErrorHoldsRemainder` — first `Interject` errors ⇒ `interjectedMsg` carries zero items; both stay staged.

**Acceptance.** All pre-existing `internal/tui` tests pass with only the mechanical fake-engine additions; green gate.

**commit.** `feat(tui): the interjection mailbox — worker-drained delivery between Steps`

---

## 4. TUI typing while running — key routing, staging rows, delivery fold

**What.** The user-visible half of type-ahead. Routing: `handleKey`'s editable class (`model.go:526-541`) admits `stateRunning` (approval/ask/errored keep today's behavior); the busy fall-through to `scrollViewport` (`:542-544`) now applies only to the excluded states — transcript scrolling while running moves to PgUp/PgDn (already intercepted `:492-495`) and the mouse wheel (`:376-380`), which is the deliberate behavioral change this plan ships. Paste (`:242-244`) admits `stateRunning`. Enter's running branch (`:463-477`): `submitParse()`; a `/command` ⇒ transcript note `commands run at idle — not queued`, input preserved; blank ⇒ no-op; a message ⇒ stage — append `queuedInterjection{id, raw, input}` to `m.pendingInterjections`, `box.push` a copy, `promptEditor.reset()`. Backspace on an empty input (running, or idle with held rows) pops the newest row back into the editor (`raw`), taking precedence over the skill-chip pop (`:529-533`). Fold: `interjectedMsg` removes delivered rows by id and adds one user-styled interjection block per item to the transcript (marked visually, e.g. `⧖ you (interjected)`; NOT added to `userBlocks` — the sticky header keeps the exchange's opening prompt). Display: pending rows render in the bottom chrome directly above the input box (the chips slot, `View` assembly `model.go:1242-1249`), included in the viewport-shrink accounting (`:1203-1215`); `statusLine` (`model.go:1753`) shows `N queued` while non-empty; the placeholder (`prompteditor.go:67`) becomes state-aware — while running it reads `queue a message…  ⏎ queue · esc stop` (swapped on the state transitions in `submit`/`finishWorker`, not per-frame). Overlays: the `@`-file autocomplete region works while running (widen `:438` for the file region; `computeAutocomplete` suppresses command/skill regions unless idle); `/sessions` stays idle. Mouse: `inputEditable` (`mouse.go:69-74`) admits `stateRunning`. Interim honesty note: until item 5, rows undelivered at exchange end simply stay staged (held) — coherent, just not yet auto-flushed.

**Tests** (extend `internal/tui/interject_test.go`; rewrite the pinned busy-behavior tests deliberately):
- `TestTypingWhileRunningEditsInput` — printable keys land in the textarea at `stateRunning`.
- `TestEnterWhileRunningStagesRow` — replaces `TestModelSubmitWhileRunningIsNoOp` (`model_test.go:492-511`): row rendered, box pushed, editor reset, state still `stateRunning`, NO second worker Cmd.
- `TestCommandWhileRunningRefusedWithNote` — `/clear` while running: note added, input preserved, nothing staged.
- `TestBackspaceEmptyPopsNewestIntoEditor` — two rows staged; backspace restores the newest raw text; the older row remains.
- `TestInterjectedMsgMovesRowToTranscript` — fold removes the row, transcript gains the marked block at the tail, sticky header unchanged.
- `TestStatusLineShowsQueuedCount` — `plain(view)` contains `2 queued`; disappears at zero.
- `TestPasteWhileRunningTypes`; `TestScrollWhileRunningViaPgKeysAndWheel`; `TestFileAutocompleteOpensWhileRunning`; `TestApprovalAndAskKeysUnchanged` — a/d/s and the ask-answer path behave exactly as before.

**Acceptance.** Green gate; every pre-existing test that changed is one of the deliberately named rewrites above, nothing else.

**commit.** `feat(tui): type-ahead while the model works — staged interjection rows above the input`

---

## 5. Flush orchestration — auto-send on natural completion, hold on stop

**What.** The queue's terminal semantics (owner decisions 1–2). Natural completion — the `exchangeDoneMsg` fold (`model.go:295`) and the `compactDoneMsg` fold (`:339`): after `finishWorker`, staged rows non-empty ⇒ `flushInterjections()`: join all `raw` texts oldest-first with blank lines into ONE unmarked user message (`UserInput{Text: joined, FileRefs: union}`), one transcript user block, and launch a normal exchange (the `submit()` machinery minus the editor read — extract the shared helper); `tea.Batch` with `saveAtIdle`'s Cmd so the completed exchange still saves first. Hold — the `cancelledMsg` (`:306`) and `errMsg` (`:320`) folds flush nothing; on the transition into a hold with rows staged, note once: `N queued message(s) held — ⏎ sends them`. Idle send — `submit()`'s blank guard (`:599-601`): an empty editor with held rows ⇒ flush; a non-empty editor with held rows ⇒ the editor text joins LAST (it is the newest). `stateErrored`: Enter dismisses first (`:473`), the next Enter sends — two presses, documented. `/clear` at idle keeps held rows (`TestClearKeepsHeldRows`); quit with rows staged just exits (session-ephemeral, per the mechanical decisions). `quitting` (`:865-869`) wins over a flush — a deferred quit never launches a new exchange.

**Tests** (extend `internal/tui/interject_test.go`):
- `TestExchangeDoneFlushesQueue` — two rows undelivered at `exchangeDoneMsg` ⇒ exactly one new worker Cmd; one user message whose text is the blank-line join in FIFO order; rows cleared.
- `TestCompactDoneFlushes` — rows typed during `/compact` fire when it completes.
- `TestCancelHoldsWithSingleNote` / `TestErrorHolds` — no launch, rows staged, one note.
- `TestIdleEnterEmptyInputSendsHeld` — Enter on empty editor sends the held queue.
- `TestIdleEnterMergesEditorLast` — held rows + typed text ⇒ one message, editor text last.
- `TestQuitDeferredBeatsFlush` — quitting during an exchange with rows staged exits without launching.
- `TestClearKeepsHeldRows`.
- `TestEndToEndInterjectionScript` — the acceptance script below as a fold-level test: stage 2 mid-run, 1 delivers at the boundary, exchange ends ⇒ the remaining 1 auto-flushes; transcript order: prompt → turn-1 output → interjected block → turn-2 output → flushed prompt.

**Acceptance.** The scripted fold test's transcript reads exactly in delivery order — no duplicates, no reordering; green gate.

**commit.** `feat(tui): interjection delivery — auto-flush on natural completion, hold on stop or error`

---

## 6. The real terminal cursor — steady, shape from `cursor-shape`

**What.** Independent of items 1–5. Config: `CursorShape string \`yaml:"cursor-shape"\`` in the config struct (`cmd/apogee/config.go:487-523`); validated set `block` (default when empty) / `underline` / `bar`, error names the options; `cmd/apogee/defaults/config.yaml` gains the commented key with a one-line doc ("the prompt cursor — apogee draws the real terminal cursor, always steady; the terminal's own configured shape cannot be inherited while the app runs"). `tui.Options` gains `CursorShape tea.CursorShape` (mapped in the `wire.go` Options construction, `:320-326` area). `prompteditor.go` `newPromptEditor` (params: the shape): `ta.SetVirtualCursor(false)`; styles gain `s.Cursor = textarea.CursorStyle{Shape: shape, Blink: false}` (alongside `blackenInput`, `model.go:196-206`). `Model.View()` (`model.go:1184-1255`): when `inputEditable()` (`mouse.go:69-74` — idle/ask today; running joins in item 4; either order composes) and `m.ready`, translate the widget cursor by the content origin — `c := m.input.Cursor(); if c != nil { x0, y0, _, _ := m.inputContentRect(); c.X += x0; c.Y += y0; v.Cursor = c }` — approval/errored leave `v.Cursor` nil (cursor hidden; the textarea is never blurred, so gating by state, not focus, is the mechanism). `Init`'s doc comment (`model.go:208-212`) is rewritten (no blink to start; `Focus()` retained for the focus STATE); the blink-routing comment at `:401-406` is updated. The `⏎ send` placeholder is untouched here.

**Tests** (`internal/tui/cursor_test.go` + `cmd/apogee/config_test.go`):
- `TestViewCarriesRealCursorAtCaret` — known window size, known input text: `View().Cursor` non-nil at the expected absolute cell; `Blink` false; `Shape` follows `Options.CursorShape`.
- `TestCursorHiddenAtApprovalAndError` — `stateAwaitingApproval` / `stateErrored` ⇒ `View().Cursor == nil`; `stateAwaitingAsk` ⇒ non-nil.
- `TestCursorFollowsMultilineCaret` — caret on a wrapped second visual row lands one row lower (pins the translation against `inputContentRect`).
- `TestVirtualCursorDisabled` — `m.input.VirtualCursor()` is false after construction.
- `TestCursorShapeConfigParses` — table: empty→block, each valid value, an invalid value errors naming the options.

**Acceptance.** Green gate. Manual: the cursor is a steady block (or the configured shape) in every state where typing is possible, never blinks, and the terminal's own cursor returns on exit.

**commit.** `feat(tui): the real terminal cursor — steady, shape from cursor-shape, virtual cursor retired`

---

## 7. Docs, decision record, and release bookkeeping

**What.** ADR **0025** `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` (next free number — 0024 belongs to the heartbeat plan; if that plan has not executed yet, take the next actually-free number and note it): the Interjection concept; the three-way split (TUI stages / worker drains / engine commits); the NAMED third engine-call class — between-Steps calls by the driving goroutine (`Snapshot` precedent, now `Interject`) alongside ADR 0011's idle-only and `SetMode` classes, with a cross-amendment note in ADR 0011's closing rule; the `Interjected` marker and the one-site derivation fix; the wire posture (user-after-tool accepted; strict-template breakage is a model-profile concern, explicitly deferred); hold-on-stop and the idle single-message join; staged rows are session-ephemeral; why the Exchange-scoped deferred pipe (ADR 0017) was NOT reused (request-scoped, exchange-cleared — an interjection must outlive both). The cursor decision gets a paragraph in the CHANGELOG and config docs, not an ADR. `CONTEXT.md`: **Interjection** entry near Turn/Exchange (the human's mid-exchange message, marked, boundary-delivered; _Avoid_: "steering" — that is ADR 0014's guided-decomposition sense; cross-reference both ways), plus "staged/held" phrasing under the entry. `internal/tui/doc.go`: narration (`:201-213`) gains `interject.go` and `cursor_test.go`; the input-cluster paragraph notes the state-aware placeholder. `README.md`: the config-key table gains `cursor-shape`; a short "type while it works" bullet in the feature list. `CHANGELOG.md` `[Unreleased]`: the Interjections block (type-ahead, mid-run delivery, hold-on-stop, `Agent.Interject`) + the cursor block — rides v0.9.0. `ISSUES.md`: check off items 1 and 2 with pointers to ADR 0025 / this plan; the text-selection item (line 5) stays open and untouched. `docs/design/technical-design.md`: amend the Agent surface row (`Interject`) and the TUI row (interjection staging).

**Tests.** None (docs); `make check` still runs.

**Acceptance.** `grep -n "Interjection" CONTEXT.md CHANGELOG.md docs/adr/0025-*.md` all hit; `grep -n "cursor-shape" README.md cmd/apogee/defaults/config.yaml` hit; ISSUES items 1–2 checked.

**commit.** `docs: ADR 0025 — interjections commit at the between-Steps boundary; CONTEXT noun + close-out`

---

## Explicitly NOT in this plan

- **Text selection while the model works** (ISSUES item 3) — same busy-gate territory (`mouse.go:69-74`), deliberately its own issue.
- **Typing during approval / ask / errored states** — a/d/s, the borrowed answer box, and Enter-dismiss keep the keyboard; owner decision 1.
- **Queueing slash commands or `/skill` attachment mid-run** — commands are idle-only and are refused with a note; skills stage at idle as today.
- **Clock-timed scheduling** ("send at 15:00") — "scheduled" in the issue means deliver-when-possible; nothing timer-based ships.
- **Strict-template mitigation for user-after-tool** (Gemma-class) — accepted risk, absorbed by the model-profile layer if it ever bites; recorded in ADR 0025.
- **An engine Event for interjections** — additive later; the TUI notifies itself.
- **A `Run`-loop drain hook for embedders** — embedders wanting mid-run interjection drive `Step`; documented on `Run`.
- **Persisting staged rows across quit / into sessions** — sessions record what was committed (ADR 0022).
- **Cursor blink configurability or inheriting the terminal's configured shape** — never blinks; the shape key is the honest substitute (the renderer cannot express DECSCUSR 0 while running).
- **The transcript scroll keys while running** (j/k/space) — deliberately ceded to typing; PgUp/PgDn and the wheel remain.

## Critical files

- `internal/domain/hooks.go`, `internal/domain/exchange.go` — the marker + derivation skip; `internal/domain` sentinels.
- `internal/agent/interject.go` (new), `internal/agent/agent.go` — `Interject`, contract docs.
- `internal/tui/interject.go` (new) — mailbox + staged rows; `internal/tui/worker.go` — the drain; `internal/tui/messages.go` — `interjectedMsg`; `internal/tui/model.go` — routing, staging, folds, flush, status/placeholder, View cursor; `internal/tui/prompteditor.go` — virtual-cursor retirement, state-aware placeholder; `internal/tui/mouse.go` — `inputEditable`; `internal/tui/tui.go` — the `Engine` seam + `Options.CursorShape`; `internal/tui/doc.go` — narration.
- `cmd/apogee/config.go`, `cmd/apogee/wire.go`, `cmd/apogee/defaults/config.yaml` — `cursor-shape` plumbing.
- `apogee.go`, `example_test.go` — sentinel re-export + guard, method enumeration.
- `docs/adr/0025-…`, `docs/adr/0011-…` (amendment note), `CONTEXT.md`, `README.md`, `CHANGELOG.md`, `ISSUES.md`, `docs/design/technical-design.md`.

## Verification (whole plan)

Manual live run against the llama-launcher host (`http://192.168.64.1:1111`; server control via the llama-launcher MCP):

1. Start an exchange that will run several tool rounds. While the model streams Turn 1, type freely — keys land in the input, the cursor is a steady block (or the configured shape), nothing blinks anywhere.
2. Enter a remark ("also check the tests") mid-run: it appears as a dim ⧖ row above the input, the status line shows `1 queued`, the editor clears. At the next tool boundary the row moves into the transcript as an interjected block and `tail_log` on the server shows the request carrying it after the tool results.
3. Queue two rows while the model writes its FINAL answer (no more tool rounds): on completion both auto-send as ONE new exchange whose prompt is the blank-line join, oldest first.
4. Queue a row, press Esc: the exchange cancels, the row stays staged with the held note; Enter on the empty input sends it. Backspace on the empty input instead pops it back into the editor, editable.
5. Type `/clear` while running: refused with the note, input preserved; `/clear` at idle with a held row keeps the row.
6. `cursor-shape: bar` in config.yaml → restart: the caret is a steady bar; an invalid value errors at startup naming the options; on quit the terminal's own cursor returns.
7. Approval flow (`Ask-Before` mode): while the approval prompt is up, a/d/s still decide, typing does nothing, and the cursor is hidden; after approval, typing resumes.

Automated: the per-item green gate after every item; `TestEndToEndInterjectionScript` (item 5) is the end-to-end proof in CI.
