# Plan — Interjections: type-ahead + mid-run delivery, the real terminal cursor, and selection that survives the stream

**Date:** 2026-07-27
**Status:** complete (grilled with the owner 2026-07-27 — six decisions recorded below; ground verified against the working tree same day. **Second grill, same day:** the text-selection issue merged in — decisions 7–9; selection ground verified against the working tree the same way.)
**Source:** ISSUES.md items 1–3 — "I cannot start writing the next prompt when the model is working … scheduled messages … sent when possible even when the model is still working", "The cursor in the prompt box is blinking … I just want a full static symbol … preferably the terminal's defined cursor (line vs block)", and "I cannot select text in apogee when the model is working. I'd like to be able to select text at any point in time."
**Track:** rides **v0.9.0** `[Unreleased]` (current `VERSION` v0.8.7; additive public surface, one deliberate behavioral change: keys type into the input while the model works instead of scrolling the transcript).
**Public API:** additive (ADR 0010): `Agent.Interject` (public automatically via the `apogee.Agent` alias, `apogee.go:52`), exported field `domain.Message.Interjected`, sentinel `domain.ErrNoOpenExchange` (re-exported at root + `example_test.go` guard), config key `cursor-shape`, `tui.Options.CursorShape`. The internal `tui.Engine` seam gains `Interject` (every fake engine in `internal/tui/*_test.go` follows). The selection merge (items 7–8) adds NO public surface — it is all internal TUI rendering.
**Standing requirement:** `/coding-standards` is forwarded to the implementer and verifier sub-agents.

Per-item green gate:

```
gofmt -l .                                              # empty
make check                                              # vet + lint + go test -race -count=1 ./...
GOOS=windows go build ./... && GOOS=darwin go build ./...
```

**Dependencies.** Items 1 → 2 → 3 → 4 → 5 run in order; the tree is coherent and green after every item and you may stop after any completed one. Items 6 (cursor) and 7 (transcript-selection survival) are fully independent — each may run at any point, even first. Item 8 (selection scope pins) follows items 4 and 7. Item 9 runs last.

**Deviations leave a trail.** Any authorized deviation gets a dated `NOTES (YYYY-MM-DD):` paragraph directly under the item heading.

**Authoritative sources**, in precedence order:
1. This plan (encodes the owner decisions).
2. ADR 0011 (thin renderer; the legal engine-call classes — this plan names a third), ADR 0010 (package layout), ADR 0017 (the Exchange-scoped deferred queue this feature deliberately does NOT reuse), ADR 0014 ("steer" is taken by guided decomposition), ADR 0022 (per-Turn session records).
3. CONTEXT.md domain language (Turn / Exchange / Step / quiescent boundary; the new noun **Interjection** lands in item 9).
4. The code as it stands.

---

## Owner decisions (grill, 2026-07-27)

1. **Delivery: ASAP, into the running exchange.** One queue. A message typed and entered while the model works is delivered INTO the running exchange at the next tool-round boundary (the model sees it mid-task); if the model is already writing its final answer, it arrives at exchange end and starts a new exchange. "Scheduled" in the issue means queue-and-deliver-when-possible, not clock-timed. Typing stays blocked at `stateAwaitingApproval` (a/d/s own the keyboard), `stateAwaitingAsk` (the box already holds the answer), and `stateErrored` (Enter dismisses).
2. **Stop/error holds the queue.** Auto-delivery happens only on natural completion. After Esc or a loop error the staged rows stay put; the next Enter — even on an empty input — sends them, and Backspace on an empty input pops the newest back into the editor. Esc genuinely stops everything.
3. **Queue UI: pending rows above the input box.** Dim ⧖ rows in the bottom chrome (the skill-chips slot), status line shows `N queued`. The transcript records each message only at actual delivery, so it stays an honest record of what the model saw and when.
4. **Cursor: the real terminal cursor, always steady, `cursor-shape` config key.** Bubble Tea v2's renderer must always name a shape (block/underline/bar ± blink) — "inherit the terminal's configured shape" is not expressible (`encodeCursorStyle` never emits the DECSCUSR-0 reset while running) — so the key (`block` default, `underline`, `bar`) is the honest substitute. Never blinking, no blink key.
5. **History: a committed, marked user message.** An interjection becomes a real, durable `RoleUser` message in engine history, carrying a marker the derived-Exchange computation skips. It survives turns, compaction, and session save/restore. The wire consequence (a user message after tool results — OpenAI-legal, but documented in-repo as breaking strict Gemma-class templates) is accepted and recorded; if it ever bites a live template it is a model-profile concern, not a history redesign.
6. **Noun: "Interjection."** A message the human interjects into a running exchange. `Agent.Interject`, interjected messages, pending interjections. Avoids ADR 0014's "steer"; CONTEXT.md gets the entry plus the disambiguation.

**Second grill (2026-07-27, selection merge):**

7. **Selection rides this plan.** ISSUES item 3 — text selection while the model works — merges in as items 7–8 rather than a companion plan, so the three issues land together. The first grill's carve-out ("deliberately its own issue") is superseded by this decision.
8. **A selection over changing text drops honestly.** The rule is keep-if-unchanged: a selection (mid-drag or lingering post-copy highlight) survives a repaint exactly when every rendered line it spans is identical before and after; the moment the text under it changes — the streaming tail, a rewrap, a fold-toggle — it drops. Repaint-freezing while the mouse button is held was rejected (the stream would visibly stall under every drag). What you copy is always exactly what you see.
9. **Scope: the transcript everywhere, the prompt where it is editable.** Transcript selection works in every state (idle, running, approval, ask, errored). Prompt-box selection follows prompt editability — idle, ask, and (via item 4) running; at approval/errored the prompt stays inert (a/d/s and Enter-dismiss own the keyboard, the cursor is hidden per item 6, and the transcript covers copying). Read-only prompt selection in inert states was rejected (a caret-less selection in a box you cannot edit).

---

## The ground (verified 2026-07-27 against the working tree)

**TUI states and the typing block.** `uiState` at `internal/tui/model.go:27-35` (`stateIdle`/`stateRunning`/`stateAwaitingApproval`/`stateAwaitingAsk`/`stateErrored`); `busy()` at `:1068-1072`. The block is pure key ROUTING: `handleKey` feeds the textarea only at `stateIdle`/`stateAwaitingAsk` (`model.go:526-541`) and hands every busy-time key to `scrollViewport` (`:542-544`); Enter's running branch is a no-op (`:463-477`); paste is dropped while busy (`:242-244`); mouse-caret placement is refused (`inputEditable`, `mouse.go:69-74`); the `/sessions` and autocomplete overlays are idle-gated (`:431`, `:438`). The textarea itself is never blurred — the widget stays focused and blinking through every state. Pinning tests: `TestModelSubmitWhileRunningIsNoOp` (`model_test.go:492-511`), `TestModelSeamMessageTransitions` (`:387-486`).

**Submit → worker → terminal fold.** `submit()` (`model.go:586-624`) parses (`submitParse`, `prompteditor.go:85-87`), launches `startExchange` (`worker.go:27-31`), sets `stateRunning`. The worker (`stepToBoundary`, `worker.go:110-130`) loops `eng.Step`; on `StatusTurnComplete` it already calls a SECOND engine method between Steps on the worker goroutine — `eng.Snapshot()` at `worker.go:119` — the in-tree precedent this plan's delivery mechanism extends. Terminal Msgs (`messages.go:19-29` compile-assert block) fold into `finishWorker` (`model.go:853-877`), whose `m.quitting` branch (`:865-869`) is the house pattern for "defer an action to the exchange-terminal fold". `/compact` runs as `stateRunning` via `startCompact` (`worker.go:45-55`) → `compactDoneMsg`.

**Engine input path.** `Submit` refuses mid-exchange (`agent.go:127-133`, `ErrInputPending`); the single-slot `pendingInput` (`agent.go:89`) is consumed at the top of `step()` — `loop.go:63-75`: `openExchange()` (caches `exchangeStart`, `turn.go:150-153`), then `resolveSkillRefs` / `resolveFileRefs` (`loop.go:71-72` — @file refs resolve at delivery time, fresh), then one `conv.Append(RoleUser…)`. The Agent's contract (`agent.go:26-27`): drive from ONE goroutine; the only anytime-goroutine-safe mutators are `SetMode`/`SetConfineToWorkspace` behind sibling mutexes (`agent.go:43-58`). ADR 0011's closing rule: idle-only calls guarded by the state machine, or a new mid-`Step` call only behind a `SetMode`-class mutex. Between-Steps calls by the driving goroutine (the `Snapshot` precedent) are a third, so-far-unnamed class — item 9's ADR names it.

**Why a mid-run user message is non-trivial.** At a tool-round boundary the conversation tail is `assistant(tool_calls), tool…` (`dispatch.go:418-422`). (a) `Request.InjectContext` (`hooks.go:391-402`) documents user-after-tool as breaking strict chat templates and routes around it — but it is request-scoped: never committed, gone after one request. (b) The deferred-injection pipe (`hooks.go:741-766` → drained at `loop.go:574-579`) is Exchange-scoped by contract (F6: `closeExchange` clears it, `turn.go:129-132`) and also request-scoped on delivery. (c) The derived Exchange opening is `lastRoleIndex(c, RoleUser)` (`exchange.go:56-61`), stable today precisely because nothing commits a user message mid-exchange (`exchange.go:3-8`); ~9 mechanism call sites read it (guided_decomposition, decompose, library, cot, empty_response, tool_use_enforcer, filehint, toolfilter). Decision 5 commits anyway and fixes the derivation in one place with a marker.

**Message persistence.** `domain.Message` marshals through `messageJSON` (`hooks.go:107`) with unknown-sibling passthrough (`extra`, `hooks.go:42-53`; `messageKnownKeys` `:92-96`). An exported `Interjected bool` field with an `omitempty` tag round-trips sessions with NO `SessionVersion` bump (old snapshots lack it → false; old binaries preserve it as an unknown sibling). The wire projection `toProviderRequest` (`wire.go:14-46`) maps fields explicitly, so the marker never leaves the process.

**Rollback fates.** A cancelled Turn drops `[t.rollback, len)` (`turn.go:104-106`) — `t.rollback` is set by `armRequest` AFTER the between-Steps window, so an interjection delivered at the boundary survives a same-Turn cancel. `AbortExchange` (`agent.go:178-188`) drops the whole exchange including delivered interjections — accepted, documented fate (the transcript keeps the visual record).

**Cursor.** `Init` returns `m.input.Focus()` (the virtual cursor's blink, `model.go:210-212`); `View()` (`model.go:1184-1255`) never sets `tea.View.Cursor`, so the real terminal cursor stays hidden and the bubbles textarea paints a simulated blinking one. bubbles v2 has first-class support for the switch: `SetVirtualCursor(false)` + `textarea.Cursor()` → a `*tea.Cursor` positioned relative to the widget (`textarea.go:1614-1641`), nil when blurred or virtual. `inputContentRect()` (`mouse.go:80-87`) already computes the textarea content's on-screen origin (the box is bottom-anchored above the three-row footer, so overlays above never move it) — the exact translation the cursor needs. Shape/blink flow from `styles.Cursor` (`textarea.go:1637-1639`). Config precedent for a flat scalar key: the `yaml:"…"` struct at `cmd/apogee/config.go:487-523`; template at `cmd/apogee/defaults/config.yaml`.

**Selection (second grill; verified 2026-07-27 against the working tree).** Both drag-selections already exist (`mouse.go`): the prompt's rune-offset model and the transcript's screen-space content-coordinate model (`:35-61`), with region-arbitrated handlers Update routes in EVERY state (`model.go:418-436`); the copied text is sliced from the cached rendered lines (`transcriptSelectionText`, `mouse.go:347-372`). Two distinct blocks produce the issue. **Prompt side:** `inputEditable` (`mouse.go:72-74`) admits only idle/ask — item 4 already admits `stateRunning`, so prompt selection while running arrives with the routing item; today's refusals are pinned by `TestClickIgnoredWhileRunning` (`mouse_test.go:180`) and `TestPasteIgnoredWhileRunning` (`:452`) — rewrites the first grill's item 4 did not name (amended now). **Transcript side:** `refreshViewport` (`model.go:1562-1586`) unconditionally clears `transcriptSel` (`:1566`) because regenerated lines invalidate content anchors — and while the model streams, every `eventMsg` fold repaints (`:278-281`; so do the presented/cancelled/err/compact folds `:303-359`, and `layout()` on resize `:1515-1531`), so a drag dies within a token. Pinned by `TestTranscriptSelectionClearsOnStreamToken` (`mouse_test.go:589`) and `TestTranscriptSelectionClearsOnResize` (`:604`). **Why keep-if-unchanged is sound:** `renderView` (`render.go:47-96`) is deterministic at fixed width and entry-append-only; the volatile region is the tail — the streaming buffer block (`:89-95`) and a tool-call run joining its group on arrival (`:81-84`) — so settled lines keep both index and content, and line equality over the span is precisely "the selection's ground did not move". The spinner is status-line chrome (its tick fold, `model.go:383-392`, never repaints the transcript); the heartbeat fold already repaints only on a noted change and cites the drag-selection as the reason (`:401-407`) — the rule demotes that guard from correctness to economy. Approval/ask hold the worker at a rendezvous (no events flow), so transcript selection already works there today; the complaint is the streaming state. A wheel-scroll mid-drag already survives (content anchors; `TestTranscriptSelectionSurvivesWheelScroll`, `mouse_test.go:558`).

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
- **Keep-if-unchanged lives in `refreshViewport`; the predicate lives on `transcriptSel`.** `spanUnchanged(old, new []string) bool` — false when inactive or the span exceeds either slice, else line equality over the normalised span — evaluated against the outgoing `m.lines` BEFORE `rendered.lines` replaces them. Cols need no re-check: identical lines have identical widths. The lingering post-release highlight obeys the same rule, so a copied span stays visibly marked while the stream continues below it.
- **Copy equals sight, by construction.** The release path slices the same `m.lines` the rule protected, so a kept selection can never copy text that differs from what is on screen.
- **Item 4's named rewrites extend to the mouse pins.** `TestClickIgnoredWhileRunning` → `TestClickPositionsCaretWhileRunning`, and `TestPasteIgnoredWhileRunning` is superseded by item 4's `TestPasteWhileRunningTypes` — a first-grill gap in item 4's rewrite list, closed by this merge.

---

## 1. Domain — the `Interjected` marker, derivation skip, and persistence — ✅ DONE (2026-07-27)

NOTES (2026-07-27): two literal-text deviations, both deliberate. (a) The acceptance grep also hits `internal/agent/state.go:34` — that is the one-line session-schema doc note this same item mandates, a comment, not the setter; the setter still arrives only in item 2, so the grep's intent (no `Interjected` writer outside `internal/domain`) holds. (b) `lastRoleIndex` was split rather than duplicated: the backward scan is now `lastMatchIndex(c, match)`, with `lastRoleIndex` (plain role) and the new `lastExchangeOpening` (RoleUser && !Interjected) as its two one-line callers, so the domain still scans history backwards in exactly one place and `CurrentExchange` is the only reader of the skip. `conversationView.LastUser` deliberately keeps reporting the most recent user message, interjected or not (the plan changes `CurrentExchange` only); the one resulting divergence from `TestCurrentExchange`'s LastUser property pin is documented at the pin and covered by `TestCurrentExchangeSkipsInterjected`.

NOTES (2026-07-27): third deviation, extending the item's literal call-site list (owner decision, second pass after an independent verifier failed the first attempt). `Request.InjectContext` (`hooks.go`) had to change too: it anchored its insert on `lastIndex(…, RoleUser)`, so on a request whose tail is an interjection the UNMARKED injected message landed between the remark and the ask and became the newest non-interjected user message — the derived opening — collapsing the Exchange to the interjection alone and breaking the shared-context invariant `guidedDecompositionCurrentExchangeStart` names (`internal/mechanisms/guided_decomposition.go:511-522`), while contradicting the three doc comments this item writes. The anchor now routes through `lastExchangeOpening`, so the opening never moves and the injection stays where it has always gone (immediately before the ask, ahead of the interjection). Pinned by `TestRequestInjectContextPreservesExchangeOpening` (`internal/domain/hooks_test.go`); `InjectContext`'s own doc comment, the `exchange.go` package doc's stability sentence, and the `lastMatchIndex`/`lastExchangeOpening` docs are corrected to match. The other domain "last user message" locators were re-checked and are correct as they stand: `Conversation.PrefixEnd` scans forward from the head (the first user message is never interjected), `Conversation.Insert` takes an explicit index, and `conversationView.LastUser` deliberately still reports the most recent user message, interjection included — its `ConversationView` interface doc (`hooks.go:270`) now says so outright instead of leaving it implied.

**What.** `internal/domain/hooks.go`: add exported `Interjected bool` to `Message` (`:42-53`) with `json:"interjected,omitempty"` in `messageJSON` and an entry in `messageKnownKeys` (`:92-96`) — session round-trip is then automatic (`MarshalJSON` `:107`), no `SessionVersion` bump (state doc note at `internal/agent/state.go:36-39` gains one line saying why). Doc comment states the contract: *set only by `Agent.Interject`; a mid-exchange user message the derived Exchange opening skips.* `internal/domain/exchange.go`: `CurrentExchange` (`:56-61`) derives from the last NON-interjected `RoleUser` message; the package doc's stability argument (`:3-8`) gains the marker clause. `PrefixEnd` (`hooks.go:666-677`) is untouched (the first user message is never interjected). `internal/agent/wire.go` is untouched — the marker never reaches `provider.Message` (pin it with a test). New sentinel `domain.ErrNoOpenExchange` ("interject requires an open Exchange") beside the existing sentinels; re-export in `apogee.go` (`:490-509` block) + the `example_test.go:23+` completeness guard.

**Tests** (`internal/domain/exchange_test.go`, `hooks_test.go` style):
- `TestCurrentExchangeSkipsInterjected` — history `user, assistant(tools), tool, user(Interjected), assistant`: opening is the FIRST user; a later unmarked user moves it as before.
- `TestMessageInterjectedRoundTripsJSON` — marshal→unmarshal keeps the flag; a payload without the key decodes false; unknown-sibling passthrough still green.
- `TestProviderRequestOmitsInterjected` (`internal/agent/wire_test.go`) — a marked message projects to a `provider.Message` with no trace of the marker.

**Acceptance.** Green gate; `grep -rn Interjected --include='*.go' internal | grep -v _test` hits only `internal/domain` (the setter arrives in item 2).

**commit.** `feat(domain): the Interjected marker — mid-exchange user messages the Exchange derivation skips`

---

## 2. Engine — `Agent.Interject` at the between-Steps boundary — ✅ DONE (2026-07-27)

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

## 3. TUI plumbing — the mailbox, the worker drain, and the seam Msg — ✅ DONE (2026-07-27)

NOTES (2026-07-27): two mechanical extensions of the item's literal text. (a) The box threads through `driveExchange`/`driveResume`/`stepToBoundary` as well, not only the two `start*` constructors the item names — `stepToBoundary` is where the drain lives, so it must receive it — and the six pre-existing call sites in `worker_test.go`/`bridge_test.go`/`e2e_test.go` therefore gained a `nil` box argument. That is one more class of pre-existing-test edit than the acceptance's "only the mechanical fake-engine additions" anticipated; no behavior or assertion in those tests changed. (b) A fresh box is created at ALL THREE exchange-launch sites, not only `submit()`'s: `/continue`'s resume (`startResume` takes a box per this item) and `/continue`'s canned turn are running Exchanges the human can type into. To keep the resulting invariant honest — `m.box` is non-nil exactly while a worker that can deliver is running — `/compact` sets it to nil (it drives no Exchange and takes no box) and `finishWorker` clears it; without those two lines a row staged during `/compact` or after the terminal fold would be pushed into a mailbox nobody drains. Both are covered by the nil-box arm of `TestWorkerEmptyBoxDeliversNothing` and by `interjectBox`'s documented nil-receiver safety.

**What.** The delivery pipe, fake-testable, not yet reachable from typing (item 4 wires the keys). `internal/tui/tui.go`: the `Engine` interface (`:70-120`) gains `Interject(domain.UserInput) error` with the call-discipline doc ("called only by the worker goroutine between Steps of the exchange it drives"); the compile pin at `cmd/apogee/wire.go:33` picks it up; every fake engine in `internal/tui/*_test.go` grows the method. New `internal/tui/interject.go`: `queuedInterjection{id int; raw string; input domain.UserInput}` (`raw` is the pre-parse editor text, for the Backspace restore) and `interjectBox` — a small mutex-guarded FIFO (`push`, `drainAll`) held BY POINTER (doc.go value-copy rule, `doc.go:215-224`; `TestModelNoBuilderByValue` stays green). `Model` gains `box *interjectBox` (created per exchange) and `pendingInterjections []queuedInterjection` (the display copy — plain slice, value-safe; the two reconcile by id via the fold). `messages.go`: `interjectedMsg{items []queuedInterjection}` + the compile-assert entry (`:19-29`). `worker.go`: `startExchange` (`:27-31`) and `startResume` (`:93-95`) accept the box; `stepToBoundary` (`:110-130`) drains at the top of each iteration — `items := box.drainAll(); for each: eng.Interject(it.input)`; delivered items go out as ONE `notify(interjectedMsg{delivered})` before `eng.Step`; an `Interject` error stops the drain and the remainder stays undelivered (held, item 5). `startCompact` takes no box. `submit()`'s call site (`model.go:615`) passes a fresh box.

**Tests** (new `internal/tui/interject_test.go`; `newTestModel`/`step` harness `model_test.go:29-80`):
- `TestWorkerDrainsBoxBetweenSteps` — fake engine scripted for a two-Turn exchange; box filled after Turn 1 ⇒ `Interject` called with the right `UserInput`, `interjectedMsg` observed before the final Step, FIFO order.
- `TestWorkerEmptyBoxDeliversNothing` — no `Interject` calls, no Msg.
- `TestInterjectBoxRaceClean` — concurrent push/drainAll under `-race`.
- `TestWorkerInterjectErrorHoldsRemainder` — first `Interject` errors ⇒ `interjectedMsg` carries zero items; both stay staged.

**Acceptance.** All pre-existing `internal/tui` tests pass with only the mechanical fake-engine additions; green gate.

**commit.** `feat(tui): the interjection mailbox — worker-drained delivery between Steps`

---

## 4. TUI typing while running — key routing, staging rows, delivery fold — ✅ DONE (2026-07-27)

NOTES (2026-07-27): five deviations from the item's literal text, each forced by a hole the literal text leaves open. (a) **`interjectBox.withdraw(id) bool`** (a third method on item 3's mailbox): the Backspace pop must take the row out of the MAILBOX as well as off the display queue, or the worker delivers at its next boundary a message the human just took back into the editor — and the human, editing a copy, sends it twice. A row already drained is refused the pop (`withdraw` false, the editor left untouched); its delivery report moves it into the transcript a moment later, which is the honest answer. A nil box (the `/compact` case) reports true — no worker is draining it. (b) **A new transcript kind `entryInterjected`** rather than an `entryUser` variant: the item asks for a user-styled block that does NOT join `userBlocks`, and `renderView` decides that by kind, so the kind is the mechanism. `renderUserBlock` gained a `marker` parameter (`❯ ` / `⧖ `) so the two blocks share one shape, and `entryKindNames` gained `"interjected"` so a delivered interjection survives a session save/restore — additive within `transcriptVersion` 1 (an older build skips an unknown kind), so no version bump. The marker is the plan's `⧖` glyph alone; the illustrative `you (interjected)` label was not added (the glyph plus the user styling already says it). (c) **The placeholder swaps at five transitions, not two**: `/continue` (both arms) and `/compact` also enter `stateRunning`, and the ask rendezvous borrows the box for an ANSWER — leaving `⏎ queue · esc stop` up while `⏎` sends the answer would be the chrome lying, so `askReqMsg` swaps to the idle legend and `submitAnswer` swaps back. (d) **`maxQueuedRows = 3`** caps the staged-row strip, overflow shown as a `… N more queued` marker above the newest rows: the strip steals its height from the transcript viewport (View's shrink accounting), so an unbounded queue would squeeze the conversation off the screen — the `maxAutocompleteItems`/`maxInputRows` posture. (e) **The routing class is `inputEditable()`**, the mouse's own predicate, rather than a second state list in `handleKey`: item 8's "editability is the rule" arrives one item early as a single definition, which is also what keeps the keyboard and the mouse from ever disagreeing about which states are live. One consequence worth naming: `TestPasteIgnoredWhileRunning` was narrowed to `TestPasteIgnoredWhereInputIsInert` (approval/errored) rather than deleted, so the refusal it pinned still has a home.

NOTES (2026-07-27): two further deviations, both owner-directed after an independent verifier read the first attempt. (f) **The narration sweep.** Three comments stated the OPPOSITE of what ships and are rewritten to describe the routing that actually lands: the `stateRunning` const (`model.go:32`, now "typing stages an interjection"), `handleKey`'s doc (`model.go:494-500` — Enter stages while running, keys feed the input wherever `inputEditable`, PgUp/PgDn are the every-state scroll), and `startExchange`'s single-worker argument (`worker.go:20`, which rested on "the model refuses input while running" — the invariant now rests on the model launching no second worker while what is typed is STAGED). The sweep for others the routing change falsified found four more, all corrected: the wheel-vs-keyboard contrast (`model.go:452-455`), `View`'s shrink narration ("idle only" was true of the chips, not of the overlay, and the queued strip was missing), the `promptEditor.autocomplete` field doc, and the `autocomplete.go` package header plus `autocompleteKey`'s "(idle only)". `internal/tui/doc.go` is deliberately untouched — its narration is item 9's, and nothing in it is now false. (g) **The drain-window defect is fixed here, folded into this item's commit (owner authorization), amending item 3's drain placement.** `driveExchange` calls `Submit` — which only sets `pendingInput`; `turns.inExchange` stays false until `step()` calls `openExchange` — and `stepToBoundary` then drained BEFORE the first `Step`, so a row staged in that window (the `⏎` right after the launching one) was unconditionally removed by `drainAll`, refused with `ErrNoOpenExchange`, and never re-entered the mailbox: a later-staged row reached the model first. `stepToBoundary` now takes an `exchangeOpen bool` and skips the drain before the FIRST Step on the Submit path (the Exchange opens inside that Step, so the flag flips to true unconditionally after it); the resume path passes **true**, because a resumed Exchange IS already open at entry (`driveResume` is launched only when `eng.InExchange()`) and holding its staged rows back would defer them by a whole Turn for nothing. Pinned by `TestDrainWaitsForTheExchangeToOpen` (fails without the fix: one `ErrNoOpenExchange` refusal and a delivery reported before the first Step) and `TestResumeDrainsBeforeItsFirstStep` (the flag is per-path, not a constant). No public or seam surface moved — `stepToBoundary` is unexported and its two callers are in the same file, so no test call site changed.

**What.** The user-visible half of type-ahead. Routing: `handleKey`'s editable class (`model.go:526-541`) admits `stateRunning` (approval/ask/errored keep today's behavior); the busy fall-through to `scrollViewport` (`:542-544`) now applies only to the excluded states — transcript scrolling while running moves to PgUp/PgDn (already intercepted `:492-495`) and the mouse wheel (`:376-380`), which is the deliberate behavioral change this plan ships. Paste (`:242-244`) admits `stateRunning`. Enter's running branch (`:463-477`): `submitParse()`; a `/command` ⇒ transcript note `commands run at idle — not queued`, input preserved; blank ⇒ no-op; a message ⇒ stage — append `queuedInterjection{id, raw, input}` to `m.pendingInterjections`, `box.push` a copy, `promptEditor.reset()`. Backspace on an empty input (running, or idle with held rows) pops the newest row back into the editor (`raw`), taking precedence over the skill-chip pop (`:529-533`). Fold: `interjectedMsg` removes delivered rows by id and adds one user-styled interjection block per item to the transcript (marked visually, e.g. `⧖ you (interjected)`; NOT added to `userBlocks` — the sticky header keeps the exchange's opening prompt). Display: pending rows render in the bottom chrome directly above the input box (the chips slot, `View` assembly `model.go:1242-1249`), included in the viewport-shrink accounting (`:1203-1215`); `statusLine` (`model.go:1753`) shows `N queued` while non-empty; the placeholder (`prompteditor.go:67`) becomes state-aware — while running it reads `queue a message…  ⏎ queue · esc stop` (swapped on the state transitions in `submit`/`finishWorker`, not per-frame). Overlays: the `@`-file autocomplete region works while running (widen `:438` for the file region; `computeAutocomplete` suppresses command/skill regions unless idle); `/sessions` stays idle. Mouse: `inputEditable` (`mouse.go:69-74`) admits `stateRunning`. Interim honesty note: until item 5, rows undelivered at exchange end simply stay staged (held) — coherent, just not yet auto-flushed.

**Tests** (extend `internal/tui/interject_test.go`; rewrite the pinned busy-behavior tests deliberately):
- `TestTypingWhileRunningEditsInput` — printable keys land in the textarea at `stateRunning`.
- `TestEnterWhileRunningStagesRow` — replaces `TestModelSubmitWhileRunningIsNoOp` (`model_test.go:492-511`): row rendered, box pushed, editor reset, state still `stateRunning`, NO second worker Cmd.
- `TestCommandWhileRunningRefusedWithNote` — `/clear` while running: note added, input preserved, nothing staged.
- `TestBackspaceEmptyPopsNewestIntoEditor` — two rows staged; backspace restores the newest raw text; the older row remains.
- `TestInterjectedMsgMovesRowToTranscript` — fold removes the row, transcript gains the marked block at the tail, sticky header unchanged.
- `TestStatusLineShowsQueuedCount` — `plain(view)` contains `2 queued`; disappears at zero.
- `TestPasteWhileRunningTypes` — replaces `TestPasteIgnoredWhileRunning` (`mouse_test.go:452`); `TestScrollWhileRunningViaPgKeysAndWheel`; `TestFileAutocompleteOpensWhileRunning`; `TestApprovalAndAskKeysUnchanged` — a/d/s and the ask-answer path behave exactly as before.
- `TestClickPositionsCaretWhileRunning` — replaces `TestClickIgnoredWhileRunning` (`mouse_test.go:180`): with `inputEditable` admitting `stateRunning`, a click in the box positions the caret mid-run (second grill, decision 9).

**Acceptance.** Green gate; every pre-existing test that changed is one of the deliberately named rewrites above, nothing else.

**commit.** `feat(tui): type-ahead while the model works — staged interjection rows above the input`

---

## 5. Flush orchestration — auto-send on natural completion, hold on stop — ✅ DONE (2026-07-27)

NOTES (2026-07-27): three deviations from the item's literal text, all small. (a) **The join uses each row's PARSED text (`input.Text`), not its verbatim `raw` editor line.** For a staged row the two differ only by trimming (`parseInput` trims, and `extractFileRefs` returns the text unchanged, so `input.Text == TrimSpace(raw)`), and the parsed text is what keeps one row's content identical whether it was DELIVERED mid-run or flushed at the end — sending a row differently depending on which path it took would be the one inconsistency this item exists to avoid. `raw` keeps its single job, the Backspace restore. The @refs stay in the text either way, so the `FileRefs` union is not double-counted. (b) **The hold note is pluralised** rather than the literal `N queued message(s) held — ⏎ sends them`: `1 queued message held — ⏎ sends it` / `%d queued messages held — ⏎ sends them` (`heldNote`), matching the chrome's prose elsewhere. (c) **The idle merge carries any attached skills**, which the item's literal text does not mention: `submit()` already builds `SkillIDs: attached`, and dropping the chips because a queue happened to be held would silently lose what the human staged. The extracted shared helper is `launchExchange` (mailbox + worker + cancel + placeholder + activity + spinner tick); `submit()` and `flushInterjections()` both end in it. `/continue`'s two launch sites still inline the same tail — re-pointing them is a refactor this item does not own.

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

## 6. The real terminal cursor — steady, shape from `cursor-shape` — ✅ DONE (2026-07-27)

NOTES (2026-07-27): three deviations from the item's literal text. (a) **`Init` no longer calls `m.input.Focus()`** — the item asks for it to be "retained for the focus STATE", but `Init` has a VALUE receiver: it holds a copy the program discards, so it never set the focus state in the first place (`newModel`'s own comment says exactly that, and it is why the box is focused at construction). With the virtual cursor retired that call also returns nil now (`textarea.Focus` → `cursor.Focus`, which schedules a blink only in `CursorBlink` mode), so retaining it would leave a call whose every possible narration contradicts itself — the failure mode item 4 was failed for. `Init` is now `return m.beatCmd()`, and its doc says where the focus state does come from. Behaviour is unchanged (`tea.Batch` already collapsed a nil member); the two heartbeat-test helpers whose comments described the batched blink Cmd (`firstBeat`, `assertNoBatch`) are rewritten, and the tests themselves pass untouched. (b) **The "blink-routing comment at `:401-406`"** is, at those lines, the heartbeat fold's repaint-guard comment (item 7's territory, which cites the same lines) — nothing to do with the blink. The comment the item means is `Update`'s default case ("route them to the focused input so the cursor keeps blinking"), which is what was rewritten; the heartbeat guard was left for item 7. (c) **The name vocabulary lives in `internal/tui`** (`ParseCursorShape`, beside `newPromptEditor`), with `cursor-shape` carried through `settings`/`options` as the raw name and parsed once in `wire.go` — the `ParseSpinnerStyle` posture, so the config layer validates against one source of truth instead of restating block/underline/bar. `applyConfig` adds the key name to the error (`apogee: invalid cursor-shape: …`), exactly as it does for `ui.spinner`. Two other comments the change falsified were swept with it: `shadeCells`'s "the only thing lost under the span is the cursor block" (there is no cursor in the rendered content any more) and `newModel`'s "Init returns the cursor's blink Cmd".

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

## 7. Transcript selection survives the stream — the keep-if-unchanged rule — ✅ DONE (2026-07-27)

NOTES (2026-07-27): two small deviations. (a) The predicate's parameters are named `oldLines, newLines` rather than the item's literal `old, new` — `new` is a predeclared identifier, and the repo's own precedent for a before/after pair spells them out (`rebindNote(oldModel, oldWindow, newModel, newWindow)`). The signature is otherwise exactly as written. (b) The narration sweep reached one comment beyond the item's list: `handleMouseRelease`'s doc said the post-copy highlight "stays until the next click, edit, or transcript change" — the last clause is precisely what this item falsifies, so it now names the keep-if-unchanged rule for the transcript span. `internal/tui/doc.go`'s `mouse.go` narration line is deliberately untouched (item 9's territory, and nothing in it is now false).

**What.** Independent of items 1–6 (may run at any point, even first). `internal/tui/mouse.go`: `transcriptSel` gains `spanUnchanged(old, new []string) bool` — false when `!active` or the normalised span exceeds either slice, else string equality of every spanned line (`old[i] == new[i]` for `i` in `[top.line, bot.line]`); cols stay valid by construction (identical lines, identical widths). The package header's selection narration (`:14-31`) gains the survival sentence. `internal/tui/model.go` `refreshViewport` (`:1562-1586`): evaluate the predicate against the outgoing `m.lines` before `rendered.lines` replaces them; clear `transcriptSel` only when it fails — the mid-drag case and the lingering post-release highlight both ride the same rule. The `transcriptSel` field doc (`:116-120`) and the heartbeat fold's repaint-guard comment (`:401-407`) are rewritten: the beat guard stays (repaint economy) but is no longer what keeps a drag alive. Everything else — anchoring, highlight overlay, copy slicing, wheel-scroll survival — is untouched: the rule only decides WHEN the existing machinery lets go.

**Tests** (`internal/tui/mouse_test.go`; deliberate rewrites named):
- `TestTranscriptSelectionSurvivesStreamAppend` — replaces `TestTranscriptSelectionClearsOnStreamToken` (`:589`): selection over the settled prompt block; stream tokens append tail lines; the selection (and its highlight) survive; release copies exactly the settled text.
- `TestTranscriptSelectionDropsWhenSpanChanges` — selection spanning the streaming tail; the next token rewrites those lines ⇒ dropped, no highlight, a release copies nothing.
- `TestTranscriptMidDragSurvivesRepaint` — click, drag, an `eventMsg` fold between motions, drag on, release ⇒ the copy equals the settled span; the drag never died.
- `TestTranscriptSelectionResize` — replaces `TestTranscriptSelectionClearsOnResize` (`:604`): a width change (rewrap) drops the selection; a height-only resize (identical lines through `layout()`) keeps it.
- `TestTranscriptHighlightPersistsWhileStreaming` — a released (copied) highlight stays shaded in `View()` while tokens stream below it.
- `TestNotedBeatRepaintKeepsSelection` — a heartbeat fold that repaints (an offline note landing) leaves a selection over settled lines intact.
- `TestSpanUnchangedTable` — unit table: inactive ⇒ false; span past either slice ⇒ false; identical span ⇒ true; one differing line ⇒ false; reversed anchor/head normalises.

**Acceptance.** Green gate; the only pre-existing tests that changed are the two named rewrites.

**commit.** `feat(tui): transcript selection survives streaming — kept while the lines it spans are unchanged`

---

## 8. Selection at any point in time — scope pins across the state ladder — ✅ DONE (2026-07-27)

NOTES (2026-07-27): one deviation, and it is an absence. The item's single piece of prose work — the
`inputEditable` doc comment naming editability as the rule (idle, ask, running; approval/errored
inert) — already landed with item 4, which adopted `inputEditable()` as the keyboard's routing class
too and rewrote the comment for both readers at once (item 4's NOTES (e)). The comment as it stands
says exactly what this item asks for, so rewriting it again would only churn it; this item therefore
ships as its three test pins alone. No behavior code was needed either — every pin passed on the
first run against items 4 and 7 as committed, which is the item's own success condition ("if any is,
it is a bug in items 4/7"). The pins were placed in one new `mouse_test.go` section rather than
scattered into the prompt and transcript sections above, so the scope decision reads as one rule
with its boundary case beside it.

**What.** After items 4 and 7 — behaviorally almost free (item 4 admits running to `inputEditable`, item 7 keeps transcript selections alive under repaints); this item pins the owner-decided scope (decision 9) so it cannot regress silently. `mouse.go`: the `inputEditable` doc comment (`:69-74`) is rewritten to name editability as the rule (idle, ask, running — the states where the human may edit; approval/errored stay inert), matching item 4's admission. No further behavior code should be needed — if any is, it is a bug in items 4/7, fixed there.

**Tests** (`internal/tui/mouse_test.go`):
- `TestPromptDragSelectsWhileRunning` — at `stateRunning`: click positions the caret, drag selects, release copies the typed runes — the prompt half of "select at any time".
- `TestTranscriptDragCopiesInEveryState` — table over idle / running / awaitingApproval / awaitingAsk / errored: a transcript drag-release copies in each (running drags over settled lines).
- `TestPromptClickRefusedAtApprovalAndErrored` — pins decision 9's boundary: a click in the box at those states starts no prompt selection (it falls through to the transcript arbitration exactly as today).

**Acceptance.** Green gate; `go test -race -run 'Selection|Drag|Click' ./internal/tui` green.

**commit.** `test(tui): selection pinned across the state ladder — transcript everywhere, prompt where editable`

---

## 9. Docs, decision record, and release bookkeeping — ✅ DONE (2026-07-27)

NOTES (2026-07-27): **0025 was actually free** — the heartbeat plan has executed and holds 0024, and
`docs/adr/` ends there — so the ADR took the number the item names, with no renumbering. Six
deviations from the item's literal text, all in the docs themselves. (a) **README has no
"config-key table."** Its `## Configuration` section documents keys in prose and per-key
subsections (`### The upstream API key`, `### The system prompt`); the only tables in the file are
the in-chat commands and the `make` targets. `cursor-shape` therefore landed as a short prose
paragraph closing the Configuration preamble, beside the `context-window:` pin it reads most like,
rather than as a row in a table that does not exist. The capability bullet went in as written
("Type — and select — while it works"). (b) **doc.go gains `interject.go` and `cursor_test.go` in
two NEW paragraphs**, not in the one-line-each file enumeration the item's line numbers point at:
that paragraph is explicitly "the REST of the package", i.e. the files no prose paragraph narrates,
so adding a file to it *and* narrating it would double-name it. The interjection cluster (mailbox /
staging / delivery fold / flush-or-hold, and `inputEditable` as the one routing predicate) and the
real terminal cursor now have a paragraph each, in the file's existing order, and the enumeration's
"names every file in it" claim still holds. (c) **The state-aware placeholder** is noted at the end
of the input-cluster paragraph, as asked. (d) **Verifier finding (b) — the `blockedUpstream`
under-count — is fixed here as the comment touch-up it is**, which extends the item's file list by
one code file: `internal/tui/model.go`'s `blockedUpstream` doc (and doc.go's heartbeat paragraph,
which repeats the same sentence) now say "the three paths a HUMAN opens an Exchange with" and name
the auto-flush as the fourth, ungated opener with the reason it is safe (`foldBeat` ignores a failed
beat mid-Exchange, so the offline state cannot have moved since the completed Exchange was allowed
to start) and the condition under which that stops being true. No behaviour changed; ADR 0025's
consequences record the invariant. (e) **technical-design.md's TUI row also names the real cursor
and the keep-if-unchanged selection rule**, not only the interjection staging the item lists: the
row is that component's status of record and this wave shipped all three, so naming one would date
the row on arrival. The Agent-surface amendment likewise reached the **Errors** row
(`ErrNoOpenExchange`) as well as "Drive the loop" (`Interject`) — the same public surface, two rows.
(f) **Verifier finding (c) — the `stateAwaitingApproval` placeholder still reading `⏎ queue` where
⏎ is a no-op — is NOT fixed here.** Correcting it means changing rendered chrome and pinning the new
wording, which is item 4's placeholder-swap territory (its NOTES (c) already enumerates the five
transitions); item 4 is done, so it is reported as a follow-up rather than absorbed into a docs
sweep. Nothing in this item's prose asserts otherwise.

**What.** ADR **0025** `docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md` (next free number — 0024 belongs to the heartbeat plan; if that plan has not executed yet, take the next actually-free number and note it): the Interjection concept; the three-way split (TUI stages / worker drains / engine commits); the NAMED third engine-call class — between-Steps calls by the driving goroutine (`Snapshot` precedent, now `Interject`) alongside ADR 0011's idle-only and `SetMode` classes, with a cross-amendment note in ADR 0011's closing rule; the `Interjected` marker and the one-site derivation fix; the wire posture (user-after-tool accepted; strict-template breakage is a model-profile concern, explicitly deferred); hold-on-stop and the idle single-message join; staged rows are session-ephemeral; why the Exchange-scoped deferred pipe (ADR 0017) was NOT reused (request-scoped, exchange-cleared — an interjection must outlive both). The cursor decision gets a paragraph in the CHANGELOG and config docs, not an ADR — and the selection-survival rule follows the same precedent: the `mouse.go` header sentence (item 7) plus a CHANGELOG bullet, no ADR. `CONTEXT.md`: **Interjection** entry near Turn/Exchange (the human's mid-exchange message, marked, boundary-delivered; _Avoid_: "steering" — that is ADR 0014's guided-decomposition sense; cross-reference both ways), plus "staged/held" phrasing under the entry. `internal/tui/doc.go`: narration (`:201-213`) gains `interject.go` and `cursor_test.go`; the input-cluster paragraph notes the state-aware placeholder; the `mouse.go` narration line (`:94-97`) gains the keep-if-unchanged rule and the editability scope. `README.md`: the config-key table gains `cursor-shape`; a short "type — and select — while it works" bullet in the feature list. `CHANGELOG.md` `[Unreleased]`: the Interjections block (type-ahead, mid-run delivery, hold-on-stop, `Agent.Interject`) + the cursor block + the selection block (transcript selection survives streaming and is available in every state; prompt selection while running) — rides v0.9.0. `ISSUES.md`: check off items 1–3 with pointers — items 1–2 to ADR 0025 / this plan, item 3 to this plan's items 7–8 and the CHANGELOG entry. `docs/design/technical-design.md`: amend the Agent surface row (`Interject`) and the TUI row (interjection staging).

**Tests.** None (docs); `make check` still runs.

**Acceptance.** `grep -n "Interjection" CONTEXT.md CHANGELOG.md docs/adr/0025-*.md` all hit; `grep -n "cursor-shape" README.md cmd/apogee/defaults/config.yaml` hit; `grep -in "selection" CHANGELOG.md` hits in `[Unreleased]`; ISSUES items 1–3 checked.

**commit.** `docs: ADR 0025 — interjections commit at the between-Steps boundary; CONTEXT noun + close-out`

---

## Explicitly NOT in this plan

- **Typing during approval / ask / errored states** — a/d/s, the borrowed answer box, and Enter-dismiss keep the keyboard; owner decision 1.
- **Selecting the actively-streaming tail** — a selection whose lines change under it drops honestly (decision 8); streamed text becomes selectable the moment it settles.
- **Freezing transcript repaints while the mouse button is held** — rejected in the second grill: the stream must not visibly stall under a drag.
- **Read-only prompt selection at approval/errored** — the prompt stays inert where it is not editable (decision 9); the transcript covers copying there.
- **The terminal's native shift+drag selection** — untouched; it bypasses the app's mouse capture at the terminal layer and remains available as-is.
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
- `internal/tui/interject.go` (new) — mailbox + staged rows; `internal/tui/worker.go` — the drain; `internal/tui/messages.go` — `interjectedMsg`; `internal/tui/model.go` — routing, staging, folds, flush, status/placeholder, View cursor, `refreshViewport` keep-if-unchanged; `internal/tui/prompteditor.go` — virtual-cursor retirement, state-aware placeholder; `internal/tui/mouse.go` — `inputEditable`, `spanUnchanged`, header narration; `internal/tui/mouse_test.go` — the selection pins and the named rewrites; `internal/tui/tui.go` — the `Engine` seam + `Options.CursorShape`; `internal/tui/doc.go` — narration.
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
8. While the model streams a long reply, drag-select a settled paragraph higher up: the highlight holds and the drag extends normally while tokens keep streaming below; release — the flash confirms and a paste matches the screen exactly. Then drag over the still-moving tail: the selection drops the moment the text changes, and the same text selects fine once the stream has passed it. Also drag-select the text you have typed into the prompt box mid-run.
9. During an approval prompt and after an error, drag-copy from the transcript still works; a click inside the prompt box in those states selects nothing there.

Automated: the per-item green gate after every item; `TestEndToEndInterjectionScript` (item 5) is the end-to-end proof for interjections, the item 7–8 selection pins for ISSUES item 3.
