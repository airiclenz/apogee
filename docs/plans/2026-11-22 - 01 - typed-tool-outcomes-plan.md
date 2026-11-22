# Typed tool outcomes — process endings and delegation verdicts travel typed, the prune note is worded once

**Goal:** a subprocess's or console's ending and a delegation's outcome (including one that never started) reach the TUI and recipes as typed `domain.ToolSummary` values set where the engine decides them, so no live reader regexes prose back; session replay and summary-less lines parse words through one parser each. The prune note is worded once, on `domain.PruneEvent`.

**Date:** 2026-11-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 95c0d0cd

**Regression check (2026-11-22, 95c0d0cd):**
- 2: recast (writer decision: `unstarted` always written, the wire spellings pinned in `TestEncodeToolSummaryMirror`, ExitCode keeps the marker's code for every State; the never-started summary prose moves to item 7); yields to ADR 0075 decision 3.
- 3: recast (writer decision: a `resultDetail` hook per presenter; the typed strip takes off the marker / status line itself; a running console_open falls to the header id).
- 4: guard folded (a killed / timed-out / denial-stopped ending carries `-1`, slot word and recipe code unchanged; the background fixture carries its ending); supersedes `internal/tools/doc.go:22-39`'s closed summary list per the ratified "Process ending on the wire" call.
- 5: guard folded (console test files named and `consoleTail`'s call site kept compiling; status line and ending from one read; the claim covers results that render a Console tail); supersedes `internal/tools/doc.go`'s closed summary list per the same call.
- 6: guard folded (writer decision: -run widened to `TestTranscriptCodec` and `TestCollapsedRun`; replay sets the verdict; the `delegationVerdict` func retired; `failed` / `succeeded` stay the painted fields; the ceiling-refusal fallback; the zero verdict keeps the ✓; the verdict set after the stat hook); yields to F-29 (`internal/tui/toolview.go:120-124`).
- 7: guard folded (the bracket test's want carries the outcome; every never-started summary sentence rewritten here; both unstarted-cancel returns in `subagent.go` attach the outcome).
- 8: recast (writer decision: `failedSummary` stays namedSummary's all-tool classifier; tests naming a deleted reader rewritten over the parser; the prose guard widened; per-case test wording); yields to `internal/tui/toolleader.go:330-338`'s record of `failedSummary`.
- 2 (re-check): guard folded (both new shapes join `roundTripSummaries`; `TestDecodeRoundTripsEveryKind` and a raw absent-`unstarted` decode test join Acceptance; `example_test.go` joins Files).
- 3 (re-check): guard folded (Goal's slot words corrected to `exit N` / `error: exit -1`; typed killed and timed-out terminal cases; every "sub_agent alone sets resultDetail" comment rewritten, `internal/tui/doc.go` joins Files).
- 6 (re-check): guard folded (writer decision: the interim replay reader reads the verdict off the stored stat when the slot is quoted, the slot text otherwise; a quoted-`done` replay case).
- 8 (re-check): guard folded (the parser reads the stat for a quoted slot; the Goal's "adding an outcome kind" sentence struck; the deleted-names grep joins Acceptance).

**Sources:**
- `.capcom/issues/process-ending-prose.md`, `.capcom/issues/delegation-outcome-reparsed.md`, `.capcom/issues/prune-sentence-restated.md`
- ADR 0083 §1 (`capcom know show "ADR 0083" --with-children`): the session record keeps rendered verdicts; replay keeps its parser
- ADR 0103 decision 9: a tool result's `summary` rides the Event JSON stream
- `docs/plans/archived/2026-11-21 - 00 - delegation-one-value-plan.md` (`childEnding` is the engine's single ending source)
- `docs/manual/headless.md` (the summary-kind table)

**Ratified design calls** (owner, 2026-11-22):
- **Process ending on the wire:** a new `ProcessEnding` ToolSummary variant ships as an additive `process_ending` summary kind on the JSON stream, documented in `docs/manual/headless.md`; the TUI keeps its prose regexes only as a fallback for a result with no summary (an older producer's lines).
- **Never-started delegation:** the engine attaches a typed `DelegationOutcome` carrying an unstarted reason on every never-started result; it rides the stream's `delegation_outcome`. The TUI's live engine-prose readers go; only replay and summary-less results parse words, through one parser.

**Standing requirements:**
- skills: coding-standards
- Narrow test runs only: one package, a `-run` filter, `GOMEMLIMIT=2GiB`; never a multi-package or whole-package `-race` run, never two runs at once.
- The session record format does not change (ADR 0083 §1); the model-facing result text does not change; stderr and headless narration stay byte-identical.
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- `diagnostics.go`'s `(exit code N)` (never parsed).
- Queued / running delegation state: stays derived at paint time from the entry's phase.
- Any change to verdict wording, glyphs or colours.

## 1. The prune note is worded on its event — ✅ DONE (2026-11-22)

**What:**
**Goal:** `domain.PruneEvent` has a value-receiver `Notice()` returning `pruned %d tool results (~%d tokens)`, pinned by a domain test; headless narration, the run record fold and the TUI each print `Notice()` and no other site formats that sentence.
**Approach (assumed at the header base):** add `func (e PruneEvent) Notice() string` beside `RefClippedEvent.Notice` in `internal/domain/events.go`. `cmd/apogee/headless.go` prints it with `fmt.Fprintln` like its neighbours; `internal/run/transcript.go`'s fold uses it and drops the "worded exactly as the TUI" comment; `internal/tui/transcript.go`'s `addPrune` takes the event and calls `Notice()` like `addMalformedChunks` (no `stripEscapes`: ints only), its caller updated.
**Files:** internal/domain/events.go, internal/domain/events_test.go, cmd/apogee/headless.go, internal/run/transcript.go, internal/tui/transcript.go
**Read first:** internal/domain/events.go — PruneEvent, RefClippedEvent.Notice, MalformedChunksEvent.Notice; cmd/apogee/headless.go — narrationSink.narrate; internal/run/transcript.go — transcriptFold.fold; internal/tui/transcript.go — transcript.apply, addPrune, addMalformedChunks;
internal/domain/events_test.go — TestMalformedChunksEventNoticeNamesTheCount; internal/tui/transcript_test.go — TestPruneNoteIsOneHostLineAtItsOwnRun; cmd/apogee/headless_test.go — TestHeadlessPrintsThePruneNotice, TestNarrationSinkForwardsEveryEvent
**Closes:** prune-sentence-restated
**Tests:**
- New `TestPruneEventNoticeNamesResultsAndTokens` (table, `TestMalformedChunksEventNoticeNamesTheCount` style) in `internal/domain/events_test.go`.
- `TestTranscriptFoldRecordsAPrune`, `TestPruneNoteIsOneHostLineAtItsOwnRun`, `TestHeadlessPrintsThePruneNotice`, `TestNarrationSinkForwardsEveryEvent` pass unchanged.
**Acceptance:**
- `go build ./internal/domain/ ./internal/run/ ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestPruneEventNotice ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestTranscriptFoldRecordsAPrune ./internal/run/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestPruneNoteIsOneHostLineAtItsOwnRun ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessPrintsThePruneNotice|TestNarrationSinkForwardsEveryEvent' ./cmd/apogee/`
- `grep -rn 'pruned %d tool results' --include='*.go' . | grep -v _test.go` prints only `internal/domain/events.go`.

**Commit:** `refactor(domain): the prune note is worded on PruneEvent`

## 2. ProcessEnding and an unstarted delegation are domain values on the wire — ✅ DONE (2026-11-22)

NOTES (2026-11-22): the root facade also re-exports the new enum constants (`DelegationRan` … `DelegationStoppedQueued`, `ProcessRunning`/`ProcessExited`/`ProcessKilled`) beside the existing `DelegationUnbounded` block, and `example_test.go` compiles them, so an embedder can name the values it switches on; an unknown `state` name decodes to `ProcessRunning` (the zero), an unknown `unstarted` name to `DelegationRan`.

**What:** Recast at the regression check (2026-11-22).
**Goal:** `domain.ProcessEnding` is a sealed ToolSummary variant `{State ProcessState; ExitCode int; TimedOut, DenialStopped bool}` with `ProcessState` = `ProcessRunning`, `ProcessExited`, `ProcessKilled` (ExitCode carries the code the prose marker writes for every State that writes one — a killed or timed-out terminal run keeps its `-1`). `domain.DelegationOutcome` gains `Unstarted DelegationUnstarted` — zero `DelegationRan`, then `DelegationSkipped` (pre-empted), `DelegationRefused` (past the fan-out ceiling), `DelegationCancelled` (turn cancel while queued), `DelegationStoppedQueued` (human stop while queued); when set, Bound, IsStoppedByUser, HasNoReport and SteeredMessages are zero. Both round-trip through `internal/eventjson` (`process_ending` kind: `state`, `code`, `timed_out`, `denial_stopped`; `delegation_outcome` gains `unstarted`, always written) and are documented in `docs/manual/headless.md`; the root facade aliases the new types. No producer sets them yet.
**Approach (assumed at the header base):** extend `internal/domain/toolsummary.go` (rewrite every "nine variants" count — grep `-rn 'nine' internal/domain/`; the `DelegationOutcome` doc's "a never-ran result carries none" sentence is item 7's); bump `TestToolSummaryVariantsAreSealed`'s `want`. Mirror structs and kind constants beside the existing ones in `encode.go` (`toolSummaryOf`) and `decode.go` (`summaryDecoders`), fixtures in `decode_test.go` so `TestDecodeCoversEveryToolSummaryVariant` passes. Aliases in `apogee.go` beside the other ToolSummary aliases.
**Regression guard.** The item yields to ADR 0075 decision 3 (no omitempty on Event lines; `.capcom/knowledge/decisions/adr-0075/adr-0075-decision.md`, `internal/eventjson/encode.go:269`) — `unstarted` is always written (`""` for DelegationRan, like `bound`), an absent or `""` key decodes to DelegationRan; the wire spellings are binding — `process_ending` fields `state` (`running|exited|killed`), `code`, `timed_out`, `denial_stopped`, and `unstarted` (`""|skipped|refused|cancelled|stopped_queued`); TestEncodeToolSummaryMirror gains rows for both and runs in Acceptance; update the pinned bytes in encode_test.go and add it to Files. ExitCode carries the code the prose marker writes for every State that writes one (a killed or timed-out terminal run keeps its `-1`), so every slot word and recipe code is unchanged from today. Item 2 does NOT rewrite the DelegationOutcome doc's "a never-ran result carries none" sentence or headless.md's "for a run that started — or null" sentence: those move to item 7 (headless.md joins item 7's Files) under the rule "every prose site saying a never-started or queued delegation result carries no summary is rewritten by item 7", with the reviewer's grep; item 2 still documents the new kind and field in headless.md's table. `TestDecodeCoversEveryToolSummaryVariant` only encodes `roundTripSummaries` and collects `kind`, so both new shapes join `roundTripSummaries` and `TestDecodeRoundTripsEveryKind` (the test that decodes them) runs in Acceptance; the absent key is pinned by a named test feeding raw `{"kind":"delegation_outcome","bound":"",...}` with no `unstarted` through `toolSummaryFrom`. The new aliases join `example_test.go`'s alias compile list.
**Files:** internal/domain/toolsummary.go, internal/domain/toolsummary_test.go, internal/domain/doc.go, apogee.go, example_test.go, internal/eventjson/encode.go, internal/eventjson/encode_test.go, internal/eventjson/decode.go, internal/eventjson/decode_test.go, docs/manual/headless.md
**Read first:** internal/domain/toolsummary.go — ToolSummary, DelegationOutcome, DelegationBound; internal/eventjson/encode.go — toolSummaryOf, delegationOutcomeSummary, delegationBoundNames; internal/eventjson/decode.go — summaryDecoders, summaryWith, toolSummaryFrom, delegationBoundNamed;
internal/eventjson/decode_test.go — roundTripSummaries, TestDecodeRoundTripsEveryKind, TestDecodeCoversEveryToolSummaryVariant; internal/eventjson/encode_test.go — TestEncodeToolSummaryMirror, TestEncodeJSONGolden;
internal/domain/toolsummary_test.go — TestToolSummaryVariantsAreSealed; apogee.go — DelegationOutcome, DelegationBound aliases; docs/manual/headless.md — The tool summary table
**Tests:**
- `TestToolSummaryVariantsAreSealed` (count +1); `TestDecodeCoversEveryToolSummaryVariant` and `TestDecodeRoundTripsEveryKind` with `roundTripSummaries` fixtures for both new shapes, including a `delegation_outcome` with `unstarted` set.
- New `TestDecodeReadsAnAbsentUnstartedAsRan` in `internal/eventjson/decode_test.go`: raw `{"kind":"delegation_outcome","bound":"",...}` with no `unstarted` key, and one with `"unstarted":""`, fed through `toolSummaryFrom`, decode to `DelegationRan`.
- `TestEncodeToolSummaryMirror` gains a `process_ending` row and a `delegation_outcome` row with `unstarted` set, pinning the wire spellings above; its existing `delegation_outcome` bytes (and `TestEncodeJSONGolden`'s) gain `"unstarted":""`.
- `TestEncodeMapsEveryEventVariant` passes.
**Acceptance:**
- `go build ./... `
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestToolSummary ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDecodeCoversEveryToolSummaryVariant|TestDecodeRoundTripsEveryKind|TestDecodeReadsAnAbsentUnstartedAsRan|TestEncodeMapsEveryEventVariant|TestEncodeToolSummaryMirror|TestEncodeJSONGolden' ./internal/eventjson/`

**Commit:** `feat(domain): ProcessEnding and an unstarted delegation outcome ride the event stream`

## 3. The TUI reads a process run's ending off ProcessEnding — ✅ DONE (2026-11-22)

NOTES (2026-11-22): the six process presenters' `detail` hooks became `resultDetail` hooks by changing the existing functions' signatures to take the whole result (`subprocessDetail`, `consoleDetail`, `consoleOpenDetail` keep their names) rather than adding parallel hooks; `TestConsoleStatusWordMustBeginItsOwnLine` now calls `consoleDetail` with a summary-less `domain.ToolResult`.
NOTES (2026-11-22): the typed strip keys on the line the ending states (`[exit code <ExitCode>…]` for the one-shot tools, `alive` / `exited with code N` / `killed` for a Console), so a misleading `[exit code 9]` the output printed stays in the body of a run whose ending carries code 2; `exitCodeStat` also reads the typed code (always 0 on its route).
NOTES (2026-11-22): `internal/tui/toolblock_test.go` (named in Files) needed no change — the new rendering-parity check lives in `TestProcessEndingDecidesTheSlot` via `renderToolBlock`.

Depends on item 2.

**What:** Recast at the regression check (2026-11-22).
**Goal:** terminal, python_exec and the four console tools' slots (terminal / python_exec: `exit 0`, red `error: exit N`; console: `alive` / `exit N` / `killed`) and body stripping are decided from a result's `ProcessEnding` when present; the `exitCodeMarker` / `consoleStatusMarker` regexes run only for a result with no summary; a result carrying a `ProcessEnding` still shows its success body. A console tail with `[wait cut short by cancel]` after its status line reads its status correctly when typed.
**Approach (assumed at the header base):** in `internal/tui/toolregistry.go`, `subprocessFailure`/`exitCodeFailure`, `exitCodeStat`, `consoleStatusStat`, `consoleOpenStat` and `consoleDetail` read `res.Summary.(domain.ProcessEnding)` first and fall back to the regex. `toolView.absorbProse` skips the `detail` extractor when Summary is non-nil, so give each of the six presenters a `resultDetail` hook (runs whether or not a summary is set) that returns today's detail outcome off the typed strip. One helper reads the ending; no parallel module.
**Regression guard.** Use a `resultDetail` hook per presenter (runs whether or not a summary is set), not a `body` hook, returning today's detail outcome so one-line and empty-output runs keep their promoted line / summaryOnly; the typed strip takes off the marker / status line itself, never "the last line" (a trailing tree-mutation warning stays in the body); consoleOpenStat with ProcessRunning falls to the header id as today; TestProcessEndingDecidesTheSlot adds a one-line, an empty-output, a trailing-warning and a typed console_open case; update the "one entry that sets it, sub_agent" comment. Slot words stay today's (`exitMarkerPhrase`): console `alive` / `exit N` / `killed`; terminal and python_exec word `exit <ExitCode>` for every State, so a killed or timed-out run stays `error: exit -1`; TestProcessEndingDecidesTheSlot adds a typed killed and a timed-out terminal case. Rewrite every comment saying sub_agent alone sets or reads resultDetail (`grep -n 'resultDetail' internal/tui/*.go` — incl. toolregistry.go's toolPresenter field doc and package narration, toolview.go's absorbProse doc, doc.go).
**Files:** internal/tui/toolregistry.go, internal/tui/toolview.go, internal/tui/doc.go, internal/tui/toolpresent_test.go, internal/tui/toolblock_test.go
**Read first:** internal/tui/toolview.go — absorbProse, enrichWithResult, absorbFailure, hookedFailureBody, applyStat; internal/tui/toolregistry.go — toolPresenter.resultDetail, subprocessFailure, subprocessDetail, exitMarkerPhrase, exitCodeStat, consoleStatusStat, consoleOpenStat, consoleDetail, consoleOpenDetail;
internal/tools/terminal.go — subprocessToolResult; internal/tools/console_common.go — consoleStatus, withCutShortNote; internal/agent/treesnapshot.go — appendTreeMutationWarning;
internal/tui/toolpresent_test.go — TestConsoleStatusWordMustBeginItsOwnLine, TestConsoleCardStatesEachFactOnce, TestPresentToolCallFailedSubprocessNamesItsExitCode, TestToolStat
**Tests:**
- New `TestProcessEndingDecidesTheSlot` in `internal/tui/toolpresent_test.go`: a result whose Content ends in a misleading `[exit code 9]` but carries `ProcessEnding{Exited, 2}` shows `exit 2`; a console tail ending in `[wait cut short by cancel]` with `ProcessEnding{Killed}` shows `killed`; success bodies match the prose-path rendering; a one-line run keeps its promoted line and an empty-output run its summaryOnly; a trailing tree-mutation warning stays in the body; a typed console_open with `ProcessRunning` keeps the header id slot; a typed `ProcessKilled` and a `TimedOut` terminal ending (ExitCode -1) both show `error: exit -1`, never `killed`.
- `TestPresentToolCallFailedSubprocessNamesItsExitCode`, `TestToolStat`, `TestConsoleCardStatesEachFactOnce`, `TestConsoleStatusWordMustBeginItsOwnLine`, `TestEveryToolShapeCollapsesInsideTheRowBudget` pass (summary-less path).
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestProcessEndingDecidesTheSlot|TestPresentToolCall|TestToolStat|TestConsoleCard|TestConsoleStatusWord|TestEveryToolShapeCollapses|TestLoneBlankRun' ./internal/tui/`

**Commit:** `refactor(tui): a process run's slot reads its ProcessEnding`

## 4. One-shot runs attach their ending; a recipe reads its code off it

Depends on items 2 and 3.

**What:**
**Goal:** every result `subprocessToolResult` builds (terminal, python_exec, the confined path) carries a `ProcessEnding` — exit 0 included; a timeout sets `TimedOut`, a confinement denial stop sets `DenialStopped`, a killed run is `ProcessKilled`. `scriptOutput` (`internal/agent/recipe.go`) reads the code off it; the `exitCodeMarker` regex in `recipe.go` is deleted, and a failed result with no `ProcessEnding` is "could not run". Content is byte-identical.
**Approach (assumed at the header base):** `subprocessToolResult` (`internal/tools/terminal.go`) builds the ending beside the prose it writes and returns via `okSummary` or a new `errSummary` helper beside it in `internal/tools/tools.go`. terminal's `-1` maps to `ProcessKilled` (with `TimedOut` when the deadline fired).
**Regression guard.** A `ProcessKilled` ending (and any `TimedOut` / `DenialStopped` one) carries `ExitCode: -1`, as the marker does today, and `scriptOutput` reads that `-1`, so a timed-out recipe script stays `StatusBlocked` (`internal/workflow/stages.go:750`). Terminal's `-1` also covers a start failure, a broken I/O copy and a drain-wedged clean leader (`internal/subprocess/subprocess.go:408-421`): item 3's slot word for any terminal / python_exec `-1` stays `error: exit -1`, never `killed`. `TestBackground_RerunFailedRerunsAScriptStageTheTallyDoesNotCount`'s fake shell result (`internal/agent/background_test.go:522-524`) gains `Summary: domain.ProcessEnding{State: domain.ProcessExited, ExitCode: 1}` so it still exercises an exit-1 script, not "could not run". Supersedes `internal/tools/doc.go:22-39` ("Exactly THIRTEEN built-ins attach one … terminal, python_exec … deliberately do not … an error result never carries one") per the ratified "Process ending on the wire" call: rewrite it to name terminal and python_exec and that their failed results carry one.
**Files:** internal/tools/terminal.go, internal/tools/tools.go, internal/tools/terminal_test.go, internal/tools/doc.go, internal/agent/recipe.go, internal/agent/recipe_test.go, internal/agent/background_test.go
**Read first:** internal/tools/terminal.go — subprocessToolResult, isFailFastStop; internal/subprocess/subprocess.go — exitCodeOf, run (the -1 forcing); internal/tools/tools.go — okSummary, errorResult; internal/agent/recipe.go — scriptOutput, exitCodeMarker, recipeScripts.RunScript, runScriptCall;
internal/workflow/stages.go — scriptReceipt, runState.runScript; internal/agent/background_test.go — TestBackground_RerunFailedRerunsAScriptStageTheTallyDoesNotCount; internal/agent/confine_linux_test.go — TestRecipe_APlanScriptWritesOnlyItsWorkflowFolder; internal/tools/doc.go — package doc
**Tests:**
- New `TestSubprocessToolResultCarriesItsEnding` (table: exit 0, exit 3, timeout, denial stop) in `internal/tools/terminal_test.go`; existing `TestSubprocessToolResult*` pass unchanged.
- New `TestScriptOutputReadsTheProcessEnding` in `internal/agent/recipe_test.go`: Content ends `[exit code 9]`, ending says 2 → code 2; a `ProcessKilled` + `TimedOut` ending → code -1; IsError with no ending → could not run.
- `TestBackground_RerunFailedRerunsAScriptStageTheTallyDoesNotCount` passes with its fixture carrying the ending.
**Acceptance:**
- `go build ./internal/tools/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSubprocessToolResult|TestTerminal' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestScriptOutput|TestRecipe|TestBackground_RerunFailedRerunsAScriptStageTheTallyDoesNotCount' ./internal/agent/`

**Commit:** `refactor(tools): a one-shot run's ending travels as a ProcessEnding`

## 5. Console tools attach their ending

Depends on items 2 and 3.

**What:**
**Goal:** every console_open, console_send, console_read and console_close result carries a `ProcessEnding` (`ProcessRunning`, `ProcessExited` with its code, or `ProcessKilled`; `DenialStopped` on a live confinement denial). Content is byte-identical.
**Approach (assumed at the header base):** `renderConsoleTail` (`internal/tools/console_common.go`) returns the ending with the string; `consoleTail`, `consoleWindowTail` and `consoleOpenTail` pass it through `withCutShortNote` untouched; each tool returns via `okSummary` / `errSummary`.
**Regression guard.** `internal/tools/console_test.go` does not exist: the three pinned tests live in `console_close_test.go` (:40, :166) and `console_open_test.go` (:100), and the new test goes in `console_close_test.go`; `console_close_test.go:178` calls `tail, gathered := consoleTail(...)`, so keep `consoleTail`'s two returns or update that call site in the same commit. `renderConsoleTail` builds the status line and the `ProcessEnding` from one read (`consoleStatus` returns both), never a second `Alive()` / `ExitCode()` read. The Goal's claim covers every result that renders a Console tail; a refusal (`lookupConsole`'s `errorResult`, console_open's and console_send's errors) has no process and carries none. Supersedes `internal/tools/doc.go:22-39`'s closed list of summary-attaching built-ins per the ratified "Process ending on the wire" call: add the four console tools there.
**Files:** internal/tools/console_common.go, internal/tools/console_open.go, internal/tools/console_send.go, internal/tools/console_read.go, internal/tools/console_close.go, internal/tools/console_close_test.go, internal/tools/console_open_test.go, internal/tools/doc.go
**Read first:** internal/tools/console_common.go — renderConsoleTail, consoleStatus, consoleTail, consoleWindowTail, consoleOpenTail, withCutShortNote, lookupConsole; internal/tools/console_open.go — ConsoleOpen.Execute; internal/tools/console_close.go — ConsoleClose.Execute;
internal/tools/console_send.go — ConsoleSend.Execute; internal/tools/console_read.go — ConsoleRead.Execute; internal/tools/console_close_test.go — TestConsoleTail_ZeroWaitReadsTheTailUnderACancelledContext, TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode; internal/tools/console_open_test.go — TestConsoleOpen_ExitedProgramIsReportedNotHidden
**Closes:** process-ending-prose
**Tests:**
- New `TestConsoleResultsCarryTheirEnding` in `internal/tools/console_close_test.go`: open (alive), read after exit (exited N), close (killed).
- `TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode`, `TestConsoleTail_ZeroWaitReadsTheTailUnderACancelledContext`, `TestConsoleOpen_ExitedProgramIsReportedNotHidden` pass unchanged.
**Acceptance:**
- `go build ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConsole' ./internal/tools/`

**Commit:** `refactor(tools): a console's state travels as a ProcessEnding`

## 6. The TUI keeps a delegation's verdict typed

Depends on item 2.

**What:**
**Goal:** `branchSummary` carries a TUI-private `delegationVerdict` (done, no-report, stopped, capped, preempted, failed, interrupted, and the unstarted kinds) set once where a sub_agent result is absorbed; `summaryStyle`, `subAgentFinished`, `failedCalls`, `toolView.demoted` and `entry.neverStarted` read the field, never the slot's words. A typed outcome with `Unstarted` set renders exactly as today's prose-detected never-started result (refused stays red `error`; the others neutral); every unstarted kind counts as never-started for the Esc stop hint.
**Approach (assumed at the header base):** the verdict is derived from `domain.DelegationOutcome` plus `IsError` in `delegationStat` / `toolView.absorbFailure` (`enrichWithResult` path) and stored beside `failed` / `succeeded`; `subAgentSummary` and `demoted` carry it, retiring `runVerdict`'s word reparse. A result with no outcome derives the verdict through `proseDelegationOutcome` and the existing prose readers, unchanged here (item 8 folds them). The verdict's words are painted from the enum by `delegationOutcomeVerdict`.
**Regression guard.** Widen Acceptance's -run to include TestTranscriptCodec and TestCollapsedRun. Replay never absorbs a result: `fromWireToolView` (`internal/tui/transcriptbridge.go`) sets the verdict from the record's words through today's readers (item 8 swaps in the parser). The `delegationVerdict(content string)` func (`toolregistry.go:1235`) is retired or renamed so the type of that name compiles; its test callers move to `delegationOutcomeVerdict` or the new enum test. `failed` / `succeeded` stay the painted fields for every tool and workflow item (`workflowItemSummary`, `failedCalls`, `runAggregate`); the delegation verdict is read only on sub_agent heads and sets those fields there. With no outcome, an IsError result opening `sub-agent not started:` with no human-act text maps to the refused unstarted verdict. `subAgentFinished` withholds the ✓ only for the failed, no-report, stopped, preempted, interrupted and unstarted verdicts; the zero verdict keeps it (`TestTranscriptCodecReDerivesSubAgentSolo`'s `✓ ⋯ refused`). The verdict is set in `enrichWithResult` after the stat hook (and in `absorbFailure`), and green keeps today's rule — an unquoted verdict slot, or the demoted fallback: the item yields to F-29 (`internal/tui/toolview.go:120-124`, a quoted line never carries the success verdict). Item 6's interim replay reader in fromWireToolView reads the verdict word from the stored stat (w.Stat / the stat value) when the slot holds a quoted line, and from the slot text otherwise — the same quoted-slot/stat split item 8's parser guards; state it in item 6 and cover it with a test case.
**Files:** internal/tui/toolregistry.go, internal/tui/toolview.go, internal/tui/toolleader.go, internal/tui/subagentblock.go, internal/tui/transcript.go, internal/tui/transcriptbridge.go, internal/tui/toolregistry_test.go, internal/tui/subagentblock_test.go, internal/tui/toolpresent_test.go, internal/tui/toolbranch_test.go, internal/tui/transcriptbridge_test.go
**Read first:** internal/tui/toolview.go — enrichWithResult, absorbFailure, applyStat, demoted, namedSummary, typedSummary; internal/tui/toolregistry.go — delegationOutcomeOf, delegationStat, delegationOutcomeVerdict, delegationNeverStartedVerdict, delegationVerdict; internal/tui/subagentblock.go — subAgentFinished, subAgentVerdictWord, collapsedSubAgentView, subAgentSummary;
internal/tui/toolleader.go — summaryStyle; internal/tui/transcript.go — entry.neverStarted, inFlightFanOut; internal/tui/transcriptbridge.go — fromWireToolView; internal/tui/workflowblock.go — workflowItemSummary;
internal/tui/toolpresent_test.go — TestCollapsedRunDemotionKeepsTheLineAndNoBody
**Tests:**
- New `TestDelegationVerdictIsTypedOnTheSummary` in `internal/tui/toolregistry_test.go`: each outcome (incl. each `Unstarted` kind) yields its verdict, slot words and colour.
- `TestDelegationVerdict*`, `TestDelegationOutcomeOutranksTheProse`, `TestSubAgent*`, `TestSummaryStyleGreensOnlyTheDelegationVerdict`, `TestEscStopHintIgnoresDelegationsThatNeverStarted` pass.
- `TestTranscriptCodecReplaysAFinishedDelegationGreen`, `TestTranscriptCodecReplaysANoReportDelegationWithoutACheck`, `TestTranscriptCodecReDerivesSubAgentSolo` and `TestCollapsedRunDemotionKeepsTheLineAndNoBody` (its `runVerdict` column moved onto the verdict) pass.
- New `TestTranscriptCodecReadsAQuotedDelegationVerdictOffItsStat` in `internal/tui/transcriptbridge_test.go`: a replayed sub_agent head whose slot holds a quoted one-line report over a recorded `done` stat reads the done verdict; an unquoted slot reads its own text.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDelegation|TestSubAgent|TestSummaryStyle|TestEscStopHint|TestSummaryBearingDelegation|TestTranscriptCodec|TestCollapsedRun' ./internal/tui/`

**Commit:** `refactor(tui): a delegation's verdict stays typed on its summary`

## 7. A never-started delegation carries its typed outcome

Depends on items 2 and 6.

**What:**
**Goal:** every result `childBracket.neverStarted` returns carries `DelegationOutcome{Unstarted: …}` matching its unstarted ending (skipped, refused, cancelled, stopped-queued); its Content and the ledger row are unchanged.
**Approach (assumed at the header base):** `childEnding` (`internal/agent/childending.go`) maps its kind to `DelegationUnstarted` for the four `unstarted*` constructors; `neverStarted` (`internal/agent/childrun.go`) attaches it to `errorToolResult`'s result.
**Regression guard.** `TestChildBracket_NeverStartedBooksOneRowAndALoneFinish` (`internal/agent/childbracket_test.go:284`) pins `result == errorToolResult(...)` and that the lone finished phase carries it: its want carries the outcome, the new table test sits beside it on `newBracketHost`, and `TestChildBracket` joins Acceptance. Every prose site saying a never-started or queued delegation result carries no summary is rewritten by this item (item 2's decision) — the `DelegationOutcome` doc's "a never-ran result carries none" (`internal/domain/toolsummary.go`), headless.md's `sub_agent_phase` "a `delegation_outcome` for a run that started — or `null`", `childending.go`'s "An ending that never ran has no summary" — found by `grep -rn -i -E 'never ran|never-ran|for a run that started|that started — or|carries none|never carries one' docs/manual/headless.md apogee.go internal/domain internal/tools/doc.go internal/tui internal/agent`. The two unstarted-cancel returns outside `neverStarted` — `subagent.go`'s cancel before the child exists (`unstartedCancelled`) and its `dispatchCancelled` run answered with `cancelledQueuedDelegationContent` — attach the same `DelegationCancelled` outcome, so every never-started result carries one.
**Files:** internal/agent/childending.go, internal/agent/childrun.go, internal/agent/subagent.go, internal/agent/childending_test.go, internal/agent/childbracket_test.go, internal/domain/toolsummary.go, docs/manual/headless.md
**Read first:** internal/agent/childrun.go — childBracket.neverStarted, settle; internal/agent/childending.go — childEnding, unstartedSkipped, unstartedPastCeiling, unstartedCancelled, unstartedStopped, summary; internal/agent/childbracket_test.go — TestChildBracket_NeverStartedBooksOneRowAndALoneFinish, newBracketHost;
internal/agent/dispatch.go — settleCancelledDelegation, stopQueuedDelegation, errorToolResult; internal/agent/subagent.go — delegateEnding; docs/manual/headless.md — sub_agent_phase row
**Tests:**
- New `TestNeverStartedResultCarriesItsUnstartedOutcome` (table over the four constructors) in `internal/agent/childbracket_test.go`, on `newBracketHost`; the two `subagent.go` cancel returns carry `DelegationCancelled`.
- `TestChildBracket_NeverStartedBooksOneRowAndALoneFinish` passes with its want carrying the outcome.
**Acceptance:**
- `go build ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNeverStarted|TestChildEnding|TestDelegation|TestChildBracket' ./internal/agent/`
- The grep above prints no sentence saying a never-started delegation result carries no summary.

**Commit:** `feat(agent): a never-started delegation's result names why it never ran`

## 8. Delegation words are parsed in one place

Depends on items 6 and 7.

**What:** Recast at the regression check (2026-11-22).
**Goal:** one parser, `delegationVerdictFromWords` in `internal/tui/transcriptbridge.go`, maps recorded or summary-less delegation words (incl. the old-record `error` slot over a never-started body) to `delegationVerdict`; `fromWireToolView` and the summary-less live fallback both call it. `succeededSummary`, `endedWithoutReportSummary`, `stoppedSummary` and every live reader of copied engine prose (`delegationStoppedByUser`, `delegationNeverStartedVerdict`, `proseDelegationOutcome`, `readDelegationSteering`, `entry.neverStarted`'s prefix check) are deleted or become the parser's private parts.
**Approach (assumed at the header base):** move the matchers' word sets and the engine-prose constants into the parser; `namedSummary` stops classifying delegation words. The session record format is unchanged (ADR 0083 §1).
Prose guard: every comment naming a matcher or saying the TUI reads a delegation's words back is rewritten — `grep -rn -E 'succeededSummary|endedWithoutReportSummary|stoppedSummary|neverStartedVerdict|proseDelegationOutcome|runVerdict|delegationStoppedByUser|readDelegationSteering|delegationEndedWithoutReport|unstartedDelegationPrefix|delegationDetail' internal/tui/*.go`.
**Regression guard.** The item yields to toolleader.go's record of failedSummary as the all-tool failure reading (`internal/tui/toolleader.go:330-338`, checked against plan "2026-09-03 - 02" item 7) — failedSummary stays namedSummary's classifier for every tool; only the delegation words (succeeded, no-report, stopped, preempted) leave namedSummary/typedSummary. Add internal/tui/transcript.go, toolregistry_test.go, toolbranch_test.go, toolpresent_test.go and subagentblock_test.go to Files under the rule "every test naming a deleted reader is rewritten over the parser" (grep the deleted names in internal/tui/*_test.go). The prose guard's alternation names every symbol the item deletes or privatises (incl. delegationStoppedByUser, readDelegationSteering, delegationEndedWithoutReport, unstartedDelegationPrefix, delegationDetail). The new test's cases: pre-empted, cancelled and stopped-queued read neutral; the ceiling refusal reads refused (red, never-started); an unknown recorded word reads the zero verdict. fromWireToolView feeds the parser the slot text when the slot is unquoted and the recorded stat (w.Stat) when it is quoted — subAgentVerdictWord's rule — so a replayed `done` head behind a quoted one-line report keeps its green collapsed fallback row; the new test adds that case. The Goal makes no claim about what adding an outcome kind touches (eventjson's `unstarted` names, `delegationOutcomeVerdict`'s wording and item 7's `childEnding` change too); Acceptance runs the prose-guard grep over the deleted names, expecting hits only inside the parser.
**Files:** internal/tui/transcriptbridge.go, internal/tui/toolleader.go, internal/tui/toolview.go, internal/tui/toolregistry.go, internal/tui/transcript.go, internal/tui/transcriptbridge_test.go, internal/tui/toolregistry_test.go, internal/tui/toolbranch_test.go, internal/tui/toolpresent_test.go, internal/tui/subagentblock_test.go, internal/tui/doc.go
**Read first:** internal/tui/transcriptbridge.go — fromWireToolView; internal/tui/toolview.go — namedSummary, typedSummary, demoted, absorbFailure; internal/tui/toolleader.go — failedSummary, succeededSummary, endedWithoutReportSummary, stoppedSummary;
internal/tui/toolregistry.go — delegationOutcomeOf, proseDelegationOutcome, delegationNeverStartedVerdict, delegationStoppedByUser, readDelegationSteering, delegationSteering, delegationFailure; internal/tui/subagentblock.go — subAgentFinished, subAgentVerdictWord, collapsedSubAgentView;
internal/tui/transcript.go — entry.neverStarted, unstartedDelegationPrefix; internal/tui/subagentblock_test.go — TestSubAgentSkippedRowReplaysNeutralFromAnOldSession; internal/tui/transcriptbridge_test.go — TestTranscriptCodecReDerivesSubAgentSolo, TestTranscriptCodecReplaysAFinishedDelegationGreen
**Closes:** delegation-outcome-reparsed
**Tests:**
- New `TestDelegationVerdictFromWordsReadsEveryRecordedVerdict` in `internal/tui/transcriptbridge_test.go`: each painted verdict round-trips to its enum; pre-empted, cancelled and stopped-queued read neutral; the ceiling refusal reads refused (red, never-started); an unknown recorded word reads the zero verdict; a quoted one-line report over a recorded `done` stat reads done.
- Every test in `internal/tui/*_test.go` naming a deleted reader is rewritten over the parser; `TestTranscriptCodecReDerivesSubAgentSolo` passes.
- `TestTranscriptCodecReplaysANoReportDelegationWithoutACheck`, `TestSubAgentSkippedRowReplaysNeutralFromAnOldSession` pass.
**Acceptance:**
- `go build ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDelegation|TestSubAgent|TestTranscriptCodec|TestSummaryStyle|TestEscStopHint' ./internal/tui/`
- The prose-guard grep above prints hits only inside the parser (`delegationVerdictFromWords`'s private parts in `internal/tui/transcriptbridge.go`).

**Commit:** `refactor(tui): delegation words are parsed once, on replay`
