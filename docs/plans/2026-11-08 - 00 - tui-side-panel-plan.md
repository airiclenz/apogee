# TUI side panel — plan

- **Goal:** ship the side panel of ADR 0110: a fixed 36-column column right of the transcript slot,
  shown on wide terminals, folding sub-agents, the task list, changed files and usage from engine
  Events. Engine Events land first, then the Driver-neutral fold, settings, geometry, paint, pointer,
  command and replay.
- **Date:** 2026-11-08
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** 49d34cff
- **Sources:** `.capcom/knowledge/decisions/adr-0110.md` and its children (`capcom know show "ADR 0110" --with-children`);
  `docs/layout/side-panel-layout.md`; `layout.md` §"The side panel"; glossary Note `side-panel`;
  ADR 0011, ADR 0030, ADR 0031, ADR 0010.
- **Closes:** tui-side-panel
- **Ratified design calls** (owner, grill 2026-11-07 and plan writing 2026-11-08):
  - **Data:** every section folds from engine Events; missing facts get additive Events first.
  - **Resume:** rebuilds from the session record's stored entries; sub-agents starts empty.
  - **Usage split:** a context-cost Event at each Turn's end; history = estimate − standing rows.
  - **Geometry:** right side, rows above the session-name rule; 36 cols; `auto` gate transcript ≥ 100,
    `on` floor ≥ 80, thresholds always count the scroll-bar column.
  - **Stability:** the column moves only on a threshold crossing or a `ui.side-panel.show` change.
  - **Config keys:** ui.side-panel.show + ui.side-panel.sections (owner, 2026-11-08).
  - **Sections:** `pinned|auto|hidden` list `ui.side-panel.sections`, default all `auto`:
    sub-agents, tasks, changed-files, usage; overflow folds to header rows, never scrolls.
  - **Pointer:** body click opens the detail view; header pin glyph toggles pin and writes back.
  - **Command:** `/panel on|off|auto`, `/panel pin|unpin|hide <section>`; no key binding in v1.
  - **Current task:** the first unfinished task (the task list stores only done / not done).
- **Standing requirements:**
  - skills: coding-standards
  - Pi box: tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`;
    never a package pattern, never two runs at once.
  - A new file in `internal/tui`, `internal/config` or `internal/domain` is named in that package's
    `doc.go` (`internal/docmap` Check runs in all three).
  - The Model stays under `TestModelFitsTheStack`; panel state sits in `sessionView` behind a pointer.
- **Out of scope:** MCP and advice sections; a key binding; keyboard focus and ←/→ navigation;
  panel scrolling; `terminal` writes in changed files; daemon/headless rendering of the panel.
- **Regression check (2026-11-08, 49d34cff):**
  - 1: recast — emit only from appendToolResult after the ContextEstimateEvent; sweep edits, count, amend-1108, alias.
  - 2: recast — `at` always written (null when zero); stamped in emitSubAgentPhase.
  - 3: guard folded — emitTurn in agent.go, eventBaseOf, run.jsonl re-record, table-row bite; count re-bumped.
  - 4: recast — runs keyed by the TUI's run identity; guard folded (self-named rows, Maintenance, fake clock).
  - 5: recast — keys `ui.side-panel.show` + `ui.side-panel.sections`; guard folded (four drift tests, key count).
  - 6: guard folded — fakeSettingsHost, edits.refresh, config doc.go.
  - 7: guard folded — join after stackTranscriptSlot, frame tests, driven tests pin `off`; supersedes layout.md breadcrumb column.
  - 8: guard folded — width by pane slot, mouse.go hit map, testUIPrefs `off`; supersedes popup.go's full-width contract.
  - 9: guard folded — nil-safe copy-on-write State, reset at both boundaries; yields to doc.go's value-copy rule.
  - 10: guard folded — run-identity scope, fill equals contextGauge.
  - 11: guard folded — panel hit before pointerPanes, claims every cell, setExpanded not toggleTaskListFold.
  - 12: recast — yields to watchVerbs' view-only rule; guard folded (grammar pin, resume doors, sources).
  - 13: guard folded — case-insensitive, wrap-proof doc rule.
  - 1 (round 2): guard folded — twenty-(four|six) sweep over both announced counts; fold-test Acceptance line.
  - 3 (round 2): guard folded — same twenty-(four|six) rule as item 1, re-bumping item 1's counts.
  - 4 (round 2): guard folded — sanitize import, DelegateLabel's stripped-name rule, zero `At` falls back to `now`.
  - 5 (round 2): guard folded — config_test.go literals, owns/everyKeyFileConfig, comparable string sections; SaveConfigSetting unchanged.
  - 6 (round 2): recast — the writer owns the whole `ui.side-panel` block; settingsHost.Write routes both keys to it.
  - 12 (round 2): guard folded — usage seeded after both doors restore totals, one write key both sides, last non-error card per run.

## 1. Task-list Event — ✅ DONE (2026-11-08)

NOTES (2026-11-08): deviation — internal/tools/task_list.go: added the exported `TaskListToolName` const (the house `XToolName` pattern beside FanOutToolName/WriteFileToolName) so dispatch keys the emit on it instead of re-spelling "task_list"; the tool spec now reads its name from it.
NOTES (2026-11-08): deviation — internal/agent/tasklist_test.go: the item's two agent tests (plus a refused-call-emits-nothing assertion) live in the existing task-list test file, driving the real tools.NewTaskList tool; the plan's Files line did not name a test file for them.
NOTES (2026-11-08): consequential edit — cmd/apogee/headless_test.go: made necessary by the new kind (TestHeadlessFormatJSONStreamsEveryEvent enumerates one emitted Event per default-stream kind; its count comment now says twenty-four).
NOTES (2026-11-08): TestKindsAreTwentySix renamed TestKindsAreTwentySeven (count 27), following the amend-1102 precedent; `data.items` encodes `[]` (never null) for a cleared list and decodes an empty list as nil Items, recorded in Decode's lossy-points comment and lossyView.

**What:**
Recast at the regression check (2026-11-08).
**Goal:** the engine emits `domain.TaskListEvent{EventBase, Items []domain.TaskItem{Text, Done}}`
whenever an agent's task list changes, at every depth, and the event round-trips through eventjson.
**Approach (assumed at the header base):** add the variant to `internal/domain/events.go` (embed
`EventBase`); emit it from `(*Agent).appendToolResult` (`internal/agent/dispatch.go`) after the
`ContextEstimateEvent` emit when `call.Tool == "task_list"` and the result is not an error.
Stamp with `a.base(turn)`. Mirror in `internal/eventjson` (`kind*` const, `Kinds()`, `encode`
case, `*Data`, `eventDecoders`), alias in `apogee.go`, list the kind in `docs/manual/headless.md`,
add a `foldCases` row (`internal/tui/fold_test.go`) that the TUI ignores for now.
**Regression guard.** Emit TaskListEvent only from appendToolResult (successful task_list), AFTER the
ContextEstimateEvent emit, never between ToolResultEvent and it; NO emit from ClearContext or restoreState (they run
on the Update goroutine and an Emit there deadlocks via programRef.send; the Driver resets its fold on /clear via
resetSessionView and resume rebuilds from stored entries, item 12). Name the hand-listed test edits (variants map in
TestEncodeMapsEveryEventVariant, a roundTripEvents fixture in decode_test.go, a TestEncodeJSONGolden case) and add
internal/eventjson/encode_test.go and decode_test.go to Files. Bump every announced count of Event kinds, both the line-kind count and the engine-event count (grep -rn -i -E
'twenty-(four|six)' internal/eventjson docs/manual .capcom/knowledge/decisions/adr-0075*; headless.md says
"Twenty-four" engine events) and file Note adr-0075-amend-1108
for the new kind, following amend-1101/-1102, named on ADR 0075's Status line and reading-order list. Alias
`TaskItem = domain.TaskItem` in apogee.go beside the variant alias. Acceptance: one package per `go test` line, each
with -run, and one runs `TestFoldEventCoversEveryEventVariant` in internal/tui (the item edits fold_test.go).
**Files:** internal/domain/events.go; internal/agent/dispatch.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; apogee.go; docs/manual/headless.md; .capcom/knowledge/decisions/adr-0075.md; .capcom/knowledge/decisions/adr-0075/adr-0075-amend-1108.md (new Note); internal/tui/fold_test.go
**Read first:** internal/agent/dispatch.go — appendToolResult, contextEstimateEvent; internal/tasklist/tasklist.go — List.Items, Item; internal/eventjson/encode.go — Kinds;
internal/eventjson/encode_test.go — TestEncodeMapsEveryEventVariant, TestKindsAreTwentySix, TestEncodeJSONGolden; internal/eventjson/decode_test.go — roundTripEvents, TestDecodeRoundTripsEveryKind;
internal/tui/fold_test.go — foldCases, TestFoldEventCoversEveryEventVariant; apogee_alias_internal_test.go — TestEveryDomainEventVariantIsAliased; docs/manual/headless.md — "The twenty-six line kinds"
**Tests:** `TestTaskListEmitsTaskListEvent` (the event follows the `ContextEstimateEvent`) and
`TestDelegateTaskListEventCarriesChildDepth` (agent); update `TestKindsAreTwentySix` to the new count;
a `TestEncodeMapsEveryEventVariant` map entry, a `roundTripEvents` fixture, a `TestEncodeJSONGolden` case.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestKinds|TestEncode|TestDecode' ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Event|TestDocMapNamesEveryFile' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TaskList' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEveryDomainEventVariantIsAliased' .`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualListsEveryEventLineKind' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFoldEventCoversEveryEventVariant' ./internal/tui/`
**Commit:** `feat(engine): emit a typed task-list Event at every depth`

## 2. Sub-agent start time — ✅ DONE (2026-11-08)

NOTES (2026-11-08): consequential edit — .capcom/knowledge/decisions/adr-0075.md: made necessary by extending adr-0075-amend-1108 with `data.at` (the record's Status line names what each amendment adds)
NOTES (2026-11-08): tests landed in internal/agent/delegationphase_test.go (TestSubAgentPhaseEventCarriesAt, beside the other phase tests) and internal/eventjson/decode_test.go (TestDecodeRoundTripsSubAgentPhaseAt, TestDecodeRejectsAnUnparsableSubAgentPhaseAt, and an `At` on the round-trip started phase); neither file is on the item's Files line
NOTES (2026-11-08): a malformed `data.at` is a decode error, like a malformed envelope `time`; Decode's lossy-form doc now says At comes back without its monotonic reading and in a fixed zone of its offset

**What:**
Recast at the regression check (2026-11-08).
**Goal:** `domain.SubAgentPhaseEvent` carries `At time.Time`, set on both phases, so any Driver can
compute a run's elapsed time; eventjson carries it as `data.at`, always written (`null` when zero).
**Approach (assumed at the header base):** add the field; stamp it once in `emitSubAgentPhase`
(`internal/agent/dispatch.go`), from the agent's clock seam if one exists, else `time.Now()`; add it to
the eventjson phase `*Data` struct.
Depends on item 1 (shared files).
**Regression guard.** `at` is always written — an RFC3339Nano string, or null via *string when zero (the RunFinished
`error` precedent); never omitempty (ADR 0075 decision 3). Stamp `event.At = a.now()` (or the agent's existing clock
seam) once inside (*Agent).emitSubAgentPhase in internal/agent/dispatch.go, not at the eight construction sites.
Update the three sub_agent_phase TestEncodeJSONGolden cases with `at` last; document data.at in the
docs/manual/headless.md sub_agent_phase row and in adr-0075-amend-1108 (created by item 1, extended here). Files add
encode_test.go, headless.md, adr-0075-amend-1108 Note; one package per Acceptance line.
**Files:** internal/domain/events.go; internal/agent/dispatch.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; docs/manual/headless.md; .capcom/knowledge/decisions/adr-0075/adr-0075-amend-1108.md
**Read first:** internal/agent/dispatch.go — emitSubAgentPhase, childBase; internal/agent/agent.go — sharedHandles.now; internal/agent/background.go — backgroundHost;
internal/agent/workflowspawn.go — workflowSpawner.Spawn; internal/eventjson/encode.go — subAgentPhaseData; internal/eventjson/decode.go — subAgentPhaseEventFrom;
internal/eventjson/encode_test.go — TestEncodeJSONGolden; docs/manual/headless.md — sub_agent_phase row
**Tests:** `TestSubAgentPhaseEventCarriesAt` (agent); eventjson round-trip with and without `At` (zero
writes `null`); the three `sub_agent_phase` `TestEncodeJSONGolden` cases updated with `at` last.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEncode|TestDecode' ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SubAgentPhase' ./internal/agent/`
**Commit:** `feat(engine): stamp sub-agent phase Events with their time`

## 3. Context-cost Event at Turn end — ✅ DONE (2026-11-08)

NOTES (2026-11-08): The ContextCostEvent is emitted right AFTER each boundary's TurnEvent under the same EventBase, so the TurnEvent opens the boundary and the cost follows it. The plan left the order open. The headless manual and ADR 0075 amendment 1108 state this order.
NOTES (2026-11-08): The `context_cost` line's data reuses the run_finished frame's exported `eventjson.ContextCost`/`ContextCostRow` types, so a reader parses both with one type. On the line, `rows` is always `[]`, never null, because a line always has a session behind it. The frame keeps null rows.
NOTES (2026-11-08): TestTurnEndEmitsContextCostEvent lives in internal/agent/contextcost_test.go, which the plan's Files line does not name. It is a new test beside the other ContextCost tests, and the `ContextCost` Acceptance pattern matches it.
NOTES (2026-11-08): consequential edit — cmd/apogee/headless_test.go: made necessary by the new variant. TestHeadlessFormatJSONStreamsEveryEvent builds its expectation from Kinds(), so its emission list gains a ContextCostEvent. Its comment now counts twenty-five default-stream variants.
NOTES (2026-11-08): consequential edit — internal/agent/contextcost.go: ContextCost's doc comment now names emitTurn as the engine's own idle-boundary reader. No helper was added: the emit is inline in emitTurn, which is the one call site.
NOTES (2026-11-08): TestManualListsEveryEventLineKind now asserts a "| `<kind>` |" table row instead of a bare backticked token, as the plan asks. Every existing kind already has such a row.
NOTES (2026-11-08): A non-race run of all of ./internal/agent fails TestDispatchGroup_OnePipelineAtEveryWidth. It fails the same way at HEAD a1b3a9e4 with this item's changes stashed. Item 2's wall-clock SubAgentPhaseEvent.At makes the width-1 and width-2 per-call events differ.
NOTES (2026-11-08): consequential edit — .capcom/knowledge/decisions/adr-0075.md: made necessary by extending adr-0075-amend-1108 with the context_cost kind (the Status line names what each amendment adds; decision 4's count is now twenty-eight)

**What:**
**Goal:** at the end of every Turn, at every depth, the engine emits
`domain.ContextCostEvent{EventBase, Cost domain.ContextCost}` with the standing rows (system
content, tool menu, tool instructions) the agent's `ContextCost()` reports; it round-trips through
eventjson.
**Approach (assumed at the header base):** emit from the agent where the Turn's end is signalled,
reusing `(*Agent).ContextCost()` (`internal/agent/contextcost.go`) — no second computation. Mirror,
alias, headless doc and `foldCases` row exactly as item 1. Depends on item 2.
**Regression guard.** item 3 re-bumps the kind count item 1 set and extends adr-0075-amend-1108 with the
context_cost kind; one package per Acceptance line. Same twenty-(four|six) rule as item 1, re-bumping what item 1
set: `grep -rn -i -E 'twenty-(four|six)' internal/eventjson docs/manual .capcom/knowledge/decisions/adr-0075*`.
Emit inside `(*Agent).emitTurn` (`internal/agent/agent.go`, not `loop.go`) so every boundary — Step's, Run's, the
step-capped fallback — gets exactly one; teach `eventBaseOf` (`subagent_test.go`) the variant; re-record `run.jsonl`
with `-update` (each `turn` line gains one). headless.md already names `context_cost` (run_finished member), so
`TestManualListsEveryEventLineKind` asserts the table row `` | `<kind>` | `` instead.
**Files:** internal/domain/events.go; internal/agent/contextcost.go; internal/agent/agent.go; internal/agent/subagent_test.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; apogee.go; docs/manual/headless.md; .capcom/knowledge/decisions/adr-0075/adr-0075-amend-1108.md; cmd/apogee/testdata/eventlines/run.jsonl; cmd/apogee/docs_eventlines_test.go; internal/tui/fold_test.go
**Read first:** internal/agent/agent.go — emitTurn; internal/agent/contextcost.go — ContextCost; internal/agent/subagent_test.go — eventBaseOf;
cmd/apogee/e2e_eventlines_test.go — TestE2EEventLinesGolden, eventLinesRedactions; internal/eventjson/writer.go — ContextCostRow;
cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind; internal/tui/fold_test.go — foldCases
**Tests:** `TestTurnEndEmitsContextCostEvent` (agent, a step-capped boundary included); kinds-count test
bumped again; item 1's sweep entries; an `eventBaseOf` case; `run.jsonl` re-recorded; the table-row check.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestKinds|TestEncode|TestDecode' ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'ContextCost|TestSubAgent_' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEveryDomainEventVariantIsAliased' .`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualListsEveryEventLineKind|TestE2EEventLinesGolden' ./cmd/apogee/`
**Commit:** `feat(engine): emit the standing context cost at each Turn's end`

## 4. Driver-neutral panel fold — ✅ DONE (2026-11-08)

NOTES (2026-11-08): API as built: `New(now)`, `SetFallbackWindow(n)`, `Fold(domain.Event)`, `SetTasks(run, items)`, `AddWrite(run, depth, path, stat *domain.DiffStat)` (nil stat = the write carried no Edit regions), `SetUsage(run, totals domain.Usage, used int)` (resume seed: totals kept under later live Cumulative readings, used = last observed fill); queries `SubAgents()` (every run whatever the scope, decision 12), `Tasks(scope)`, `ChangedFiles(scope)`, `Usage(scope)` returning `View[R]{Rows, Summary, IsLive}` / `UsageView`; queries are nil-safe, mutators are not.
NOTES (2026-11-08): `RunKey` is keyed by the run id alone when one is set, else by (depth, spawning call id) — the stated runRef semantics; the zero RunKey is the top-level run, and `RunOf(domain.EventBase)` is the one constructor (items 10-12 build a key from a runRef through it).
NOTES (2026-11-08): fill mirrors the TUI's contextGauge: the top-level scope shows the newer ContextEstimateEvent over the reading (IsEstimate), a delegated run its reading alone; top-level Usage totals sum every run's base + latest Cumulative, a run scope its own.
NOTES (2026-11-08): the two tool-surface ContextCostRow names ("tool menu", "tool instructions") are local constants: internal/agent's own are unexported and the package may not import it — a rename there would silently move tools into the system share.
NOTES (2026-11-08): sidepanel.go is 556 lines (doc-heavy), over the ~400-line [should] guideline; kept as one file because the item's Files line names only it.
NOTES (2026-11-08): no CHANGELOG entry — the package has no user-visible surface until the panel paints (item 13 documents the feature).

**What:**
Recast at the regression check (2026-11-08).
**Goal:** a new leaf package `internal/sidepanel` (imports only `internal/domain`, `internal/sanitize`
and the standard library) folds Events into per-run panel state and answers, for a given run scope, each section's
rows, summary line and live predicate.
**Approach (assumed at the header base):** one deep module: `State` with `Fold(domain.Event)` plus
typed entry points (`SetTasks(run, items)`, `AddWrite(run, depth, path, stat)`,
`SetUsage(run, …)`, `run` the run key of the guard below) that the Event fold and item 12's resume adapter both call. Sections:
sub-agents (from `SubAgent*Event`, `At` for elapsed, per-run fill from `UsageEvent`), tasks
(`TaskListEvent`; current = first unfinished), changed files (`ToolResultEvent.WriteTarget` with
`!Result.IsError`, `+/-` from the summary's `EditRegions.Stat()`, depth > 0 marked), usage
(`UsageEvent`, `ContextEstimateEvent`, `ContextCostEvent`; history = estimate − standing rows).
Live rules: sub-agents while any runs and 30 s after the last ends; tasks while one is unfinished;
changed files once one write landed; usage at fill ≥ 50%. The clock is injected (`now func() time.Time`).
**Regression guard.** sidepanel keys runs by the same identity the TUI uses (runRef semantics: RunID, falling back
to depth+spawn when RunID is empty) expressed in domain terms so the package still imports only internal/domain and internal/sanitize;
items 10-12 query by that key.
A self-named delegation gets no `SubAgentNamedEvent`: name it from `ToolCallEvent` with `SpawnRunID != ""` (args `name`,
else the task's first line), deciding on the escape-stripped name exactly as `title.DelegateLabel` does (sanitize, which
imports only "strings"), so a control-character-only name falls back to the task. A zero `At` (a pre-item-2 line
decoded from `null`) falls back to the injected `now` at fold time. Fill and live predicate skip `Maintenance` readings
(Cumulative keeps them); a fallback window is a State input used when `ContextWindow` is 0. The linger test injects a
fake `now` advanced by `testtime.Fixed(30*time.Second)`, never the wall clock.
**Files:** internal/sidepanel/sidepanel.go; internal/sidepanel/sidepanel_test.go
**Read first:** internal/domain/events.go — SubAgentPhaseEvent, ToolCallEvent.SpawnRunID, ToolResultEvent.WriteTarget, UsageEvent (Maintenance, ContextWindow);
internal/tui/transcript.go — runRef, runOf; internal/title/title.go — DelegateLabel; internal/tui/toolregistry.go — subAgentTarget, subAgentName;
internal/domain/toolsummary.go — EditRegions.Stat; internal/testtime/testtime.go — Fixed
**Tests:** table tests per section, per live rule (linger with an injected `now` advanced by `testtime.Fixed`),
per-run scoping (RunID and the depth+spawn fallback), self-named sub-agent row, a control-character-only
name falling back to the task, a zero `At` row (elapsed from the injected `now`), Maintenance reading
skipped for fill, `ContextWindow` 0 fallback, refused write ignored, history arithmetic.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/sidepanel/`; `go vet ./internal/sidepanel/`
Depends on item 3.
**Commit:** `feat(sidepanel): fold engine Events into side-panel state`

## 5. Settings keys — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the side-panel vocabulary (SidePanelShow, SidePanelSection, SidePanelMode, SidePanelEntry, SidePanelSections with Parse/Spell) lives in a new file internal/domain/sidepanelprefs.go (+ its _test.go) rather than in uiprefs.go, which was already 808 lines; UIPrefs fields, UIKey* constants, Validate and Set stay in uiprefs.go; the new file is named in internal/domain/doc.go.
NOTES (2026-11-08): design call: a section `ui.side-panel.sections` leaves out is appended after the listed ones in `auto`, in default order (so a list written before a later section exists still loads); `[]` therefore reads as the default. The parse always returns every section, and Set/the file pass land that full canonical spelling.
NOTES (2026-11-08): the section list is judged at the file pass (fileSidePanelSections, ui.footer.colors' posture) and stored canonical; the show word is judged by UIPrefs.Validate (ui.spinner's posture). The YAML shape is a list of single `section: mode` maps, so the canonical flow spelling is itself valid YAML.
NOTES (2026-11-08): consequential edit — internal/config/configwrite_scalar_test.go: made necessary by making the depth-3 `ui.side-panel.show` Editable; TestSpliceScalarSettingRoundTripsEveryEditableKey now skips keys deeper than scalarPathDepth (writableKey already refuses them), leaving them to item 6's block writer. SaveConfigSetting and its depth guard are unchanged, so until item 6 lands a /settings commit of ui.side-panel.show is refused "nested too deeply"; the manual already says /settings edits it.
NOTES (2026-11-08): template: `side-panel:` with a live `show: auto` and a commented `sections:` example, after `git-fetch`; ui.side-panel.show is last in uiKeys (fifteen keys), sections kept out of uiKeys like the footer keys. internal/domain/doc.go's "Twenty-eight files" count was already stale (34 non-test files) before this item and is left as is.

**What:**
Recast at the regression check (2026-11-08).
**Goal:** `ui.side-panel.show` (`auto|on|off`, default `auto`, Editable) and `ui.side-panel.sections`
(structured ordered list of `<section>: pinned|auto|hidden`, default all four `auto`) are declared,
parsed, validated, in the template, live-applied as renderer-owned, and documented.
**Approach (assumed at the header base):** `internal/domain/uiprefs.go` (`UIKey*`, `uiKeys`,
`DefaultUIPrefs`, `Validate`, `Set`); the sections list mirrors `FooterRows` (`ParseFooterRows` /
`SpellFooterRows`, kept out of `uiKeys` like `UIKeyFooterRows`); `internal/config` (`uiConfig`,
registry row `KindStructured`, `defaults/config.yaml`); `settingsApplyLocal` cases in
`internal/tui/settingsapply.go`; `docs/manual/configuration.md` §"The terminal UI — `ui:`".
**Regression guard.** the keys are `ui.side-panel.show` (auto|on|off, default auto, Editable) and
`ui.side-panel.sections` (structured list) — the owner ratified this spelling 2026-11-08 because YAML cannot hold a
`ui.side-panel` scalar beside a `sections` child; add it to the header's Ratified design calls (`**Config keys:**
ui.side-panel.show + ui.side-panel.sections (owner, 2026-11-08).`) and replace every `ui.side-panel` scalar mention
in the plan with `ui.side-panel.show`. New files in internal/config and internal/domain are named in that package's
doc.go too (docmap.Check runs there) — fold that into the header's standing requirement line about doc.go.
The keys redden `TestSettingsRowsFormatEffectiveValues` (a value per key), `TestEveryEditableSettingKeyHasAnApply`
(`ui.side-panel.show` joins `settingKeysAppliedByTheRenderer`), `TestUIRowsLandThroughUIPrefsSet` and
`TestSettingsApplyLocalLandsEveryUIKeyThroughSet` (a value pair each): update all four. Every comment counting the
`ui:` keys (`grep -rn 'fourteen' internal/domain`) follows the new count.
internal/config/config_test.go spells UIPrefs by hand: update `wantUIDefault` and every literal
(`grep -n 'FooterRows: wantFooterRowsDefault' internal/config/config_test.go`). Add `"ui.side-panel.show": "UI"` to
`TestRegistrySetIsTheInverseOfRead`'s owns table and give `everyKeyFileConfig` non-default show and sections values.
UIPrefs stays comparable: sections is a canonical-spelling string type (FooterRows' shape); Validate accepts "" for both.
`ui.side-panel.show` is a depth-3 path that config.SaveConfigSetting refuses (scalarPathDepth = 2) — leave
SaveConfigSetting and its depth guard unchanged; the registry row for ui.side-panel.show is written by item 6's block
writer, not the scalar writer.
**Files:** internal/domain/uiprefs.go; internal/domain/uiprefs_test.go; internal/domain/doc.go; internal/config/config.go; internal/config/registry.go; internal/config/registry_test.go; internal/config/config_test.go; internal/config/defaults/config.yaml; internal/tui/settingsapply.go; internal/tui/settingsapply_test.go; cmd/apogee/settingsrows_test.go; cmd/apogee/wire_settings_test.go; docs/manual/configuration.md
**Read first:** internal/domain/uiprefs.go — uiKeys, DefaultUIPrefs, ParseFooterRows, SpellFooterRows; internal/config/registry.go — ui.footer.rows row; internal/config/keyfield.go — uiField, uiBool;
internal/config/config.go — fileFooterRows; internal/config/registry_test.go — TestRegistrySetIsTheInverseOfRead, TestUIRowsLandThroughUIPrefsSet; internal/config/config_test.go — wantUIDefault, everyKeyFileConfig;
internal/domain/uiprefs_test.go — TestUIPrefsSetLandsEveryKey; cmd/apogee/wire_settings_test.go — settingKeysAppliedByTheRenderer
**Tests:** parse/spell round trip and refusal of an unknown section or mode; drift tests
`TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase`, `TestRegistryIsBijectionWithFileConfig`,
`TestTemplateMentionsEveryRegistryKey`, `TestManualDocumentsEverySettingsKey` updated; the four tests above;
`wantUIDefault` and the config_test.go literals, the owns table and `everyKeyFileConfig` updated.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase|SidePanel|TestDocMapNamesEveryFile' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRegistryIsBijectionWithFileConfig|TestTemplateMentionsEveryRegistryKey|TestUIRowsLandThroughUIPrefsSet|TestRegistrySetIsTheInverseOfRead|TestEveryConfigKeyReachesTheOptions|SidePanel|TestDocMapNamesEveryFile' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettingsRowsFormatEffectiveValues|TestEveryEditableSettingKeyHasAnApply' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Settings.*Apply' ./internal/tui/`
**Commit:** `feat(config): add ui.side-panel.show and its section list`

## 6. Sections write-back — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the new SettingsHost method is `WriteSidePanelSection(section, mode) (domain.SidePanelSections, error)`; it answers the list the file now resolves to, so the TUI caller (items 11/12) lands it and journals it via recordSettingEdit under `ui.side-panel.sections` — no TUI caller is added here.
NOTES (2026-11-08): a section the file's list does not name (or no list at all) writes the whole resolved list with the one mode changed, so display order is kept; a list written one entry per line, or a `side-panel:` block in flow style, is refused ("edit the file by hand"), the scalar writer's posture.

**What:**
Recast at the regression check (2026-11-08).
**Goal:** a pin, unpin or hide changes one section's mode in `ui.side-panel.sections` in the global
config file, keeping the other entries and their order, through the settings host.
**Approach (assumed at the header base):** a structured writer beside `config.SaveConfigSetting`
(`internal/config/configwrite_scalar.go`, which refuses non-Editable keys) that rewrites only the
`ui.side-panel` block; exposed on `settingsHost` (`cmd/apogee/wire_options.go`) as a method the TUI calls
like `m.opts.Settings.Write`, recorded through `recordSettingEdit`. Depends on item 5.
**Regression guard.** same key names as item 5.
The new `SettingsHost` method is implemented on `fakeSettingsHost` (`internal/tui/settings_test.go`) too, and
Acceptance compiles the tui test package. The host path calls `h.edits.refresh()` after a landed write, as `Write`
and `Reset` do (ADR 0041 D8). `configwrite_sections.go` is named in `internal/config/doc.go`.
The new writer owns the whole `ui.side-panel` block (both `show` and `sections`), creating the block when absent and
keeping comments, order and other keys; settingsHost.Write (cmd/apogee/wire_options.go) routes both `ui.side-panel.*`
keys to it, so /settings and /panel both write through it.
**Files:** internal/config/configwrite_sections.go; internal/config/configwrite_sections_test.go; internal/config/doc.go; cmd/apogee/wire_options.go; cmd/apogee/wire_options_test.go; internal/tui/tui.go; internal/tui/settings_test.go
**Read first:** internal/config/configwrite_scalar.go — SaveConfigSetting, writableKey; internal/config/configedit.go — verifiedEdit;
cmd/apogee/wire_options.go — settingsHost.Write, settingsHost.Reset; internal/tui/tui.go — SettingsHost;
internal/tui/settings_test.go — fakeSettingsHost; internal/tui/settingsapply.go — recordSettingEdit
**Tests:** write keeps comments, order and other keys; missing list is created from the default; a `show`
write; a write into a config with no `ui.side-panel` block; the host's sections write refreshes the
external-edit baseline (cmd/apogee).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Sections|TestDocMapNamesEveryFile' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Sections' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go vet ./internal/tui/` (compiles the tui test package)
**Commit:** `feat(config): write one side-panel section's mode back`

## 7. Slot width and the column — ✅ DONE (2026-11-08)

NOTES (2026-11-08): panelShown() shows the column only for an explicit `auto` or `on`; the zero UIPrefs' empty `SidePanelShow` draws no column (the config always resolves an unset key to `auto`, and testUIPrefs leaves it empty), so hand-built internal/tui tests keep the classic frame.
NOTES (2026-11-08): the driven-test pin covers every 140-column launch in the four named files (eight launches, including TestE2ESettingsTypeToFilter and the launcher and parallel homes), not only the five the guard counts; the pin is one shared `sidePanelOffConfig` const in e2e_approval_test.go, and watchedConfigBody now continues the `ui:` block that pin closes the home's config on (a second `ui:` key would be refused by the loader).
NOTES (2026-11-08): the transcript-slot panes still render at m.width until item 8; joinColumn squares every row above the rule to the slot width, so while the panel shows a pane's right edge is cut at the slot edge until item 8 sizes panes to the slot.
NOTES (2026-11-08): the separator `│` and the `nothing live` line paint in the existing `statusFaint` style (muted, bg-free); no new theme role was added.

**What:**
**Goal:** while the panel shows, the transcript slot is `m.width − 36` wide and the panel column
(separator included) fills the remaining 36 cells on every row above the session-name rule; the
session-name rule, status line, prompt box and footer keep `m.width`. Visibility follows the gate
(`auto`: transcript ≥ 100, `on`: ≥ 80, thresholds count the scroll-bar column whether painted or
not), and changes only on a resize across a threshold or a setting change. An empty panel paints
`nothing live`.
**Approach (assumed at the header base):** add `slotWidth()` and `panelShown()` beside
`transcriptBudget` (`internal/tui/model.go`); `layout()` sets the viewport from `slotWidth()`;
join the column in `stackTranscriptSlot` with a new authority-measured join in `boxdraw.go`
(ADR 0030: `th.measure`, `squareLine`; never `JoinHorizontal` or `Style.Width`); add the panel's
visibility to `frameKey` (`paintcache.go`). Depends on items 4 and 5.
**Regression guard.** the five cmd/apogee driven tests that run shipped defaults at 140 cols (e2e_approval_test.go,
e2e_livestate_test.go, e2e_parallel_test.go, e2e_guard_secrets_test.go x2) pin `ui.side-panel.show: off` in their
config so their row scanners keep reading transcript cells only; add those files to Files and their tests (one -run
line on ./cmd/apogee/) to Acceptance.
Join the column after `stackTranscriptSlot` returns, over every View row above `topRule` (body after `joinScrollbar`,
gap row, slot blocks) — never inside the walk `frameSpans` shares. Supersedes `layout.md` §Run view's rule that the
breadcrumb hint ends in the status line's right-slot column (~line 1448): with the panel shown it ends at the slot edge; amend it.
**Files:** internal/tui/model.go; internal/tui/boxdraw.go; internal/tui/paintcache.go; internal/tui/sidepanel.go; internal/tui/doc.go; internal/tui/sidepanel_test.go; layout.md; cmd/apogee/e2e_approval_test.go; cmd/apogee/e2e_livestate_test.go; cmd/apogee/e2e_parallel_test.go; cmd/apogee/e2e_guard_secrets_test.go
**Read first:** internal/tui/model.go — Model.layout, Model.transcriptBudget, Model.View, stackTranscriptSlot, Model.frameSpans;
internal/tui/boxdraw.go — Model.joinScrollbar, squareLine; internal/tui/paintcache.go — Model.frameKey
**Tests:** gate table (widths 116/117/136/137 × modes × scroll bar on/off); a section change
leaves `frameKey` and the transcript wrap unchanged; `TestPaintedBoxRowsAreNotFolded` still passes;
an empty column's top row reads `nothing live` and every row from `topRule` down is `m.width` wide.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanel|TestPaintedBoxRowsAreNotFolded|TestModelFitsTheStack' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EApprovalForcedLookSurvivesAutoMode|TestE2ELiveState|TestE2EParallel|TestE2EGuardSecrets' ./cmd/apogee/`
**Commit:** `feat(tui): reserve the side-panel column on wide terminals`

## 8. Slot panes take the slot width — ✅ DONE (2026-11-08)

NOTES (2026-11-08): the list surface (listsurface.go) is shared by transcript-side panes and the input-side dropdown, so its width reads go through a new `Model.paneWidth(framePane)` in panes.go (slotTranscript → slotWidth(), slotInput → m.width); every other slot reader calls `slotWidth()` directly.
NOTES (2026-11-08): `testUIPrefs` is left as it was rather than pinned to `off`: since item 7 the zero UIPrefs' empty `SidePanelShow` already draws no column, so hand-built tests keep the classic frame; pinning `off` would move TestSettingsApplyLocalLandsEveryUIKeyThroughSet's seed (its accepted value is `off`). Panel tests opt in by setting `SidePanelShow: auto`.
NOTES (2026-11-08): the settings-click test is `TestSettingsClickBesideTheSidePanel` (key-row selection and edit-buffer caret at 140×40 with the panel shown), placed in model_test.go beside TestSlotPanesFitBesideTheSidePanel; both match the Acceptance `-run` pattern.
NOTES (2026-11-08): model.go's View comment ("into the full window width layout() then gave it", ~line 3583) went stale with item 7, which gives the viewport the slot width; model.go is outside this item's files, so it is left as is.

**What:**
**Goal:** every pane painted in the transcript slot sizes itself from `slotWidth()`, so none
overpaints the panel; chrome readers keep `m.width`.
**Approach (assumed at the header base):** route the slot readers of `m.width` through
`slotWidth()`: `settings.go`, `listsurface.go`, `reportpane.go`, `ask.go`, `approval.go`,
`adoptionbody.go`, `picker.go`, `thinkingpane.go`, `panes.go` (`renderPopupPlaced`, `popupHeight`,
`popupInnerWidth`). Rule for the sweep: every `m.width` read whose output lands inside the transcript
slot — find with `grep -n 'm\.width' internal/tui/*.go`. Chrome (`model.go` input/status/footer/
`topRule`, `joinFrame`, `interject.go`, `suggestband.go`) is unchanged. Depends on item 7.
**Regression guard.** Width follows the pane's slot (`slotTranscript` → `slotWidth()`, `slotInput` → `m.width`); the
autocomplete dropdown (`autocomplete.go`) is chrome. The rule covers every `m.width` read that paints OR re-composes a
slot pane — `mouse.go`'s `settingsPaint`/`settingsFieldPaint` included. `testUIPrefs` pins the panel `off`; panel tests
opt in. Supersedes, for slot panes, the "full window width" contract (`grep -n 'full window width' internal/tui/*.go`).
**Files:** internal/tui/settings.go; internal/tui/listsurface.go; internal/tui/reportpane.go; internal/tui/ask.go; internal/tui/approval.go; internal/tui/adoptionbody.go; internal/tui/picker.go; internal/tui/thinkingpane.go; internal/tui/panes.go; internal/tui/popup.go; internal/tui/sessions.go; internal/tui/mouse.go; internal/tui/model_test.go
**Read first:** internal/tui/popup.go — renderPopupPlaced, popupInnerWidth; internal/tui/listsurface.go — Model.renderListPlaced;
internal/tui/autocomplete.go — Model.renderAutocompletePlaced; internal/tui/mouse.go — Model.settingsPaint, Model.settingsFieldPaint;
internal/tui/panes.go — paneSpecs, paneSpec.slot
**Tests:** `TestSlotPanesFitBesideTheSidePanel` — opens each `paneSpecs` pane with `slot == slotTranscript` with the
panel shown and asserts no painted row crosses the separator column, and that the dropdown still spans `m.width`;
a `/settings` click maps to the right row and field caret with the panel shown.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSlotPanesFitBesideTheSidePanel|SidePanel|TestThinkingWrapColumnFollowsTheWindow' ./internal/tui/`
**Commit:** `feat(tui): size transcript-slot panes beside the side panel`

## 9. Paint the sections — ✅ DONE (2026-11-08)

NOTES (2026-11-08): re-derived from the assumption that `sidepanel.State` could be held copy-on-write from the TUI alone: its fields are unexported, so the deep copy lives in its own package. `internal/sidepanel/sidepanel.go` adds `(*State).Clone()` (nil-safe) and `sidepanel.Reads(event)`, so an Event the panel never reads, such as a streamed token, costs no clone. Both are covered in sidepanel_test.go, and item 12's resume seeding can use the same Clone.
NOTES (2026-11-08): paint differs from the spec's sketch in four places, for item 13 to align `docs/layout/side-panel-layout.md`: (1) finished rows use the glyph set's `done` (`✓`, `√` under `ui.glyphs: safe`) and a failed run uses `failure` (`✗`), not the sketch's `✔`; (2) elapsed time uses the status line's `formatElapsed` (`01:12s`), not `1m12`; (3) the phase column reads queued/running/done/failed, because the State carries no Delegation target to show the sketch's `explore`; (4) every header that has a summary shows it, Sub-agents and Changed files included.
NOTES (2026-11-08): a running delegation is led by the status line's spinner frame (`m.spin.drawn`), so it animates with the spinner and honours `ui.glyphs: safe`. A queued delegation and an open task use `○`. New fixed glyphs and two styles (`panelHeader`, `panelCurrent`) are in theme.go.
NOTES (2026-11-08): TestSessionViewHoldsEveryFieldTheBoundariesReset needed no edit. It enumerates sessionView by reflection, and both boundaries now write `panel` (resetSessionView and resumeLoaded set it to nil), so the guard sees the field as the plan requires.
NOTES (2026-11-08): `tasks`, `changed-files` and `usage` read the top-level scope (`sidepanel.RunKey{}`), and the viewed run is not marked. Scoping to the run view is item 10's work.

**What:**
**Goal:** the column paints the shown sections per `docs/layout/side-panel-layout.md` — header row
`◆ <Title>` with summary and pin glyph `⊙`/`●`, rows per section, overflow (pinned first, then
list order; a squeezed section folds to its header; headers that do not fit become `+N more`) — from
the `sidepanel.State` the TUI folds every Event into.
**Approach (assumed at the header base):** hold `*sidepanel.State` in `sessionView`
(`sessionview.go`; extend `TestSessionViewHoldsEveryFieldTheBoundariesReset`); feed it from
`foldEventAgainst` (`fold.go`) so live and watch mode share the path; paint in
`internal/tui/sidepanel.go`; glyphs and styles in `themeStyles`. Depends on item 8.
**Regression guard.** `newModel` and `clearWatchView` build `sessionView` literals with a nil State: allocate it on
first fold and keep it copy-on-write (replace the pointer on each write, as `copyOnWriteField` does) — the item yields
to `doc.go`'s no-shared-mutable-state rule. `resetSessionView` (`commandrun.go`) and `resumeLoaded` (`sessions.go`)
reset it, so `TestSessionViewHoldsEveryFieldTheBoundariesReset` sees the field.
**Files:** internal/tui/sidepanel.go; internal/tui/sessionview.go; internal/tui/fold.go; internal/tui/theme.go; internal/tui/sidepanel_test.go; internal/tui/commandrun.go; internal/tui/sessions.go; internal/tui/watch_test.go
**Read first:** internal/tui/fold.go — Model.foldEventAgainst; internal/tui/sessionview.go — sessionView; internal/tui/watch.go — Model.clearWatchView, Model.foldWatchedEvent;
internal/tui/commandrun.go — Model.resetSessionView; internal/tui/model.go — newModel; internal/tui/watch_test.go — TestSessionViewHoldsEveryFieldTheBoundariesReset;
internal/tui/fold_test.go — TestFoldEventCoversEveryEventVariant
**Tests:** frame tests per the spec's sketch (each section, each mode, overflow fold, `+N more`,
empty state), using the drivers in `docs/design/test-drivers.md`; a watched journal's first Event
folds without a panic; `/clear` empties the panel; a discarded Model copy's fold leaves the kept one unchanged.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanel|TestSessionView' ./internal/tui/`
**Commit:** `feat(tui): paint side-panel sections from folded Events`

## 10. Run view scoping — ✅ DONE (2026-11-08)

NOTES (2026-11-08): re-derived from the assumption that asking `sidepanel.State` for the viewed run gives the gauge's fill: the State measures the top level against the reading's own window, while `contextGauge` uses `topLevelWindow()` and also handles `stateErrored` and the resume and watch seeds of `ctxUsed`. The panel now takes the gauge's own figure instead. In `internal/tui/model.go`, `contextGauge`'s body moved into `viewedFill() (contextUsage, bool)`, and `contextGauge` renders it. `internal/sidepanel` gains `UsageView.WithFill(Fill)`, which recomputes history, summary and the live rule from the fill it is given. `Usage` now computes through it and behaves as before.
NOTES (2026-11-08): this changes the top level too. The usage section now measures against `topLevelWindow()`, as the gauge does, so item 9's paint and overflow tests now read on the session window (`panelUsed` = 20480 of 32768) instead of a reading naming 1000.
NOTES (2026-11-08): the spec leaves the viewed sub-agent's mark open. It is a `▸` before the run's name, both in the current task's `panelCurrent` style. Item 13 should write this into docs/layout/side-panel-layout.md §Run view.
NOTES (2026-11-08): a run recorded before run ids paints an unnamed sub-agent row: `sidepanel.foldDelegationCall` (item 4) skips a call that has no SpawnRunID. Scoping by depth plus spawn still works and is tested.

**What:**
**Goal:** while Run view shows a run, the tasks, changed-files and usage sections show that run's
state; sub-agents lists every run and marks the viewed one.
**Approach (assumed at the header base):** the paint reads the scope from `viewedRun()` /
`inRunView()` (`model.go`, the same source `contextGauge()` uses) and asks `sidepanel.State` for that
run. Depends on item 9.
**Regression guard.** query sidepanel by the run identity of item 4.
The usage section's fill for the viewed scope equals `contextGauge`'s (top level: `ctxUsed`/`ctxEstimate` over
`topLevelWindow()`; child: `viewedChild()`'s `ctxUsed`/`ctxLimit`, which skip Maintenance and fall back via `childWindow`).
**Files:** internal/tui/sidepanel.go; internal/tui/sidepanel_test.go
**Read first:** internal/tui/runview.go — viewedRun, viewedChild; internal/tui/model.go — contextGauge, topLevelWindow;
internal/tui/transcript.go — runRef, applyUsage, childWindow; internal/tui/runview_test.go — TestRunViewGaugeStatesTheViewedRun
**Tests:** open a child's Run view; panel shows the child's tasks and usage; `esc` restores the top level;
the usage fill matches the gauge for a child whose last reading is Maintenance or has `ContextWindow` 0;
a run with an empty RunID is scoped by depth+spawn.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanelRunView' ./internal/tui/`
**Commit:** `feat(tui): scope the side panel to the viewed run`

## 11. Pointer

**What:**
**Goal:** a body click opens the section's detail (sub-agent row → its Run view; task row → the
task-list card unfolded and scrolled to; file row → that write's tool block; usage → `/usage`); a
pin-glyph click toggles `pinned`↔`auto` and writes back (item 6); header title clicks, wheel and drag
over the panel do nothing; hover shape matches.
**Approach (assumed at the header base):** a panel hit zone before `pointTranscriptRow` in
`handleMouseClick` (`mouse.go`) and `pointerShapeAt` (`pointershape.go`); `foldMouseWheel` ignores
panel cells. Depends on items 6 and 10.
**Regression guard.** Ask the panel hit BEFORE the `pointerPanes` loop for `x >= slotWidth()` in `handleMouseClick`,
`foldMouseWheel` and `pointerShapeAt` (pane hits are y-only); it claims every panel cell (a no-op where nothing is
targeted), so no click falls to `pointTranscriptRow`, whose clamp stays for the drag. A task-row click unfolds that
one card via `transcript.setExpanded`, never `toggleTaskListFold` (it writes `ui.task-list-open`).
**Files:** internal/tui/mouse.go; internal/tui/pointershape.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go
**Read first:** internal/tui/mouse.go — handleMouseClick, pointTranscriptRow, popupPaneHit, approvalHit, foldMouseWheel;
internal/tui/pointershape.go — pointerShapeAt; internal/tui/runview.go — openRunAt; internal/tui/settingsapply.go — toggleTaskListFold
**Tests:** one driven click per target; pin click writes through a fake settings host; a drag from
the transcript stops at the separator; with the approval menu up, two clicks on a panel row take no
decision; a click on a blank panel cell beside a tool-block header leaves the block unchanged; a
task-row click writes no setting.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanelClick|SidePanelPin|SidePanelDrag' ./internal/tui/`
**Commit:** `feat(tui): open details and pin sections from the side panel`

## 12. `/panel` command and resume

**What:**
Recast at the regression check (2026-11-08).
**Goal:** `/panel on|off|auto` sets `ui.side-panel.show`; `/panel pin|unpin|hide <section>` sets a mode
(`unpin` → `auto`) through item 6's writer; both are in `/help`, autocomplete and
`docs/manual/commands.md`. A resumed session's panel shows its stored task list, changed files and
usage; sub-agents starts empty.
**Approach (assumed at the header base):** a `commandSpecs` row in `command.go` with `verbGrammar`
parsing; `/panel` stays out of watch mode (`watchVerbs`; see the guard). Resume: in
`replayResumed` (`model.go`) walk the restored entries and call `sidepanel.State`'s typed entry
points (task list from the restored agent state, writes from `session.ToolView` target and
`EditRegions`, usage from the stored totals). Depends on item 11.
**Regression guard.** same key names as item 5.
Add `panel` to `TestOnlyTheGrammarVerbsCarryAParseArgsHook`'s pinned set; bare `/panel` reports the mode and section
modes (`parseArgs(nil)` succeeds); name `whileRunning`. Reset in `resumeLoaded`, rebuild tasks and writes in `replayScrollback`
(both resume doors), not `replayResumed`: tasks from the last non-error `task_list` card per run key (item 4's), fed as
`SetTasks(run, …)` per run; writes from non-error writer-tool entries keyed workspace-relative on both sides —
`Clean(Join(workspace, args path))` on replay, `Rel(workspace, WriteTarget)` live (or WriteTarget persisted additively on
the stored tool entry). Usage is seeded after both doors restore ctxUsed/usageBase/delegateUsage (`resumeLoaded` calls
`replayScrollback` before it restores them), or read live as item 10's gauge does — never inside `replayScrollback`. Yields to `watchVerbs`' view-only rule (`watch.go`)
and `command.go`'s idle-only rule for config-writing verbs.
**Files:** internal/tui/command.go; internal/tui/command_test.go; internal/tui/help.go; internal/tui/watch.go; internal/tui/model.go; internal/tui/sessions.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; docs/manual/commands.md
**Read first:** internal/tui/sessions.go — resumeLoaded; internal/tui/model.go — replayResumed, replayScrollback; internal/session/transcript.go — ToolView;
internal/tools/workspace_scoped.go — WorkspaceWriteTarget; internal/tools/task_list.go — TaskList.Execute; internal/tui/commandrun.go — resetSessionView;
internal/tui/command.go — commandSpecs, commandSpec, verbGrammar, commandByName
**Tests:** `/panel` verbs drive the setting with the exact strings `/help` lists; refused section
name; bare `/panel` reports; resume frame test through both doors (start-up resume and a `/sessions`
restore, the `/sessions` door asserting the usage row); a live rewrite after a replay keeps one row; a child's
card and an errored card do not replace the session's list; watch mode refuses `/panel`; `TestHelpNoteListsEveryVerb`; the grammar-verb pin.
**Acceptance:** `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanelCommand|SidePanelResume|TestHelpNoteListsEveryVerb|TestOnlyTheGrammarVerbsCarryAParseArgsHook' ./internal/tui/`
**Commit:** `feat(tui): add /panel and rebuild the side panel on resume`

## 13. Docs go live

**What:**
**Goal:** the shipped docs describe the panel as implemented: the spec's status line, `layout.md`
§"The side panel" and the glossary Note `side-panel` drop "not yet implemented"; a manual section
explains the panel, its settings, `/panel`, and that `terminal` writes are not shown.
**Approach (assumed at the header base):** edit `docs/layout/side-panel-layout.md`, `layout.md`,
`.capcom/knowledge/glossary/glossary-identity/side-panel.md`; add the manual section where the
TUI's surfaces are described under `docs/manual/`. Rule: every line naming the panel unbuilt or
"design of record" — `grep -rniE 'not yet|design of record' docs/layout/side-panel-layout.md layout.md .capcom/knowledge/glossary/glossary-identity/side-panel.md`. Depends on item 12.
**Regression guard.** same key names as item 5. The rule's grep is case-insensitive and phrase-split-proof: the spec
says `**Not yet implemented.**` and `layout.md` wraps "not yet / implemented" across two lines.
**Files:** docs/layout/side-panel-layout.md; layout.md; .capcom/knowledge/glossary/glossary-identity/side-panel.md; docs/manual/configuration.md
**Read first:** docs/layout/side-panel-layout.md — Status line; layout.md — §"The side panel"; .capcom/knowledge/glossary/glossary-identity/side-panel.md;
docs/manual/configuration.md — §"The terminal UI — `ui:`"; .capcom/knowledge/decisions/adr-0110/adr-0110-consequences.md — Docs bullet;
cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none beyond docs drift tests.
**Acceptance:** `capcom check`; `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManual' ./cmd/apogee/`
**Commit:** `docs: describe the shipped side panel`
