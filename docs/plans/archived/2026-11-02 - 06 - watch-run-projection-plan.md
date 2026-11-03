# A watch holds one run projection, not a second copy of the Model's fields

**Goal:** A read-only watch measures the watched run's sub-agents and gauge against the watched run's own model and window, from the moment it opens. The session view a watch parks becomes one embedded `sessionView` value swapped by one method, so the field lists and their sync chores go.
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 00d5bf9f
**Sources:**
- `docs/reviews/architecture-review-2026-11-02.html` — card #4 (a run projection the watch holds a second copy of)
- Issue `watch-child-model-mismatch` (`capcom show watch-child-model-mismatch`)
- ADR 0103 (decision 3, the journal's frame pair; decision 5, watching) · ADR 0011 (Model copied by value) · ADR 0045 (a child's model cell) · ADR 0075 D10 (lenient readers)
- Glossary Note `watch-mode` · `layout.md` §"The status line's right slot", §"The footer's upstream slot" · `docs/design/test-drivers.md`
- `docs/plans/archived/2026-10-31 - 00 - empty-answers-name-their-cause-plan.md` item 4 (the approved watcher-window fallback)

**Ratified design calls:**
- **Journal frame** (owner, 2026-11-02): the journal's `run_started` gains `model` and `context_window` (ADR 0103 amendment).
- **Projection shape** (owner, 2026-11-02): one embedded `sessionView` struct on `Model` with the swap kept; no separate projection type with its own apply.
- **Projection contents** (owner, 2026-11-02): exactly today's 23 parked fields; split only when a third consumer appears.
- **Footer while watching** (owner, 2026-11-02): it keeps naming the watcher's own host, model and effort; `layout.md` says so.
- **Guards and gates** (owner, 2026-11-02): `TestWatchIsolatesRoutesEveryFoldedMsg` stays; the 11 `watch.isActive` gates stay.
- **Watched-binding authority** (writer, keeps today's fallback): depth-0 usage line > journal frame > record `Meta.Model` (model only) > the watcher's own `opts`.
- **resumeLoaded** (writer, keeps behaviour): keeps its field-by-field writes (it leaves `titleTouched` standing on purpose) and is guarded, not rewritten.
- **Frame members** (writer): always written; `""` and `0` mean unknown. They state the binding at the journal's birth.
- ADR 0011 (copy-safety) and ADR 0103 (a watch is read-only and holds no Engine) hold.

**Standing requirements:**
- skills: coding-standards
- Test-run rules (pass to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never run `-race` or `-cover` over a whole package or a pattern. Never run two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).
- The tui `Model` is copied by value on every Update (ADR 0011): no `strings.Builder` by value; `sessionView` holds plain values; rebuild a slice rather than append in place on a copy.
- `internal/tui/model.go` is ~5k lines: read only the symbols an item names.
- Rendered output outside a watch stays byte-identical in every item, and inside a watch too in items 2–4.
- Test durations go through `internal/testtime`. Driven tests follow `docs/design/test-drivers.md`.

**Out of scope:**
- Review #9, one context-fill value (issue `run-fill-value`). Review #10, one status-line priority fit (issue `status-line-priority-fit`).
- Child `~` estimates on run views.
- Splitting `sessionView` into a run projection, an owner adapter and view navigation.
- Headless stdout's `run_started` frame (unchanged). Re-stamping a journal's frame after a mid-session rebind (the next depth-0 usage line carries the new binding).

**Regression check (2026-11-02, 00d5bf9f):**
- 1: guard folded (owner folds keep `m.opts`).
- 2: guard folded (each `forgetPaintedFrame` deletion made able to fail).
- 3: guard folded (writer decision: direct fields only).
- 4: recast (writer decision: promoted fields; owns the glossary guard sentence).
- 5: guard folded; ADR 0103 decision 3 and the `JournalStarted` doc superseded.
- 6: guard folded (`modelStamper`/`fakeStamper`, launch and fallover halves tested, frame-literal rule).
- 7: guard folded (head-0 case observable; comment rule; `model.go` in Files).
- 8: recast (writer decision: `sessions.md` gauge sentence; glossary guard sentence moves to item 4).
- 4 (re-check): guard folded — `reflect.VisibleFields` named for the promoted fields; `fieldsRead` deleted; `make lint` and a grep for the deleted park symbols added to Acceptance.
- 8 (re-check): guard folded — `sessions.md` check greps the unwrapped prefix; deleted-symbol grep over `layout.md`, `docs/manual`, `.capcom/knowledge` and a `layout.md` grep for the new sentences; glossary Note `event-journal` in Files and the members rule; `layout.md` names both fallback cases.

## 1. A watch measures sub-agent rows against the watched run — ✅ DONE (2026-11-03)

NOTES (2026-11-03): the watched pair reaches the fold through a new `foldEventAgainst(e, window, model)`. `foldEvent(e)` is now a thin wrapper that passes `m.opts`, so its ~150 test callers and the live `eventMsg` case (and with it `foldForOwner`'s owner fold) keep the session's own binding. Only `foldWatchedEvent` passes `topLevelWindow()`/`topLevelModel()`. This follows the item's Regression guard, which overrides the Approach's `foldEvent` bullet.
NOTES (2026-11-03): `recordHead` now returns a fourth value, `model` (Meta.Model), alongside `ctxUsed`. `contextGauge`'s doc sentence now names the top-level run's window as the run-view fallback, because its code now reads it.
NOTES (2026-11-03): `TestWatchMeasuresSubAgentsAgainstTheWatchedRun` gives each child a first tool call, because a running child's row only gets its summary line (fill and model cells) once one exists. Confirmed by temporarily stashing the non-test changes: cases 1 and 2 fail at the base. Case 3 fails when `foldEvent` passes `topLevelWindow()`/`topLevelModel()`.

**What:**
Fix for `watch-child-model-mismatch` (not a regression: wrong since watch shipped on 2026-10-17). `foldEvent` hands `applyUsage` the watcher's `opts.ContextWindow` and `opts.Model`, which a watch does not swap. So an unrouted child of a session on another model shows `· <its model>`, and a child routed onto the watcher's model shows no cell.
**Goal:** in a read-only watch, every sub-agent row's model cell (`subAgentModel`) and fallback window are measured against the watched run's model and window. The watched model is the last depth-0, non-maintenance usage `Model`, else the record's `Meta.Model`, else the watcher's `opts.Model`. The watched window keeps today's rule (`watch.window`, else `opts.ContextWindow`). Outside a watch, both are the session's own `opts`, byte-identical to today.
**Approach (assumed at the header base):**
- `watchState.model` sits beside `watchState.window` and has the same doc.
- `recordHead` also returns the record's `Meta.Model` (both callers: `loadWatchStart`, `readRecordOnly`). `watchStart` carries it, and `paintWatchStart` sets `m.watch.model` when it is non-empty.
- `foldWatchedEvent` sets `m.watch.model` from the same depth-0, non-maintenance usage that moves `watch.window`, when that usage's `Model` is non-empty.
- `Model.topLevelModel()` sits beside `topLevelWindow()`, with the same rule and doc.
- `foldEvent`'s `applyUsage` call passes `m.topLevelWindow(), m.topLevelModel()`; rewrite its comment.
- `contextGauge`'s run-view fallback, for a head with no `ctxLimit`, reads `m.topLevelWindow()`.

**Regression guard.** `foldForOwner` folds an owner `eventMsg` through `fold` → `foldEvent` with `watch.isActive` still true, so `topLevelWindow`/`topLevelModel` there would give the owner's own delegation rows the watched pair. Hand the watched pair to `foldEvent` from `foldWatchedEvent` only (a parameter, or a value `foldForOwner`'s owner fold never sees); this overrides the Approach's `foldEvent` bullet. Owner folds keep `m.opts`.
**Files:** internal/tui/watch.go; internal/tui/fold.go; internal/tui/model.go; internal/tui/watch_test.go
**Read first:** internal/tui/fold.go — foldEvent; internal/tui/watch.go — foldWatchedEvent, paintWatchStart, recordHead, foldForOwner;
  internal/tui/model.go — topLevelWindow, contextGauge; internal/tui/transcript.go — applyUsage
**Closes:** watch-child-model-mismatch
**Tests:** `TestWatchMeasuresSubAgentsAgainstTheWatchedRun` in `watch_test.go`, driven through `newWatchFixture` (signature unchanged), `emit` and `pollWatch`. The watcher is bound to `model-a` with a 32768 window. The journal carries:
- a depth-0 usage with `Model: model-b, ContextWindow: 131072`;
- two depth-0 `sub_agent` calls (`SpawnRunID` set), each with a started `SubAgentPhaseEvent`;
- a depth-1 usage per child: child 1 has `Model: model-b, ContextWindow: 0` (unrouted), and child 2 has `Model: model-a, ContextWindow: 0` (routed onto the watcher's model).

The rendered view must show child 1's row with no `model-b` cell and a fill reading `/128k`, and child 2's row with the `model-a` cell. A second case has no depth-0 usage and a record whose `Meta.Model` is `model-b` (head 3 over `recordHeadEntries()`, seeded by a helper beside `seedRecordCtxUsed`); there, child 1 still shows no cell. A third case folds an owner depth-1 usage (`Model:` the watcher's `opts.Model`) mid-watch through `foldForOwner`; after esc the owner's row shows no model cell and a fill against the watcher's own window. Bite: the first two cases fail at the header base.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Watch|TestApplyUsageStripsTheDelegateModel|TestSubAgentModelFoldsOnlyWhenItDiffers|TestTranscriptLayoutGolden' ./internal/tui/`
**Commit:** `fix(tui): measure a watched run's sub-agents against the watched run`

## 2. Every session-view swap forgets the painted frame — ✅ DONE (2026-11-03)

NOTES (2026-11-03): re-derived from "the owner's generation one below its painted key": clearWatchView touches the generation three times (reset, setRoot, setStage), so the test measures the offset on a probe copy (clearWatchView over a fresh paint cache) instead of hardcoding one, and asserts the watched generation lands on the owner's painted one.
NOTES (2026-11-03): the entry is enterWatch called directly and settled by a tea.FocusMsg Update (folds nothing), not the browser's ⏎: that Update also closes the browser, and the transcript's regained rows lay it out through freshenTranscriptClamp whatever the key says, so deleting enterWatch's forgetPaintedFrame passed through the browser path. The owner fold (exchangeDoneMsg) and esc are driven through Update; the fold's generation offset is measured on the unwatched twin, and the post-fold parked generation is asserted equal to the watched one.
NOTES (2026-11-03): bite results — deleting forgetPaintedFrame in enterWatch fails "entering the watch left the owner's text on screen"; in foldForOwner fails "after the owner's fold the screen is not the watched view"; in leaveWatch fails "the view after esc is not the owner's unwatched view byte for byte". No production code changed.

**What:**
A test-only item. It pins the rule the glossary states but no test checks: every swap of the session view calls `forgetPaintedFrame`, because the parked and watched transcripts count generations apart, and `frameKey` names a transcript by its generation.

**Regression guard.** An owner `eventMsg` lays nothing out (`fold`'s arm), and `clearWatchView`'s `reset` always moves the generation, so the draft cases cannot bite. Drive the mid-watch owner fold with an `exchangeDoneMsg` (`finishWorker` → `m.layout()`), its generation aligned to the watched one; set the owner's `transcript.generation` one below its painted key before the entry Update and assert, before the first read is stepped, that the owner's text is gone; compare the post-esc `View()` with a twin owner model that folded the same owner Msg unwatched.
**Tests:** `TestWatchRepaintsOnEverySessionViewSwap` in `watch_test.go`, driven through `Update`:
- The owner's transcript stands at generation g, and the watched transcript is brought to the same g. Equal counts are the case the rule exists for.
- Right after the entry Update (the owner's generation one below its painted key), before the first read is stepped, `View()` no longer shows the owner's text.
- After an owner `exchangeDoneMsg` folded mid-watch (`foldForOwner`), its generation aligned to the watched one, `View()` still shows the watched view, never the owner's.
- After esc, `View()` shows the owner's view, and it equals byte for byte the `View()` of a twin owner model that folded the same owner Msg unwatched.

Bite: delete each `forgetPaintedFrame` call in turn (in `enterWatch`, `leaveWatch` and `foldForOwner`). Each deletion must fail at least one case; record the three results as a NOTES line. If a deletion passes, align the fixture's generations. Never change production code here.
**Files:** internal/tui/watch_test.go
**Read first:** internal/tui/watch.go — enterWatch, leaveWatch, foldForOwner, clearWatchView; internal/tui/model.go — finishWorker, fold (eventMsg and exchangeDoneMsg arms);
  internal/tui/transcript.go — reset; internal/tui/watch_test.go — TestWatchIsolatesOwnerEventsInTheParkedSession
**Acceptance:**
- `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestWatchRepaintsOnEverySessionViewSwap|TestWatchIsolates' ./internal/tui/`
**Commit:** `test(tui): pin the repaint on every session-view swap`

## 3. sessionView holds the event-derived half of the parked view — ✅ DONE (2026-11-03)

NOTES (2026-11-03): parkedSession embeds sessionView anonymously (not a named member), so the existing `m.watch.parked.state` (schedule.go) and `m.watch.parked.transcript` (watch_test.go) reads compile unchanged; clearWatchView's `parkedSession{...}` literal is nested too (a third composite literal the plan did not list).
NOTES (2026-11-03): the moved fields' doc comments were carried verbatim except positional words the move made false ("above"/"below" in advice, thinking and acts); in Model, the worker and pendingDecision comments now point at sessionView for state and liveStats, and the shared workflows/wakePending comment keeps its other paragraphs with a pointer to sessionView.workflows. transcript and state had no doc of their own and gained one in sessionView.
NOTES (2026-11-03): TestWatchIsolatesParksExactlyTheSessionViewResetSet expands a whole-sessionView selector through a new expandSessionView helper (reflect over sessionView's direct fields only, so the embedded liveStats counts as "liveStats").

**What:**
**Goal:** `Model` embeds one `sessionView` struct (`internal/tui/sessionview.go`) holding `transcript`, `thinking`, `advice`, `liveStats` (embedded), `usage`, `usageBase`, `delegateUsage`, `servedModels`, `sessionCurrency`, `workflows`, `acts` and `state`. The watch still parks and restores exactly the same 23 fields, and rendered output is byte-identical everywhere.
**Approach (assumed at the header base):**
- Move those field declarations, with their doc comments, out of `type Model struct` into `sessionView` under the same names. Promotion keeps every `m.transcript`, `m.state` and `m.ctxUsed` site compiling unchanged (no collisions at the base; the build proves it).
- `sessionView`'s doc names what it holds and that it is the unit a watch swaps.
- Two composite literals become nested: `newModel`'s `state: stateIdle`, and `workflowblock_test.go`'s `Model{transcript: *tr}` (`TestFanOutCardSeatsARunHeadPerItem`).
- `parkedSession` holds a `sessionView` plus the other 11 fields. `parkSession` and `restoreSession` copy the `sessionView` in one assignment, plus the 11.
- `TestWatchIsolatesParksExactlyTheSessionViewResetSet` keeps its set comparison. Its `fieldsRead` and `fieldsWritten` results are expanded so that a `sessionView` selector counts as every `sessionView` field (by `reflect` over `sessionView`).

**Regression guard.** the reflect check in item 3 walks sessionView's DIRECT fields only (no promotion through embedded structs), matching resetSessionView's named set (which names e.g. `liveStats` as one field)
**Files:** internal/tui/sessionview.go; internal/tui/model.go; internal/tui/watch.go; internal/tui/watch_test.go; internal/tui/workflowblock_test.go
**Read first:** internal/tui/model.go — Model, liveStats, newModel; internal/tui/watch.go — parkedSession, parkSession, restoreSession;
  internal/tui/watch_test.go — TestWatchIsolatesParksExactlyTheSessionViewResetSet; internal/tui/commandrun.go — resetSessionView
Depends on item 2.
**Tests:** No new behaviour. The watch suite, item 2's test, the reset and resume tests, `TestTranscriptLayoutGolden`, `TestModelNoBuilderByValue` and `TestModelFitsTheStack` pass unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Watch|TestClear|TestResume|TestSession|TestFanOutCardSeatsARunHeadPerItem|TestTranscriptLayoutGolden|TestModelNoBuilderByValue|TestModelFitsTheStack' ./internal/tui/`
**Commit:** `refactor(tui): hold the event-derived session view in one sessionView`

## 4. The whole parked view is one sessionView, swapped by one method — ✅ DONE (2026-11-03)

NOTES (2026-11-03): enterWatch still parks by copying m.sessionView into watch.parked before the paint-cache steps (the park is a copy, not a swap); the swap into the watch is clearWatchView's swapSessionView, so the entry's trailing forgetPaintedFrame call is gone. clearWatchView now forgets the painted frame on a truncation repaint too (it swaps); that only forces a re-layout of the same state, so output is unchanged.
NOTES (2026-11-03): foldForOwner now also forgets the painted frame on the swap IN (before the owner's fold), not only on the swap back; m.painted is read only by settle at Update's tail, so the fold is unaffected.
NOTES (2026-11-03): expandSessionView (watch_test.go) lost its only caller with the old guard and was deleted beside fieldsRead. The allow-list the AST found is eng, resumePending, writeBusy and pendingWrites (the last two via scheduleWrite); the guard also fails on a stale allow-list entry (a view field, or one neither boundary writes).
NOTES (2026-11-03): moved field docs carried verbatim except positional or neighbour references the move made false (boundary's "the queue above it", cursor's "those same targets"/"the map above it", spentSkills' shared skill-band paragraph, which was split with a pointer left on skillHints); Model's and sessionView's own docs now say the struct holds the whole view and name swapSessionView and the new guard.
NOTES (2026-11-03): the glossary Watch mode Note's "no test checks that" sentence about forgetPaintedFrame was rewritten too (item 2's test checks it, and the swap now goes through swapSessionView), beside the guard sentence this item owns.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** all 23 parked fields live in `sessionView`. `parkedSession`, `parkSession` and `restoreSession` no longer exist, and `watchState.parked` is a `sessionView`. Every swap goes through `(m *Model) swapSessionView(v sessionView) sessionView`, which always calls `forgetPaintedFrame`. A guard test fails when `resetSessionView` or `resumeLoaded` writes a `Model` field outside `sessionView` that is not on that test's named allow-list. Rendered output is byte-identical.
**Approach (assumed at the header base):**
- Move `viewStack`, `detached`, `flash`, `cursor`, `spentSkills`, `autoTitleFired`, `titleTouched`, `pendingTitle`, `sessionName`, `boundary` and `hasBoundary` into `sessionView`, with their docs.
- `enterWatch`, `leaveWatch`, `foldForOwner` and `clearWatchView` swap through `swapSessionView`. `clearWatchView` becomes one `sessionView` literal: the fresh transcript, the reset boards, `stateIdle`. Their paint-cache steps (clear on the way in and out, a fresh cache for the watch) keep their order relative to the park and the swap.
- `resetSessionView` and `resumeLoaded` keep their field-by-field writes.
- `schedule.go`'s `quiescent` (`m.watch.parked.state`) compiles unchanged.
- Replace `TestWatchIsolatesParksExactlyTheSessionViewResetSet` with `TestSessionViewHoldsEveryFieldTheBoundariesReset`. It uses `fieldsWritten` over both methods and `reflect` over `sessionView`. The allow-list is `eng` for the reset, plus `resumeLoaded`'s non-view writes (`resumePending` and whatever the AST finds), each with a one-line reason.
- `TestWatchIsolatesRoutesEveryFoldedMsg` is untouched.
- Rule for comments: every comment naming the deleted symbols or the old guard is rewritten. Find them with `grep -n "parkedSession\|parkSession\|restoreSession\|ParksExactly" internal/tui/*.go`. The test helper `restoreSession(t, m)` in `contextfiles_test.go` is a different symbol and stays.

**Regression guard.** item 4's replacement guard walks sessionView's direct AND promoted fields (e.g. `ctxUsed`), stated explicitly; item 4 also owns the one sentence in the glossary Watch mode Note (.capcom/knowledge/glossary/…/watch-mode.md, the line naming the field-list guard it deletes) and edits it in the same commit so the Note is never stale — item 8 no longer touches that sentence.
Re-check: the new guard names the expansion — `reflect.VisibleFields` over `sessionView`, so the embedded `liveStats` and its promoted fields (`ctxUsed`, which `resumeLoaded` writes) count as view fields; the allow-list holds only `eng`, `resumePending` and the other non-view fields the AST finds. `fieldsRead` (`watch_test.go`) loses its only caller with the old guard and is deleted with it (golangci-lint's `unused` lints tests; `make lint` fails otherwise). The Goal's deletions are checked by a grep over the non-test sources, which leaves the `restoreSession(t, m)` test helper (`contextfiles_test.go`) out.
**Files:** internal/tui/sessionview.go; internal/tui/model.go; internal/tui/watch.go; internal/tui/watch_test.go; .capcom/knowledge/glossary/glossary-identity/watch-mode.md
**Read first:** internal/tui/watch.go — enterWatch, foldForOwner, clearWatchView, parkSession, restoreSession; internal/tui/sessions.go — resumeLoaded;
  internal/tui/commandrun.go — resetSessionView; internal/tui/watch_test.go — fieldsWritten
Depends on item 3.
**Tests:** Item 2's test now drives `swapSessionView`. Removing its `forgetPaintedFrame` call must fail that test. The new guard passes, with a promoted field such as `ctxUsed` (written by `resumeLoaded`) counted as a view field, and the item-3 set passes unchanged. `fieldsRead` is deleted with the old guard; `make lint` reports no unused helper.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Watch|TestSessionView|TestClear|TestResume|TestSession|TestTranscriptLayoutGolden|TestModelNoBuilderByValue|TestModelFitsTheStack' ./internal/tui/`
- `make lint` passes (golangci-lint only, not the test suite)
- `grep -n "parkedSession\|parkSession\|restoreSession(" $(ls internal/tui/*.go | grep -v _test.go)` prints nothing
- `grep -c "ParksExactly" .capcom/knowledge/glossary/glossary-identity/watch-mode.md` prints 0, and `capcom check` passes
**Commit:** `refactor(tui): park and restore the session view as one sessionView value`

## 5. A Firing's journal frame names the run's model and window — ✅ DONE (2026-11-03)

NOTES (2026-11-03): the zero-options frame is pinned by a new test, `TestJournalFrameWritesAnUnknownBindingAsZero`, rather than a row in `TestJournalFrames` (whose rows all assert the `s-1` session stamp); `eventjson.Decode` needed no change — `RunStarted` already maps `model`, so it now reads the journal's model as written.

**What:**
**Goal:** the journal's `run_started` data carries `model` and `context_window` after `entries`, always written. Every journal `run.Once` opens fills them from its `domain.Config` (`Model`, `Context.MaxContextTokens`). Headless stdout's `run_started` is byte-identical.
**Approach (assumed at the header base):** This item changes the shape of `JournalStarted`, so here are its producers and consumers.
- Producers:
  - `eventjson.JournalOptions` and `JournalStarted` (`journal.go`, `NewJournal`) gain `Model` and `ContextWindow`.
  - `run.JournalStart` gains both, and `Once` fills them.
  - `firingJournal` (`cmd/apogee/wire_firing.go`) forwards them.
  - `sessionHost.openEventJournalLocked` is item 6.
- Consumers:
  - The tui's `loadWatchStart` binds the members in item 7.
  - `cmd/apogee`'s `readJournalFrame` ignores them, unchanged.
  - `eventjson.Decode` now reads the journal's `model` into `RunStarted.Model`.

**Regression guard.** `TestJournalFrames` pins the frame's `data` literal: it gains `"model":…,"context_window":…` after `entries`, and its subtest name says so. `planSpec` sets no window, so `TestOnceJournals…` sets `spec.Config.Context.MaxContextTokens` (e.g. 65536) and wants it on `JournalStart`. Headless stdout is pinned by `TestWriterEnvelopeOrderAndNulls`, so the eventjson run takes `Journal|TestWriter|TestDecode`; `textByStream` skips frames, so the decode comparison over `journalStream` does not change. Supersedes ADR 0103 decision 3's "adds only `title` and `entries`" (amended in item 8) and the `JournalStarted` doc ("a journal born at a hold does not hold" the model): this item rewrites the `JournalStarted`, `JournalOptions` and `run.JournalStart` doc comments.
**Files:** internal/eventjson/journal.go; internal/eventjson/journal_test.go; internal/eventjson/testdata/journal.jsonl; internal/run/run.go; internal/run/run_test.go; cmd/apogee/wire_firing.go
**Read first:** internal/eventjson/journal.go — JournalOptions, JournalStarted, NewJournal; internal/eventjson/journal_test.go — TestJournalFrames;
  internal/run/run.go — JournalStart, Once; internal/run/harness_test.go — planSpec; cmd/apogee/wire_firing.go — firingJournal
**Tests:**
- `journalOptions()` names a model and a window, and the golden `testdata/journal.jsonl` is regenerated with `-update` and reviewed: only the frame line changes.
- A zero-options frame writes `"model":"","context_window":0`.
- `TestJournalFrames`' `run_started` data literal gains `"model":…,"context_window":…` after `entries`, and its subtest name says so.
- `TestOnceJournalsAHeldFiringForTheLifeOfItsHold` sets `spec.Config.Context.MaxContextTokens` to a non-zero value (e.g. 65536); its `JournalStart` want carries the spec's model and that window.
- `TestWriterEnvelopeOrderAndNulls` (headless stdout's frame) passes unchanged.
- In `wire_session_test.go`, `TestSessionHostEventJournalEndsBeforeTheHoldIsReleased` (via `firingJournal`) passes unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/eventjson/ ./internal/run/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Journal|TestWriter|TestDecode' ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestOnceJournals' ./internal/run/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestSessionHostEventJournal' ./cmd/apogee/`
**Commit:** `feat(eventjson): the journal's run_started names the run's model and window`

## 6. An interactive session's journal frame names its binding — ✅ DONE (2026-11-03)

NOTES (2026-11-03): wire_live.go seeds the launch window with a direct field write (`w.host.window = w.live.window()`, beside the existing `w.host.currency` write, before any goroutine runs), not a `SetModel` call, so the launch model keeps its one source (newSessionHost's argument); the resumed-record test seeds it the same way.
NOTES (2026-11-03): lock order checked: every `mu` section in wire_session.go releases `mu` before taking `evMu`, so `openEventJournalLocked` reads the pair under `mu` inside `evMu` without an inversion.

**What:**
**Goal:** an interactive session's journal frame names the model and context window the session is bound to at the journal's birth: the launch binding, or the one the last rebind or server fallback bound (`""` and `0` when unbound or unknown).
**Approach (assumed at the header base):**
- `sessionHost` gains `window` beside `model`, under `mu`. `SetModel(model string, window int)` sets both.
- The callers pass:
  - the rebind in `wire_verbs.go`: `spec.MaxContextTokens`;
  - `sessionMover.arrive` in `upstream.go`, which gains a window parameter: `move` passes `0` beside its `""` model, and `fallover` passes the window it hands the TUI in its `ServerFallbackResult`.
- `wire_live.go` sets the launch window (`w.live.window()`, the value `Options.ContextWindow` is built from) right after `newSessionHost` and before `attachEventJournal`, where a resumed record's journal is born. `newSessionHost`'s signature is unchanged (33 test calls).
- `openEventJournalLocked` fills `JournalStart.Model` and `JournalStart.ContextWindow` through one locked read of the pair. Confirm that no path takes `evMu` while holding `mu`.

**Regression guard.** `sessionHost` satisfies `modelStamper` (`upstream.go`), and `fakeStamper` (`wire_helpers_test.go`) implements it for every test mover: extend `modelStamper` and `fakeStamper` together (`fakeStamper` keeps `models` and adds `windows`), and fix `TestSessionHostSetModelStampsSaves`' one-argument call. Assert the fallover window (via `fakeStamper`) in `TestTUIFallbackFalloverRunsTheHeartbeatTail`, and the launch pair on a resumed host's frame born at attach. Every frame literal in `wire_session_test.go` gains the pair — found with `grep -n "JournalStart{\|JournalStarted{" cmd/apogee/wire_session_test.go`.
**Files:** cmd/apogee/wire_session.go; cmd/apogee/wire_verbs.go; cmd/apogee/upstream.go; cmd/apogee/wire_live.go; cmd/apogee/wire_session_test.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/fallback_test.go
**Read first:** cmd/apogee/wire_session.go — sessionHost, SetModel, openEventJournalLocked; cmd/apogee/upstream.go — modelStamper, sessionMover.arrive, fallover;
  cmd/apogee/wire_helpers_test.go — fakeStamper; cmd/apogee/wire_live.go — wireSession
Depends on item 5.
**Tests:**
- `TestSessionHostEventJournalNamesTheBinding`: a host after `SetModel("bound-model", 131072)` writes a frame (the `readJournal` helper) naming both values. After a later `SetModel` and a `Rotate`, the next journal born names the new pair.
- Every frame literal in `wire_session_test.go` (`grep -n "JournalStart{\|JournalStarted{" cmd/apogee/wire_session_test.go`) gains the pair, `newSessionHost`'s model included.
- `TestSessionHostSetModelStampsSaves` calls the two-argument `SetModel`.
- `TestTUIFallbackFalloverRunsTheHeartbeatTail` asserts the fallback window on `fakeStamper.windows`; a resumed host's frame born at attach names the launch pair.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestSessionHost|Rebind|Fallover|Fallback|TestLoadProfile|TestMove' ./cmd/apogee/`
**Commit:** `feat(apogee): an interactive session's journal frame names its binding`

## 7. A watch binds the frame's model and window as it opens — ✅ DONE (2026-11-03)

NOTES (2026-11-03): `reopen` now delegates to the new `reopenBound(title, head, model, window)` helper (frame naming no binding), so the fixture's frame is written in one place; `reopen`'s and `newWatchFixture`'s signatures are unchanged.
NOTES (2026-11-03): the head-0 subtest asserts `m.topLevelModel()` too (the frame's model at head 0, where the record is not read), beside the plan's `m.topLevelWindow() == 131072`; the no-members subtest also asserts the watcher's own model stands in.

**What:**
**Goal:** a journal frame naming a `context_window` > 0 makes it the watch's window, and a non-empty `model` makes it the watch's model, from the frame's fold onward. This happens before any usage line and at head 0 too, where the record is not read. Afterwards, a depth-0 usage line moves either value as before. A frame without the members (an older apogee's journal, or `0` and `""`) keeps item 1's fallback.
**Approach (assumed at the header base):**
- `loadWatchStart` reads the two members off `journalStartedLine` into `watchStart`.
- `paintWatchStart` applies them over the record's `Meta.Model`, and leaves `watch.window` and `watch.model` as they are when the frame names none.
- `newWatchFixture`'s signature is unchanged (`sessions_test.go` calls it 8 times). A helper beside `reopen` writes a frame that names a binding.

**Regression guard.** At head 0 with no reading and no estimate, `contextUsage.view` paints nothing, so the head-0 case asserts `m.topLevelWindow() == 131072` (or `usageRows`' main row, or emits a depth-0 `ContextEstimateEvent` and reads `~…/128k`). Rule: every comment naming where `watch.window` or `watch.model` comes from names the frame too — found with `grep -n "watch\.window\|watch\.model\|usage line has named\|usage line that named\|watched window" internal/tui/*.go`.
**Files:** internal/tui/watch.go; internal/tui/model.go; internal/tui/watch_test.go
**Read first:** internal/tui/watch.go — loadWatchStart, journalStartedLine, paintWatchStart, watchState; internal/tui/model.go — topLevelWindow, contextGauge;
  internal/tui/usage.go — usageRows; internal/tui/watch_test.go — newWatchFixture
Depends on items 1, 4 and 5.
**Tests:** `TestWatchBindsTheFramesModelAndWindowAtEntry`:
- An idle owner's frame names `model-b` and 131072, and the record's `CtxUsed` is 8k (head 3, `seedRecordCtxUsed`). The status line reads `8k/128k` at entry with no usage line, and an unrouted `model-b` child row shows no model cell.
- At head 0, the frame's window alone lights the limit: `m.topLevelWindow()` is 131072 (or `usageRows`' main row, or a depth-0 `ContextEstimateEvent` reads `~…/128k`).
- A hand-written frame line without the two members loads, and the status line reads `8k/32k`.
- `TestWatchLightsTheGaugeFromTheWatchedRun` passes unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Watch|TestSession' ./internal/tui/`
**Commit:** `feat(tui): a watch binds the watched run's model and window as it opens`

## 8. Docs: the frame's binding, the watch gauge and footer, the session view — ✅ DONE (2026-11-03)

NOTES (2026-11-03): the glossary Note `watch-mode`'s "In code" paragraph already states the shipped behaviour (one `sessionView` swapped by `swapSessionView`, which forgets the painted frame, `TestWatchRepaintsOnEverySessionViewSwap`, `TestWatchIsolatesRoutesEveryFoldedMsg`, a 2026-11-03 source line, no "no test checks that"), written by item 4, so it is left unchanged and is not in FILES.
NOTES (2026-11-03): the `event-journal` Note's frame sentence is reworded to list `title`, `entries`, `model` and `context_window` together, so the Acceptance grep for "title. and .entries" prints nothing; its `Decode` sentence now says the headless frame keeps `model` and drops `title`, `entries` and `context_window`.
NOTES (2026-11-03): the prose rule's remaining "own context window" hits (layout.md info-row line, docs/manual/configuration.md) and the ADR 0075 decision / ADR 0103 consequences "title and entries" text are not about the watch gauge or are point-in-time decision text, and stay.

**What:**
Recast at the regression check (2026-11-02).
**Goal:** ADR 0103, `layout.md`, the glossary Note `watch-mode` and `docs/manual/headless.md` state the shipped behaviour of items 1–7, and no doc names a deleted symbol or the replaced guard.
**Approach (assumed at the header base):**
- `capcom know add --id adr-0103-amend-1102 --parent adr-0103`. The amendment says:
  - It qualifies decision 3. The journal's `run_started` also carries `model` and `context_window`: the run's binding at the journal's birth, with `""` and `0` meaning unknown.
  - A watch binds them as it opens, and a depth-0 usage line moves them.
  - A journal without them falls back to the record's model and the watcher's own window.
  - The source is this plan.
- Name `adr-0103-amend-1102` on `adr-0103.md`'s Status line and in its reading-order sentence.
- In `layout.md`:
  - §"The status line's right slot": the watch gauge is measured against the window the watched run is bound to (named by its journal's opening frame). A journal from an older apogee, or a frame naming no window (e.g. after a `/server` move), uses your own window until the run's first reading names its own.
  - §"The footer's upstream slot": while you watch, the footer keeps naming your own host, model and effort. Only the gauge is the watched run's.
- In the glossary Note `watch-mode`, rewrite the "In code" paragraph:
  - the view is one `sessionView` swapped by `swapSessionView`, which always forgets the painted frame (`TestWatchRepaintsOnEverySessionViewSwap`);
  - a new `fold` Msg case still needs a route (`TestWatchIsolatesRoutesEveryFoldedMsg`);
  - drop "no test checks that", and add a dated source line. The sentence naming the field-list guard is item 4's, already rewritten there.
- `docs/manual/headless.md`'s journal bullet ("The opening frame carries two more members") lists the frame's members exactly as `JournalStarted` encodes them.
- Rule for prose: every live doc naming the old guard, the deleted park functions, or the watch's window as always the run's own is corrected. Find them with `grep -rn "ParksExactly\|parkedSession\|parkSession\|own context window" layout.md docs/manual .capcom/knowledge`; every live doc listing the opening frame's members is corrected too, found with `grep -rn "title. and .entries\|two more members\|journal-only members" docs/manual layout.md .capcom/knowledge` (archived and historical text excepted; a hit about anything but the watch gauge is not this rule's).
- `docs/manual/sessions.md`'s watch-gauge sentence is rewritten to match the `layout.md` text above.

**Regression guard.** item 8 also rewrites docs/manual/sessions.md's watch-gauge sentence ("against its own context window from the moment you open it") to match item 8's layout.md text exactly (own window from the moment you open it when the journal's run_started frame names one; otherwise — an older apogee's journal or a frame with window 0, e.g. after a /server move — your own window until the first reading); the prose rule's grep excludes docs/manual/configuration.md (its "own context window" hit is unrelated) — state the rule, not a list.
Re-check: `layout.md`'s right-slot sentence names both fallback cases (an older apogee's journal, or a frame naming no window, e.g. after a `/server` move), and `sessions.md` keeps matching it; its new wording does not reuse the old prefix "own context window from the moment you open". The glossary Note `event-journal` (`run_started` "adds `title` and `entries`"; `Decode` "drops `title` and `entries`") states the frame's members and what `Decode` reads after item 5. Decision-record text (ADR 0075 decision, ADR 0103 consequences) is point-in-time and stays; ADR 0103's is covered by `adr-0103-amend-1102`. Acceptance greps `layout.md`, `docs/manual` and `.capcom/knowledge` for the symbols items 3–4 delete, and `layout.md` for the new sentences.
**Files:** .capcom/knowledge/decisions/adr-0103.md; .capcom/knowledge/decisions/adr-0103/adr-0103-amend-1102.md; .capcom/knowledge/glossary/glossary-identity/watch-mode.md; .capcom/knowledge/glossary/glossary-identity/event-journal.md; layout.md; docs/manual/headless.md; docs/manual/sessions.md
**Read first:** .capcom/knowledge/decisions/adr-0103.md — Status line; .capcom/knowledge/decisions/adr-0103/adr-0103-decision.md — decision 3;
  layout.md — §"The status line's right slot" (watch-gauge sentence), §"The footer's upstream slot"; docs/manual/sessions.md — "While you watch" bullet; docs/manual/headless.md — §"The Event journal — what a watcher reads" (opening-frame bullet);
  .capcom/knowledge/glossary/glossary-identity/event-journal.md — run_started and Decode sentences; .capcom/knowledge/glossary/glossary-identity/watch-mode.md — "In code" paragraph
Depends on items 4, 6 and 7.
**Tests:** none (docs only).
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0103" --with-children | grep -c "context_window"` prints at least 1
- `grep -c "ParksExactly\|no test checks" .capcom/knowledge/glossary/glossary-identity/watch-mode.md` prints 0
- `grep -n "context_window" docs/manual/headless.md`
- `grep -n "own context window from the moment you open" docs/manual/sessions.md` prints nothing (the unwrapped prefix; it matches the base text), and `grep -n "older apogee" docs/manual/sessions.md` names the new gauge sentence
- `grep -rn "ParksExactly\|parkedSession\|parkSession\|restoreSession" layout.md docs/manual .capcom/knowledge` prints nothing
- `grep -n "opening frame" layout.md` and `grep -n "your own host" layout.md` each print the new right-slot and footer sentences
- `grep -n "title. and .entries" .capcom/knowledge/glossary/glossary-identity/event-journal.md` names `model` and `context_window` beside them, or prints nothing
**Commit:** `docs: the journal frame names the run's binding; the watched view is one sessionView`
