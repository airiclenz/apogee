# Plan: /thinking — a plain-text pane for the model's reasoning

- **Goal:** a `/thinking` command opens a non-modal report pane showing the plain reasoning text
  of the model — the main agent's when at top level, the viewed sub-agent's alone when a run view
  is open — with no JSON, prefixes or wire clutter (deliberately unlike `/inspect`, whose
  readable view is passages of the wire payload).
- **Date:** 2026-09-02 · **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:** `internal/tui/reasoning.go` (the retention seam this plan CASHES IN —
  "NOTHING IN THE VIEW READS THIS BUFFER … the retention seam a future reasoning display will be
  built on"; this is that display); `internal/domain/events.go:90` (`ReasoningEvent`,
  observation-only, Text untrusted → escape-strip) and `events.go:30` (`EventBase.Turn`, the Turn
  index every record is stamped with); `internal/agent/loop.go:707` (native path) and `loop.go:802`
  (`emitReasoningDelta`, inline path); `internal/tui/reportpane.go` (the report-pane doctrine — one
  module behind /usage and /inspect); `internal/tui/inspector.go` (the pane template: command, ring,
  scoping, key/mouse wiring, the eight thin wrappers at inspector.go:483-517); `internal/tui/popup.go`
  (row rows are TRUNCATED to the pane's inner width, popup.go:804 — never re-wrapped);
  `CONTEXT.md` "Thinking channel"; `layout.md:101-160,1715-1755`; ADR 0011, 0039, 0053, 0063;
  executed precedent `docs/plans/archived/2026-09-01 - 03 - inspector-readable-wire-plan.md`.

**Ratified design calls**
- **Record scope (owner, 2026-09-02):** full session history, always on — a bounded ring keeps the
  thinking of every completed turn, per agent, newest last, the pane opens on the newest; **no
  config key** (capture never touches the transcript, the model's context or the wire).
- **Sub-agent scoping (owner, 2026-09-02):** opened inside a run view the pane shows that run's
  thinking only (and names it in the title); opened at top level it shows the main agent's only.
- **One rendering:** the pane is plain wrapped text — no `ctrl+r` raw toggle (raw wire bytes are
  `/inspect`'s job), no prefixes, no turn metadata inside the body.
- **The tail is RETIRED, not kept beside the board (review + owner go-ahead, 2026-09-02):**
  `reasoningTail` exists for one stated reason — it is "the retention seam a future reasoning
  display will be built on" (reasoning.go:13; doc.go:441-443) — and the board is that display's
  retention, a strict superset of it (same `stripEscapes` entrance, same rune-safe `keepLastBytes`
  cut, same per-agent rule, larger cap, plus commit). Keeping both would ship a permanently unread
  buffer whose own file says why it should not exist. `internal/tui/reasoning.go` is therefore
  RENAMED to `internal/tui/thinking.go` and rewritten into the board; the alternative considered
  and rejected was keeping the tail and having the board's live record read it, which leaves two
  answers to "what has this agent been thinking" in one Model (AGENTS.md: prefer the best
  long-term architecture over lowest churn).
- **In-flight records are keyed by run (review, 2026-09-02):** a fan-out interleaves its
  delegates' chunks in one stream (ADR 0039), so ONE live record shredded each agent's turn into a
  record per interleaved chunk — heading spam in the pane, and a 64-record cap spent in seconds,
  which silently voids the ratified "every completed turn" scope. The board holds one in-flight
  record PER RUN and both boundary events are run-scoped.
- **Rows are composed at the pane's real width (review, 2026-09-02):** a fixed 96-column wrap plus
  the popup's truncation loses text irrecoverably on any terminal under ~100 columns, and this
  pane has no raw toggle to recover it from.

**Considered and out of scope (recorded so it is not re-asked):** `Agent.Snapshot()`
(internal/agent/agent.go:1065) already carries each committed Turn's reasoning on its assistant
message (`reasoning_content`, preserved in `domain.Message.extra` — hooks.go:105), so the pane
could read history from the engine instead of a view-side ring and would then survive resume. The
owner's ratified scope keeps capture view-side and unpersisted; this note exists so the next
reader does not re-open it.

**Regression check (2026-09-02, 8018ac96; re-checked after the 2026-09-02 review revision):**
- 1: recast twice — the item now RETIRES `reasoningTail`, so its narration sweep is no longer a
  sweep but a deletion plus a rewrite: `TestReasoningTailIsRenderedNowhere`
  (reasoning_test.go:203) is the guard whose premise this plan ends, and the item deletes it
  BY NAME rather than leaving a red test for the implementer to discover; `foldCase.wantReasoning`
  (fold_test.go:51) is RENAMED to `wantBoard`, not joined by it, and the runner at
  fold_test.go:382-383 re-pointed; doc.go:441-455, doc.go:616 and model.go:359 are rewritten
  rather than swept. fold.go's own header count ("the four folds a view update is made of",
  fold.go:12, and doc.go:614) is already stale at HEAD — `foldWire` is unlisted — and item 1
  corrects it to the five folds `foldEvent` actually runs.
- 2: guard folded — the wrap column is derived, not constant, so the guard is a LOSS test (no
  rune of a canned record is missing from the painted rows at width 80), not an exact-row-text
  test alone; `wrapReadable`/`cutReadable` gain a column parameter, which puts internal/tui/
  inspector.go in **Files:** and amends the "/inspect untouched" out-of-scope line (its callers
  pass `readableWrapColumn` and its rendering is byte-identical).
- 3: guard folded — the claimant entry is `paneClaim(Model.thinkingKey)`, the method expression
  every sibling rung uses (`paneClaim(m.thinkingKey)` does not compile in the package-level
  `keyClaimOrder` literal, where no `m` exists); `reportKind`'s three `if r == inspectReport`
  bodies (reportpane.go:71-124) FALL THROUGH to /usage, so a missed branch compiles and paints
  the wrong pane — they become exhaustive switches with a table test; the eight thin wrappers
  inspector.go:483-517 defines are enumerated rather than implied; a `/thinking` case joins
  `reportCases` (reportpane_test.go:27) and the `paneThinking` row joins
  `TestFrameOverlayBlocksAnswerForEveryPane` (reportpane_test.go:220); the writer's rule sweeps
  every prose site naming how many reports/panes share the transcript slot or the click chain
  (grep -in 'two reports|two of them|only panes of the transcript-side slot'
  internal/tui/reportpane.go layout.md docs) when the third report lands.
- 4: guard folded — the guard is the rule "every doc site enumerating the report panes or the
  while-running verbs beside /usage and /inspect" (`grep -n "usage" layout.md
  docs/manual/commands.md`); the README anchor re-pointed to the `/` command-menu sentence
  (README.md:132-135 — `/inspect` is named nowhere in README.md); CONTEXT.md:489's **Thinking
  effort** _Avoid_ drops its `/thinking` reservation in the same edit.

**Standing requirements**
- skills: coding-standards

**Out of scope**
- `/inspect` — its readable view, its raw toggle, its ring and `ui.inspector` are untouched. The
  ONE exception, made explicit: the shared `wrapReadable`/`cutReadable` helpers gain a column
  parameter in item 2; /inspect's callers pass `readableWrapColumn` and its rendering does not
  change by a byte.
- thinking in the transcript, or any new status-line readout beyond the existing activity line
- any config key (`ui.thinking` et al.) — capture is unconditional (ratified above)
- persisting the thinking ring into saved sessions or replaying it on resume
- turning on the popup module's `wrapRows` for reports — the report module's scroll arithmetic is
  row-indexed and one row is one line (`reportWindow`, `popupBudget`); variable-height rows would
  rewrite shared code /usage and /inspect also stand on. Rows are composed at the right width
  instead (item 2).
- a version bump (carried as a suggestion at the end; never an item)

---

## 1. Retire the reasoning tail; retain every Turn's reasoning in its place — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the rename is a `git mv` for both files (reasoning.go → thinking.go,
reasoning_test.go → thinking_test.go); the deletions of the old paths are already staged in the
index, so they are not listed under FILES (a `git add` on a vanished path errors).
NOTES (2026-09-02): fold_test.go's `foldCase.wantBoard` is asserted through a new
`liveThinking(thinkingBoard)` test helper defined in thinking_test.go — the table's rows each fold
one event from one agent, so the concatenation of live records is that run's record; the
interleave cases assert on the records themselves.
NOTES (2026-09-02): the byte-cap test appends 128 × 1152-byte chunks rather than the retired
test's per-byte loop — the cap grew 16×, so the old loop shape would have churned ~4 GB.

**What.** Recast at the regression check and again at the 2026-09-02 review. `git mv
internal/tui/reasoning.go internal/tui/thinking.go` and `reasoning_test.go` →
`thinking_test.go`, then rewrite the file: the tail this seam landed as becomes the BOARD it was
landed for. The name follows CONTEXT.md's own term for the channel ("Thinking channel"), which
is also the verb the pane is opened with.

One value struct on the Model, replacing `Model.reasoning reasoningTail` (model.go:363) with
`Model.thinking thinkingBoard` (ADR 0011; plain fields only):

    thinkingBoard{done []thinkingRecord; live []thinkingRecord}
    thinkingRecord{run runRef; turn int; text string}

`runRef` (transcript.go:164) is the {depth, spawn} identity the pane later scopes by; `turn` is
`domain.EventBase.Turn` off the chunk's own event (events.go:30 — the field `inspector.go:194`
already stamps its wire records from), and it is what the pane's heading spells.

`live` is one in-flight record PER RUN, found by scanning for `runOf(e.EventBase)` — a slice and
not a map, because a map would alias across the value-copied Model (ADR 0011) where a slice of
values does not, and because its length is bounded by the concurrent fan-out (ADR 0039) to a
handful. Binding rules, carried from the seam and corrected for concurrency:

- every chunk crosses `stripEscapes` — the one entrance (the rule the retired tail states at
  reasoning.go:24-27), so a display never paints ESC bytes a model wrote;
- `text` is bounded to the LAST `thinkingRecordCap = 64 << 10` bytes via `keepLastBytes`
  (rune-safe, the same cut the tail used; the constant is 16× the retired `reasoningTailCap`
  because this record is READ rather than merely retained, and 64 records of it is the ~4 MB
  ceiling the record cap below buys);
- a chunk lands on the record whose `run` matches its own, and opens one where none is live; a
  chunk whose `turn` differs from that run's live record COMMITS it and starts a new one (the
  belt beside the MessageEvent brace below);
- `StreamResetEvent` DROPS **that run's** live record (its Turn is superseded, events.go) —
  unscoped, it would destroy a sibling's in-flight text when one delegate retried;
- `MessageEvent` COMMITS **that run's** live record (the Turn is over; the canonical copy on the
  engine message is history's concern, not the view's) — unscoped, it would commit whichever
  agent happened to stream last;
- the worker-unwind boundary (`finishWorker`, model.go:1817 — a stop/fault emits no closing
  MessageEvent) and a new Exchange (`launchExchange`, commandrun.go:92) COMMIT EVERY live record
  and clear, replacing the `m.reasoning.reset()` call each site makes today: the run is over
  however it ended, and what it thought before dying is the point of the pane.

Committed records append to `done` in completion order — newest last, which is the order the pane
opens on. The board holds at most `maxThinkingRecords = 64` committed records: the OLDEST is
dropped.

Sole writer: `foldThinking` (renamed from `foldReasoning`) — one of the five folds `foldEvent`
runs (fold.go:44), placed with the other order-free folds; it touches nothing another fold
establishes. fold.go's own header says "the four folds a view update is made of" (fold.go:12) and
lists four, which was already wrong at HEAD — `foldWire` is a fold and unlisted — so this item
corrects the count and the list there and at doc.go:614.

The narration this item rewrites rather than sweeps, because the claim it makes is the one this
plan ends: reasoning.go:13 and :52 (moved into thinking.go), model.go:359 (`NOTHING RENDERS IT`),
doc.go:441-455 and doc.go:616. Each becomes the board's own statement: retention that the
`/thinking` pane reads, bounded and per-agent for the reasons the tail's three rules gave, with
no remaining unread buffer to explain. `doc.go`'s file map swaps `reasoning.go` for `thinking.go`.

**Files:** internal/tui/thinking.go (git mv from reasoning.go); internal/tui/thinking_test.go
(git mv from reasoning_test.go); internal/tui/fold.go; internal/tui/model.go;
internal/tui/doc.go; internal/tui/commandrun.go; internal/tui/fold_test.go

**Tests.** `go test ./internal/tui/ -run 'TestFoldThinking|TestThinkingBoard'` — table over:
append→commit→second-turn sequence; StreamReset drops only its own run's record; MessageEvent
commits only its own run's record; a stop path (finishWorker) commits every live record; a
two-agent interleave (A,B,A,B chunks) yields exactly TWO records, one per run, each holding that
run's whole text in order — the case the single-live-record shape got wrong; a same-run Turn
change commits; caps (byte cap keeps the last `thinkingRecordCap`; record cap drops the oldest).
The five retained tests from `reasoning_test.go` carry over renamed (strip-at-the-seam,
byte cap, rune-boundary cut, one-agent-at-a-time → now one-record-per-agent, boundaries).
`TestReasoningTailIsRenderedNowhere` (reasoning_test.go:203) is DELETED: its doc comment says
"the day a reasoning display lands, this test is the one that says so out loud", and this is that
day — the deletion is the statement it asked for, and item 2's rows are what replaces it.
`TestFoldEventCoversEveryEventVariant` (fold_test.go) gains its row; `foldCase.wantReasoning`
(fold_test.go:51) is renamed `wantBoard` and the runner (fold_test.go:382-383) asserts the board
instead of the tail — every existing row's expectation stands (the ReasoningEvent row's `hmm` is
now the live record's text). `TestDocMapNamesEveryFile` after the doc.go edit;
`TestModelNoBuilderByValue` with the new field present.

**Acceptance.** `go test ./internal/tui/`; `go build ./...`; `grep -rn "reasoningTail\|foldReasoning"
internal/` reports nothing.

**Regression guard.** The retirement is the risk this item carries, so it is enumerated rather
than swept: every reader of the retired field is named in **Files:** — `m.reasoning.reset()` at
model.go:1818 and commandrun.go:92, the fold at fold.go:44, the field at model.go:363, the
narration at doc.go:441-455 and :616 — and the acceptance grep is what proves none was missed.
The one test whose PREMISE this plan ends is deleted by name, not discovered red. The
value-copied-Model rule is load-bearing: byte and record caps keep the Model bounded (ADR 0011),
`live` is a slice of values rather than a map so no copy aliases another's board, and interleaved
delegate chunks must never concatenate into one record NOR shred into one record per chunk
(ADR 0039) — both pinned by the table above. The board is retention until item 2 and must not
become a surface here: no render path reads it yet.

**Commit:** `refactor(tui): retire the reasoning tail for a bounded per-run thinking board`

## 2. The thinking rows — plain text at the pane's own width — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the LOSS test paints the pane through `renderPopup` via a local
`paintThinkingPane` helper composing the spec in `reportSpec`'s shape, not through `m.View()` as the
item's Tests line words it — the pane's `reportKind`, command and frame wiring land in item 3, so
`m.View()` cannot paint it yet. The truncation path under test (`popup.go`'s `truncateToWidth` at
the pane's inner width) is the same one, and the test does fail against a constant 96-column wrap at
width 80 (verified by temporarily substituting `readableWrapColumn`).

**What.** New `internal/tui/thinkingpane.go` (the pane whole, as `usage.go` and `inspector.go`
each hold one pane whole; `thinking.go` from item 1 holds the board behind the fold):
`thinkingRows(column int) ([]popupRow, []popupRowKind)` renders the board as report rows — per
record oldest→newest: one heading row (`popupRowHeading`), then the record's text as plain rows.
Heading: `turn <n>` for the main agent, `<run label> · turn <n>` for a sub-agent (the label
`m.runLabel(rec.run.spawn)` spells — the wording `/inspect`'s scoped title already uses,
inspector.go:539). Body: the record's text split on newlines, each line wrapped at `column` the
way `wrapReadable` wraps (first row carries no prefix, continuation two spaces, so a wrap is
visibly a wrap and the model's own paragraphing survives), kind `popupRowPlain`. NO `·` prefixes,
no JSON, no tool-call passages, no per-record elision — the byte cap is the only bound; the text
IS the content.

`column` is the pane's REAL row budget for this frame, not a constant:

    popupInnerWidth(m.th, m.width) - popupRowIndent - scrollbarWidth

— the border frame (popup.go:1067), the two-cell marker lead every row carries (popup.go:786,880)
and the overflow bar's column (popup.go:701, model.go:1981), reserved whether or not the bar is
drawn so the wrap does not change under it. Floored at `minThinkingWrapColumn = 20`, below which
the frame will not seat the pane anyway. This is the review's blocker: popup rows are TRUNCATED
to the inner width (popup.go:804, `truncateToWidth`) and never re-wrapped, so a constant
96-column wrap silently cut ~20 characters off every line on an 80-column terminal, unreachable
in a pane with no raw toggle and no horizontal scroll. `/inspect` survives the constant because
its rows are wire records with `ctrl+r` behind them; a pane for reading prose does not.

To take the column, `wrapReadable(prefix, text string, column int)` and
`cutReadable(lead, segment string, column int)` (inspector.go:435-470) take it as a parameter —
`cutReadable` already computes `budget` from `readableWrapColumn`, so it is that constant becoming
an argument. /inspect's callers pass `readableWrapColumn` and its rendering is byte-identical.
The row list is therefore width-dependent: a resize recomposes it and the scroll offset lands
elsewhere in the text, which is the same correction `reportSpec`'s clamp already applies every
frame (reportpane.go:152).

Scope filter: `viewedRun()` zero → only `run.depth == 0` records; non-zero → only records with
`rec.run == viewedRun()` (defensive even though the claim route scopes the entry, item 3). The
scoped run's LIVE record, where one is in flight, renders at the TAIL of the filtered rows — its
own arrival position, the board's newest-last, so a fan-out's partial text is never seated
between committed records; no in-progress marker row.

Plus `thinkingContent() reportContent` (the reportpane.go:122 shape): title `thinking`, or
`thinking — <run label>` under a run view; hint `↑/↓ scroll · esc close` (no ctrl+r — one
rendering); rowCap as the inspector's (`maxInspectorRows`, inspector.go:119); where the filtered
rows are empty, the one body row exactly `no thinking recorded yet`. Depends on item 1.

**Files:** internal/tui/thinkingpane.go; internal/tui/thinkingpane_test.go;
internal/tui/inspector.go; internal/tui/doc.go

**Tests.** `go test ./internal/tui/ -run 'TestThinkingRows|TestInspectorReadable'` — table over:
empty board (the exact empty-state string), one main-agent record, child records under a fan-out,
both scope filters, the live record rendered at the tail, a multi-line record's wrapping, heading
spellings. Plus the LOSS test the derived column exists for: a canned record of long lines
rendered through `m.View()` at width 80 and at width 200, asserting every rune of the record's
text appears in the painted rows in order — a constant wrap fails it at 80. /inspect's existing
readable-rendering tests stand unchanged and are what pins the parameterised helper.

**Acceptance.** `go test ./internal/tui/`; `go build ./...`.

**Regression guard.** thinkingpane.go is a new non-test file, so `TestDocMapNamesEveryFile`
(docmap_test.go:12; internal/docmap/docmap.go:41-63) fails the next full `go test ./internal/tui/`
the moment it lands unless its map entry rides this item: `internal/tui/doc.go` is edited here,
`thinkingpane.go` beside the `thinking.go` entry item 1 renamed. The wrap helper is SHARED with
/inspect, so the out-of-scope line's exception is explicit above and /inspect's own tests are in
this item's acceptance — a helper change that altered the wire pane's rows fails there, not in
review. The rows must stay plain — clutter is what this pane exists to remove, and it returns one
prefix or heading at a time: assert the EXACT row text for a canned record (no `· thinking`, no
`json` fragments), so a rendering tweak cannot reintroduce prefixes or metadata without a test
naming the change. The live record's tail placement keeps the board's newest-last order in a
fan-out — a rule stated against an agent's last committed record would seat the partial between
committed records and break arrival order.

**Commit:** `feat(tui): render the /thinking pane's plain rows at the pane's own width`

## 3. `/thinking` — the command, the pane, the sub-agent scope — ✅ DONE (2026-09-02)

NOTES (2026-09-02): the item's Files list is a floor. `TestThinkingCommand`,
`TestThinkingOpensOnTheNewestRecord` and the `thinkingPaneModel` helper `reportCases` needs went
into internal/tui/thinkingpane_test.go — the pane's own test file, beside item 2's rows tests —
rather than into one of the three listed test files.
NOTES (2026-09-02): consequential edit — internal/tui/doc.go: made necessary by the third report
joining the module (the package narration said the reports were "two panes that can be up
TOGETHER", that reportpane.go was "written once and named twice", and that popup.go paints "those
three" overlays).
NOTES (2026-09-02): consequential edit — layout.md: made necessary by `paneThinking` joining the
give-way order, which made "the two panes below it" false; the count claim is corrected without
naming the new pane, which is item 4's work.
NOTES (2026-09-02): consequential edit — internal/tui/thinkingpane.go and
internal/tui/thinkingpane_test.go: two comments item 2 wrote deferred to "item 3" as a future
("item 3's claim route", "arrives with its command (item 3)") and now name the landed verb instead.
NOTES (2026-09-02): `reportKind` gained a `reportKinds` count sentinel (the `paneKinds` idiom) so
`TestReportKindsResolveDistinctly` walks every declared kind rather than a hand-written list a
fourth report could be left out of.

**What.** Open the pane as the third report. `reportpane.go`: `thinkingReport` reportKind after
`inspectReport`. Its three dispatch bodies — `pane()` (reportpane.go:71), `reportState()`
(reportpane.go:95) and `reportContent()` (reportpane.go:121) — are today `if r == inspectReport
{…}` with /usage as the FALL-THROUGH, so a third kind that misses one of them compiles and paints
/usage's state or content inside the /thinking pane. All three become exhaustive `switch r`
with a `default` that panics on an unknown kind, and `TestReportKindsResolveDistinctly` walks
every `reportKind` asserting a distinct `framePane`, a distinct `*reportPane` address and a
title that names the right pane. New `Model.thinkingPane reportPane` field; `reportContent` →
`m.thinkingContent()`; `pane()` → new `paneThinking` in `framePane` (model.go:3608, after
`paneInspector`, before `paneDropdown`).

The eight thin per-pane wrappers `inspector.go:483-517` defines are what tie a report to the
frame, the keyboard and the pointer, and `thinkingpane.go` gets all eight by the same template:
`renderThinking`, `thinkingSpec`, `thinkingKey`, `dismissThinking`, `thinkingPaneRect`,
`thinkingWindow`, `handleThinkingClick`, `thinkingWheel` — each one line, each delegating to the
shared module with `thinkingReport` filled in.

Command handler `runThinkingCommand` in `thinkingpane.go`, mirroring `runInspectCommand`
(inspector.go:160): opens via `m.reportState` (top = the last row, so the pane opens on the
newest record) + `m.layout()`. `command.go:250`: row `{name: "thinking", summary: "the model's
plain thinking — the main agent, or the viewed run", whileRunning: true, noRecall: true}`,
alphabetically between `/sub-agents-server` and `/undo`; `commandrun.go:287`: `case "thinking":
return m.runThinkingCommand()`.

`keyClaimOrder` (model.go:1245): one entry `{"thinking pane", paneClaim(Model.thinkingKey)}` —
the METHOD EXPRESSION form every sibling rung uses (`paneClaim(Model.inspectorKey)`,
model.go:1311), since `paneClaim(m.thinkingKey)` does not compile in a package-level literal
where no `m` exists — placed after "inspector pane" and before "run view", so with a run view
open, esc closes the pane first and the NEXT esc continues up the run view (runview.go:269's
order decides, unchanged); `thinkingKey` delegates to `reportKey(thinkingReport, msg)`. Non-modal
doctrine stands: the pane claims only esc + the four scroll keys; every printable key reaches the
box behind it.

Mouse: the click chain (mouse.go:415 usage, :423 inspector) asks the reports in the slot's draw
order — usage → inspector → thinking, with `handleThinkingClick` asked LAST — and mouse.go's
doctrine comment (mouse.go:414-423) extends from two reports to three; the wheel chain
(mouse.go:1391 usage, :1397 inspector, narrated at :1364-1365) gets the same one-notch wiring.

`openPanes` (model.go:3626) and `frameOverlays.block` (reportpane.go:252) gain the pane, as do
the remaining model.go frame sites a third report forces — the `transcriptSlotPanes` slot list
(model.go:2359), the `frameOverlays` field, its `height()` list and its composer
(model.go:2252-2309).

Guard tests updated in place: `TestKeyClaimOrderMatchesTheDocumentedPrecedence`
(keyclaim_test.go:12), `TestTranscriptSlotPanesStateTheStackingOrderOnce`
(reportpane_test.go:173), `TestOnlyResetAndPureUIVerbsAreNotRecallable` (command_test.go:355 —
pins the noRecall set BY NAME), and the name-pinned `commandSpecs` list `wantParsed`
(command_test.go:62) — every name-pin on the table breaks with the new row. Depends on items 1-2.

**Files:** internal/tui/reportpane.go; internal/tui/model.go; internal/tui/command.go;
internal/tui/commandrun.go; internal/tui/thinkingpane.go; internal/tui/mouse.go;
internal/tui/reportpane_test.go; internal/tui/keyclaim_test.go; internal/tui/command_test.go

**Tests.** `go test ./internal/tui/ -run 'TestCommandSpecsReadAlphabetically|TestKeyClaimOrderMatchesTheDocumentedPrecedence|TestTranscriptSlotPanesStateTheStackingOrderOnce|TestOnlyResetAndPureUIVerbsAreNotRecallable|TestFrameOverlayBlocksAnswerForEveryPane|TestReportKindsResolveDistinctly|TestThinkingCommand'`
— `TestReportKindsResolveDistinctly` walks every `reportKind` and asserts a distinct pane, a
distinct state pointer and its own content, so a fourth report cannot silently inherit /usage's.
`TestThinkingCommand` drives the input `/thinking` + ⏎ (the EXACT announced verb) with folded
reasoning events and asserts `m.openPanes().has(paneThinking)` (inspector_test.go:377 shape);
a second case opens it inside a run view and asserts the scoped title and rows; a third asserts
a printable key still reaches the box behind the open pane; a `/thinking` case joins `reportCases`
(reportpane_test.go:27) so its shared-body drivers (`TestReportKeysScrollEitherReportByOneArithmetic`,
`TestReportWindowIsThePaintersOwnAnswer`, `TestReportScrollClampsToTheLastFullWindow`) run over
all three panes, and `TestFrameOverlayBlocksAnswerForEveryPane` (reportpane_test.go:220) gains
the `paneThinking` row.

**Acceptance.** `go test ./internal/tui/`; `go vet ./internal/tui/`; `go build ./...`.

**Regression guard.** The module's fall-through is the trap this item is written around: three
`if r == inspectReport` bodies default to /usage, so the failure of a missed branch is a WRONG
PANE rather than a compile error — hence the exhaustive switches and
`TestReportKindsResolveDistinctly`, which is the guard a fourth report will inherit. The What
enumerates the remaining model.go frame sites a third report forces (`transcriptSlotPanes`
model.go:2359; the `frameOverlays` field, its `height()` list and its composer, model.go:2252-2309),
the eight thin wrappers a report needs (inspector.go:483-517), and the additional name-pinned
`commandSpecs` list in command_test.go:62. The click chain follows the slot's draw order — usage
→ inspector → thinking, thinking asked last. A report pane is the frame's lightest overlay, and
the guard is that it stays one: the pane claims exactly esc + scroll (a printable key must still
open a message — reportpane.go doctrine), the give-way order keeps /inspect and /usage ahead of
it, and the two-reports-together slot rule extends to three without a fourth claiming the slot.
The hand-built enumerations must meet the third pane too: a `/thinking` case joins `reportCases`
(reportpane_test.go:27) so the shared scroll/clamp/window body drives all three reports, and the
`paneThinking` row joins `TestFrameOverlayBlocksAnswerForEveryPane` (reportpane_test.go:220).
Item 3's guard likewise carries the report-count claims as a rule — every prose site naming how
many reports/panes share the transcript slot or the click chain (grep -in 'two reports|two of
them|only panes of the transcript-side slot' internal/tui/reportpane.go layout.md docs) is swept
when the third report lands. Pinned by the claim-order, command-table, reportCases and
frame-overlay tests this item updates.

**Commit:** `feat(tui): open /thinking as a report pane scoped to the viewed run`

## 4. Name the pane in the layout spec and the user docs — ✅ DONE (2026-09-02)

NOTES (2026-09-02): README anchor — the item offered "the `/` command-menu sentence or a Features
bullet"; the bullet was taken, seated after the `/undo` bullet whose shape it mirrors, since the
command-menu sentence names key gestures rather than verbs.
NOTES (2026-09-02): the "usage" sweep the guard prescribes found three further layout.md
enumerations beyond the give-way sentence — the height-budget pane list (layout.md:105), the
overflow-bar pane list (:545) and the report-only verbs (:2140) — and the commands.md while-running
sentence (:17); all four gained `/thinking`.
NOTES (2026-09-02): consequential edit — layout.md (the `/inspect` popup's click-chain sentence):
made necessary by the third report joining the slot's draw order, which made "the report is asked
first where both are up" ambiguous; it now names `/usage` as the first and `/thinking` as the last.
NOTES (2026-09-02): consequential edit — docs/adr/0050-...-canonical-wire-mapping.md: made
necessary by the verb landing under the name that ADR reserved. Two dated markers record that the
reservation is spent (decision §, and the deferred-display rejected-alternative); the decision
itself and the `/thinking`-as-effort-name rejection are untouched.
NOTES (2026-09-02): the guard's `grep -rn "reasoning" CONTEXT.md layout.md docs/` walk found no doc
claiming a retention seam nothing reads — item 1's narration lived only in Go files.

**What.** `layout.md`: a new `## The /thinking popup` section beside `## The /inspect popup`
(layout.md:1715) — non-modal, transcript-slot, opens on the newest record, plain-text rows wrapped
to the pane's own width, no raw toggle, scoped to the run view when one is open — and the
give-way sentence (layout.md:139-153) gains `/thinking` in its walked position.
`docs/manual/commands.md`: a table row for `/thinking` beside `/inspect`'s (commands.md:32), `✅`
in the while-running column. `README.md`: the verb joins the `/` command-menu sentence
(README.md:132-135) or a Features bullet — README.md names `/inspect` nowhere, so there is no
`/inspect` entry to anchor on. `CONTEXT.md`: a **Thinking pane** term after **Inspector**
(CONTEXT.md:289) — the Driver-side plain view of the **Thinking channel**
(`domain.ReasoningEvent`), opened with `/thinking`, scoped by the run view; cross-link both
neighbours; the **Thinking effort** _Avoid_ (CONTEXT.md:489) drops its "/thinking" reservation
clause in the same edit and cross-links the new term — the item's grep guard already sweeps it,
since the line names /thinking. No ADR: no door-keeping invariant of ADR 0031 is touched and the
surface is ADR 0053's report-pane doctrine, extended, not changed. The tail's retirement (item 1)
is an internal seam and named in no user doc; it belongs to `CHANGELOG.md` under `[Unreleased]`
with the feature, not to the manual. Depends on items 1-3.

**Files:** layout.md; docs/manual/commands.md; README.md; CONTEXT.md

**Tests.** Prose only — `grep -c "/thinking" layout.md docs/manual/commands.md README.md CONTEXT.md`
reports ≥1 per file.

**Acceptance.** The greps above; `go build ./...` (docs-only, tree still builds).

**Regression guard.** Announced names must stay in step with what the program emits: the verb
spelled in every doc is the exact `commandSpecs` name `thinking`, and any future rename must
sweep these files — the rule is "every doc site naming the verb", found by
`grep -rn "/thinking" layout.md docs README.md CONTEXT.md`, never a closed list. The edit list
is not closed on siblings either: every doc site enumerating the report panes or the
while-running verbs beside /usage and /inspect gains /thinking too, found by
`grep -n "usage" layout.md docs/manual/commands.md` — never a closed list of sites. And nothing
in the docs may still promise a retention seam nothing reads: `grep -rn "reasoning" CONTEXT.md
layout.md docs/` is walked for a claim item 1 ended.

**Commit:** `docs(layout,manual,readme): specify the /thinking pane`

---

**Suggested version bump.** minor — a new user-facing command and pane (v0.20.0 at the next
release cut). Not performed by this plan; the owner decides.
