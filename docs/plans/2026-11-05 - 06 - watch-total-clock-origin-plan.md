# Watch-mode clocks count from the watched session's own times

**Goal:** In Watch mode, the status line's `[total …]` readout and the per-phrase clock ("thinking · 12s") measure the watched session's work from the Event journal's own line times, not from the moment the watcher attached. At attach the total equals the time from the watched prompt's line to the journal's newest line, and from then on counts on the watcher's clock.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 4b43312b
**Closes:** watch-total-clock-origin

**Sources:**
- `.capcom/issues/watch-total-clock-origin.md`
- ADR 0103 (`capcom know show "ADR 0103"`); glossary `Watch mode`, `Exchange`, `Event journal`
- `internal/eventjson/writer.go` (`envelope.Time`, RFC3339Nano), `internal/eventjson/decode.go` (`Line.Time`)

**Ratified design calls:**
- **Phrase clock:** fixed in this plan too, from the same journal times as the total (owner, 2026-11-05).
- **Origin event:** the total counts from the top-level, non-interjection `prompt` line's time; an Interjection never moves it; `run_started`'s time is never the origin (it is the journal's birth).
- **Clock mapping:** a watch never compares a journal time with the watcher's own clock; it uses only differences between the journal's own stamps. Each folded watch batch records an anchor pair (the newest line time in that batch, the watcher's `time.Now()` when the batch folds); a line stamped t maps to the watcher's clock as anchorLocal − (anchorLine − t), clamped to at most `time.Now()` as a backstop. So an owner clock offset from the watcher's by any amount (an RTC-less Pi before its NTP step) cannot skew either clock; a step in the middle of the journal still can, and that is out of scope (owner, 2026-11-05; replaces the earlier "Future stamps" clamp call).
- **Owner path:** a session that is not watching keeps stamping clocks with `time.Now()`, unchanged.

**Standing requirements:**
- skills: coding-standards
- This box is a Pi 5: run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run <TestX> ./internal/tui/`, one run at a time; never a whole-package or pattern race run. Pass this on to every sub-agent.
- A duration in a test goes through `internal/testtime`, or is built as `time.Now().Add(-d)`; no fixed sleeps.

**Out of scope:**
- Exchanges opened by `Wake` or a recipe, which write no `prompt` line, so a watch never shows them as running (predates this issue).
- The ≤30 ms coalescing lag of token/reasoning line stamps.
- `/sessions`' `LiveSession.At` (journal birth), which stays as it is.
- A step of the owner's clock in the middle of the journal (an NTP correction after the prompt line was written), which still skews a watch's clocks; only a constant offset between the two clocks is handled.
- A watch attaching while the journal has been silent (prefill on a slow local model, a long tool call): both clocks undercount by that silent gap, since the mapping knows the owner's time only as of the journal's newest line. Accepted as the consequence of the ratified clock mapping; closeout does not claim the issue's first Acceptance bullet ("≈N at once") in full.

**Regression check (2026-11-05, 4b43312b):**
- 1: recast — clock mapping by journal-stamp differences (owner decision) replaces the future-stamp clamp; guards folded (zero line time is unset, only `next.since` moves, non-delta phrase event, interjection test clock).
- 2: guard folded — acceptance greps match only the new sentences, the rule's grep reaches `totalSuffix`'s sentence, the drift-test run is dropped.
- 1 (re-check): decision applied — Goal no longer claims "≈N at once" (header Goal too; silent-stretch undercount added to Out of scope); guards folded (each test's journal ends with a present-stamped line that does not move the slot; the `activityNow()` Approach bullet restated as the anchor mapping).
- 2 (re-check): decision applied — doc sentences count from the watched session's journal times and promise no exact match with the owner's display.

## 1. Watch clocks take the journal line's time as their origin

**What:**
Recast at the regression check (2026-11-05).
**Goal:** a watch that attaches to a Live session mid-Exchange shows, at attach, a total equal to the time from the watched prompt's line to the journal's newest line, and from then on counts on the watcher's clock; its phrase clock likewise counts from that phrase's journal line. Origins map onto the watcher's clock through differences between the journal's own stamps (the header's clock mapping), so an owner clock offset from the watcher's cannot skew them. The owner's own clocks are unchanged.
**Approach (assumed at the header base):**
- `watchItem` (internal/tui/watch.go) gains an `at time.Time`, set in `decodeWatchLine` from `eventjson.Line.Time`. It is the value's only producer; `foldWatchItem` is its only consumer.
- One seam owns the clock: a `Model` method (e.g. `activityNow()`) returns anchorLocal − (anchorLine − lineTime), clamped to `time.Now()`, when a line time is set, else `time.Now()`. The anchor (the batch's max non-zero item time, `time.Now()`) is computed in `foldWatchBatch` before its item loop and kept on `watchState`; an untimed batch (`readRecordOnly`) leaves `time.Now()`. `moveActivity` (internal/tui/activity.go) takes `next.since` from it instead of calling `time.Now()` directly. That covers every path into the slot: `setActivity`, `foldSlot` ← `foldActivity` ← `foldEventAgainst`, and `watchRunning`.
- `foldWatchItem` sets the current line time on the watch state (a field of the watch's own state, not of `sessionView`) for the duration of folding one item, and clears it afterwards. The owner path never sets it.
- `watchRunning` opens the top-level slot from the `prompt` item, so the total's origin is the prompt line's time. An Interjection prompt does not reopen the slot.
- This is a fix: defect `watch-total-clock-origin`. Both clocks now start at the attach instant.

**Regression guard.** Clock mapping: a watch never compares a journal time with the watcher's own clock; it uses only differences between the journal's own stamps. Each folded watch batch records an anchor pair (the newest line time in that batch, the watcher's time.Now() when the batch folds); a line stamped t maps to the watcher's clock as anchorLocal − (anchorLine − t), clamped to at most time.Now() as a backstop. So an owner clock offset from the watcher's by any amount (an RTC-less Pi before its NTP step) cannot skew either clock; a step in the middle of the journal still can, and that is out of scope. This supersedes the plan's earlier "Future stamps" call.
Also binding: `decodeWatchLine` is not the only producer — `readRecordOnly` (watch.go) builds `watchItem`s with no time; a zero line time is unset and falls back to `time.Now()`. Only `next.since` in `moveActivity` takes `activityNow()`; `slot.lastEvent` and `noteEngineHeard` stay on `time.Now()` (the stall guard is untouched). The phrase test uses a non-delta event (a `ToolCallEvent`), since the fixture's hour-long coalescing `Window` holds a delta off disk.
Decision (re-check, 2026-11-05): the relative clock mapping only knows the owner's time as of the journal's newest line, so a watch attaching while the journal has been silent undercounts both clocks by that silent gap; this is accepted as the consequence of the ratified mapping. Tests assert against the prompt-to-newest-line quantity (prompt stamped 5 minutes before the newest line → total of at least 5 minutes), never against wall-clock age. Each test's journal therefore ends with a line stamped at the present that does not move the slot (a depth-0 `UsageEvent`; a second `ToolCallEvent`/`ToolResultEvent` would restart the phrase clock, `foldActivity`).
**Files:** internal/tui/watch.go, internal/tui/activity.go, internal/tui/watch_test.go
**Read first:** internal/tui/activity.go — moveActivity, foldActivity; internal/tui/watch.go — foldWatchBatch, foldWatchItem, decodeWatchLine, watchState;
internal/tui/watch_test.go — reopenBound; internal/tui/model.go — totalSuffix
**Tests:** in `internal/tui/watch_test.go`, using `newWatchFixture` with `f.now` backdated before `f.reopen`:
- `TestWatchTotalCountsFromTheWatchedPrompt`: prompt stamped 5 minutes before the journal's newest line (a depth-0 `UsageEvent` stamped at the present), attach with `enterWatchOn`. The status line carries a `[total …]` of at least 5 minutes, exactly as `totalSuffix` formats it.
- `TestWatchPhraseClockCountsFromItsJournalLine`: a non-delta event (a `ToolCallEvent`) stamped 2 minutes before the journal's newest line (a depth-0 `UsageEvent` stamped at the present, which does not move the slot) gives a phrase clock of at least 2 minutes.
- `TestWatchClockIgnoresOwnerClockOffset`: a journal whose every line is stamped 3 hours ahead of (and, in a second case, 3 hours behind) the watcher, its prompt 5 minutes before its newest line, shows a `[total …]` of about 5 minutes, never negative, epoch-sized or hours-large.
- `TestWatchInterjectionKeepsTheTotalOrigin`: prompt at −5m, interjection at −1m, then a depth-0 `UsageEvent` at the present as the newest line, stamped through an `f.now` closure over a variable (`JournalOptions.Now` is captured at `reopenBound`); the total stays at least `05:00s`.
- The existing `TestStatusLineExchangeTotal*` still pass, proving the owner path is unchanged.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWatch(TotalCountsFromTheWatchedPrompt|PhraseClockCountsFromItsJournalLine|ClockIgnoresOwnerClockOffset|InterjectionKeepsTheTotalOrigin)' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStatusLineExchangeTotal|TestWatchIsolatesRoutesEveryFoldedMsg|TestSessionViewHoldsEveryFieldTheBoundariesReset' ./internal/tui/`
- The four new tests fail against the pre-item tree (bite check).
**Commit:** `fix(tui): watch clocks count from the watched journal's own times`

## 2. Docs say a watch's clocks are the watched session's

**What:**
**Goal:** the docs that describe the status line's `[total …]` readout and Watch mode say that, while watching, both clocks count from the watched session's own times.
**Approach (assumed at the header base):**
- `layout.md`: in the "How long the whole Exchange has run" paragraph (the one naming `totalSuffix`), add one sentence: in a watch the total counts from the watched prompt's journal time, and the phrase clock from its line's time.
- `docs/manual/sessions.md` §"Watching a live session": add one plain-language sentence that the status line's clocks show how long the watched session has been working, not how long you have been watching.
- `docs/manual/commands.md`: in the paragraph beginning "After the clock comes the Exchange's total", add a short clause pointing at that behaviour.
- Rule for the guard: every doc line that says the total counts "since your message went out" stays true for the owner and gets the watch clause only where it describes Watch mode. Find them with `grep -rn -i "message went out\|\[total" layout.md docs/manual docs/layout README.md`.
- Depends on item 1.

**Regression guard.** The base already satisfies the draft's greps (layout.md:1521 "a reader is watching", sessions.md:111 "clock order"), so acceptance greps the new sentences' own words: `grep -n -i "watched session" layout.md docs/manual/sessions.md docs/manual/commands.md | grep -i "total\|clock"` prints three lines after the item and none at base. The rule's grep is `grep -rn -i "message went out\|\[total" layout.md docs/manual docs/layout README.md`, which reaches layout.md:1795 (`totalSuffix`'s sentence). No drift test reads these files, so no test command runs.
Decision (re-check, 2026-11-05): the doc sentences describe the clocks as counted from the watched session's own journal times and must not promise an exact match with the owner's display; one short clause may say the clocks can run slightly behind the owner's right after attaching while the session is quiet.
**Files:** layout.md, docs/manual/sessions.md, docs/manual/commands.md
**Read first:** layout.md — "How long the whole Exchange has run" paragraph; docs/manual/commands.md — "After the clock comes the Exchange's total" paragraph;
docs/manual/sessions.md — "Watching a live session", "While you watch" bullet; internal/tui/model.go — totalSuffix
**Tests:** none (docs only). No drift test reads layout.md, sessions.md or commands.md.
**Acceptance:**
- `grep -n -i "watched session" layout.md docs/manual/sessions.md docs/manual/commands.md | grep -i "total\|clock"` prints three lines (one per file), and printed none at the base.
**Commit:** `docs(watch): the status line's clocks count the watched session's time`
