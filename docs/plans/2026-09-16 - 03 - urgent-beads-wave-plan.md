# Urgent beads wave — defects, Anthropic Messages wire, session fork

**Goal:** Close every open P2 bead and every open P3 bug in the register: six defect items (apogee-ku1, o4w, 5wb, mfh, 8as, 1mj), a second outbound wire codec — Anthropic Messages — selected per server by a `wire:` key (apogee-6fp), and session forking with a parent pointer on Meta and a `/fork` picker (apogee-zci).
**Date:** 2026-09-16 · **Status:** unexecuted · **Base:** 63d6e631
**Sized for:** ~200k-context host.

**Sources:** `bd show apogee-ku1 apogee-o4w apogee-5wb apogee-mfh apogee-8as apogee-1mj apogee-6fp apogee-zci`; `docs/adr/0031-*.md` (invariants); `docs/adr/0056-*.md` D2; `docs/adr/0060-*.md` D2/D3/D9; `docs/adr/0072-*.md`; `docs/adr/0074-*.md` D7; `docs/adr/0075-*.md` D10; `CONTEXT.md` §Upstream, §Session record, §Exchange, §Compaction; `docs/reviews/archived/session-mining-2026-09-14.md:33`; `docs/design/test-drivers.md`.

**Ratified design calls** (owner, 2026-09-16, via AskUserQuestion — do not re-ask):
- **Scope:** P2 wave + all open P3 bugs; apogee-ku1 is already fixed at `52e0d677` — its item is the structural guard + bead close.
- **Wire key name:** `wire: openai|anthropic` per server entry (zero value = openai); CONTEXT.md gains a *Wire* term; "dialect" stays the effort term.
- **Effort on the anthropic wire:** the wire implies the effort mapping (Effort → `output_config.effort`); a non-empty `effort-dialect:` on an anthropic entry is a `ValidateServers` refusal.
- **Thinking:** v1 never requests `thinking` on the anthropic wire (no signed-thinking replay); a signed-thinking carrier is a follow-up bead.
- **Discovery on anthropic:** `GET /v1/models` with anthropic headers, no `/props`; window comes from the `context-window:` pin.
- **Seam shape (writer, under the calls above):** one `*Client` with an unexported `wireCodec` selected by `provider.WithWire`; auth = `x-api-key` only; stubllm gains a `/v1/messages` route, its Recorder stays chat-completions.
- **Denial kill (apogee-1mj) — revised 2026-09-16 (via AskUserQuestion):** BOTH a stderr-only watch on the pipe path (`subprocess.Run`: stdout unwrapped, stderr through `DenialKillWriter`) AND a line-anchored signature (a line whose tail, after `TrimRight` of `" \t\r"`, ends in `Permission denied` / `Operation not permitted` (either case) plus an allowed tail, or contains `EACCES`/`EPERM` bounded by non-alphanumeric bytes); the PTY console keeps its single watched stream under the anchored rule; dated ADR 0056 amendment.
- **/fork invocation:** bare `/fork` opens a pop-up picker of the session's top-level prompts; ⏎ keeps the history through that Exchange.
- **Child record:** full copy of the prefix (cut engine State + cut transcript) + `Meta.ParentID`; no RecordVersion bump.
- **After fork:** save the parent at idle, then switch the TUI to the child via the sessions-browser resume flow; a note names the parent id.
- **Listing:** `⑂ <parent title>` tag in the browser's title cell (schedule-tag slot); flat sort unchanged.
- **Compacted stretch:** the picker lists only prompts after the last depth-0 `compacted` entry; none eligible → note `that stretch was folded — fork at a later block`, no fork.
- **Child state:** parent's title (auto-title latched off), task list cleared, usage/servedModels/ctxUsed zero, undo store starts empty (ADR 0074 D7).

**Regression check (2026-09-16, 63d6e631):**
- 1: guard folded (blank identifier excluded from the seam set; bite check reverses the fix hunk with `git apply -R`).
- 3: guard folded (`wantKinds` gains `ref_clipped`; `example_test.go` name list).
- 5: guard folded (a single over-cap merged window is cut with `capDefault` and gets the plain-read tail).
- 6: recast (owner revised call — stderr-only watch on pipes plus the anchored signature; supersedes the "Denial kill" call above and ADR 0056 D2's "false match … surfaces loudly" trade via the dated amendment).
- 7: guard folded — decision applied (`yaml:"wire,omitempty"`; acceptance gates on the key spelling).
- 8: guard folded — decision applied (codec-neutral wire capture tee in `Client.Stream`; `setAuth` stays the shared applier; `Client.Wire()` accessor).
- 9: guard folded — decision applied (`thinking: {"type":"disabled"}` always sent; yields to the header's *Thinking* call; no-tools requests fold tool history to text).
- 10: guard folded (fault rows are the in-band `overloaded_error` / `rate_limit_error` shapes; the wire-capture test for the anthropic stream lands here).
- 11: guard folded — decision applied (`apogee probe` dials with the entry's wire; anthropic Discover reports the effort dial; headless beat seams carry a `provider.Wire`).
- 12: guard folded (`cmd/apogee/upstream.go` replaces `wire_settings.go`; Dialer fake asserts `Client.Wire()`; judge dropped from Files; routed case in `dialer_test.go`).
- 14: guard folded (`stubllm.Request.Wire` judged; `launchTUIOn`/`eventLinesHome` homes; fixture under `testdata/stubllm/`).
- 16: guard folded (the clear lives in `decodeRecord`, serving List, Load and LoadPath alike).
- 17: recast (cut from the END — `CutSession(snap, dropExchanges)`; the overflow bridge is an opening).
- 18: recast (`forkPoint.drop` counted from the end; cancelled/aborted prompts marked and uncounted).
- 19: recast (the fork write goes through the record write queue as `writeFork`; resumed children keep `ParentID`; the host stamps `ParentID` from its own identity).
- 20: recast (`/fork` queues `writeFork` with `CutSnapshot(row.drop)`; the switch runs on the completion message; yields to sessionsave.go's one-queue rule; `picker.go` in Files).
- 21: guard folded (`sessionRowCells` gains one parent-title parameter resolved in `unfilteredRows`; nine test calls updated).
- 22: guard folded — decision applied (judged from the end via the stub request log; parent compared minus `UpdatedAt`; fixture under `testdata/stubllm/`; `/sessions` check after the child's prompt).
- 2, 4, 13, 15: SAFE.
- Re-check (2026-09-16, 63d6e631):
- 6: guard folded (tail after `: '`/`: "` runs to end of line, ` (N)` accepted; line start/end bound `EACCES`/`EPERM`, Perl tail's optional `.`; scan carries the unfinished line, not the 22-byte overlap; confinetest `runChainedClobber` re-wired stderr-only and the two "interleaved" claims name the exception).
- 17: guard folded — decision applied (n=0 keeps the messages but still normalises: tasks/PendingInput cleared; `TestCutSessionZeroIsAClone` pins it).
- 18: guard folded — decision applied (the mark lands only on foldCancelled/foldLoopError, never on a faulted exchangeDoneMsg — three-case test; pre-mark records cut one Exchange later, dated limitation in What; `wantEntry` pin in transcriptbridge_test.go extended).
- 19: guard folded (`UserMsgs` counted by the `userMessageCount` rule — every `entryUser`, depth>0 included — in the queue from the prefix).
- 20: guard folded (`popup-dropdown.txt` golden regenerated, `TestE2EPopupFramesLists` in Acceptance; `/fork` refused with the /sessions posture when `m.sessions` is nil; busy at the completion fold → switch skipped, `forked as <child id> — resume it from /sessions`).

**Standing requirements:**
- `skills: coding-standards`.
- Deviations from item text land as a dated NOTES line under the item.
- Every item that closes a bead runs `bd close <id> --reason="<commit subject>"` at its commit; the CHANGELOG entry travels in the item's sidecar.
- The engine stays wire-silent (ADR 0031): no item adds a network surface owned by `internal/agent`.

**Out of scope:** `Client.CanAskForNoReasoning()` (bead note, optional); signed-thinking replay on the anthropic wire (file a bead at item 9); Anthropic OAuth/Bearer tokens; the curl-23/useradd denial misses (no signature printed); headless `--resume`; block-cursor stops on user blocks; copying the parent's undo odb; every P3/P4 non-bug bead.

## 1. Guard: no parallel test swaps a package-level seam (apogee-ku1) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): the guard treats parallelism as transitive — a serial `t.Run` literal inside a parallel test, and a parallel `t.Run` literal inside a serial test, both count as running under `t.Parallel` (a subtest of a parallel test still races every other parallel test); the plan's "both calls `t.Parallel()` and assigns" reads as the direct case only — the superset passes at HEAD.
NOTES (2026-09-16): assignment targets rooted at a seam (`seam.field = …`, `seam[k] = …`, `seam++`) count as writes; a name the test function declares locally (`:=`, `var`, parameter, range variable) shadows the seam and is skipped — pinned by the fixture's `TestParallelShadow` case.
NOTES (2026-09-16): bead close is the verifier's at commit time — `bd close apogee-ku1 --reason="test(cmd/apogee): guard that no parallel test swaps a package-level seam — race fixed at 52e0d677, guard pins the shape"`.

**What:** apogee-ku1's race (`runOnce` swapped by a `t.Parallel()` test while a Scheduler firing read it) was fixed at `52e0d677`; this item pins the shape out. Add `TestNoParallelTestSwapsAPackageSeam` to `cmd/apogee/seams_guard_test.go`: with `go/parser` collect every identifier declared by a top-level `var` in the package's non-test files (the seam set: `runOnce`, `hardExit`, `interruptSignals`, `prewarmLabelWalk`, `discoverBeat`, `discoverDelegationBeat`, `tuiScheduleClock`, `daemonClock`, `acquireDaemonLock`, `watchSchedules`, `daemonExecutable`, `daemonUserHome`, `probeKeyStore`, `probeTerminalStreams`, `newConfiner`, `hostToolchain` today — derived, never a literal list), then fail for any test func or `t.Run` literal that both calls `t.Parallel()` and assigns to one of them, reporting `file:line`. Close the bead with the commit subject and a reason naming `52e0d677`.

**Regression guard.** The seam set excludes the blank identifier: `var _ io.Writer = daemonLogWriter{}` (cmd/apogee/daemonfire.go:480; also launcher.go:94, wire.go:74, wire_session.go:88, :331) must not put `_` in the set, or the eleven parallel tests that write `_ = …` (e.g. launcher_test.go:256, wire_session_test.go:112) fail the guard at HEAD. The bite check reverses the fix hunk — `git show 52e0d677 -- cmd/apogee/schedule_test.go | git apply -R` (applies cleanly at HEAD) — never `git checkout 21468104 -- …`, whose file no longer compiles at HEAD (`e2eWriteFinal` and `config.Options.StartupMaxOutputTokens` are gone), so `go test` would fail at compile, never at the guard; the synthetic string fixture stays the in-suite bite.

**Files:** `cmd/apogee/seams_guard_test.go`
**Read first:** cmd/apogee/headless.go — runOnce, hardExit, interruptSignals; cmd/apogee/schedule_test.go — TestScheduleFiringCarriesTheSessionsSyncLane, TestScheduleFiringCarriesTheParallelAgentsWidth; cmd/apogee/daemonfire_test.go — parser.ParseFile precedent (parser.SkipObjectResolution); cmd/apogee/launcher.go — liveLauncherOps; cmd/apogee/wire.go — newConfiner

**Tests:** the guard itself; a self-check that it would fail on a synthetic source (parse a string fixture with a parallel test assigning `runOnce`) so the detector is proven, not assumed.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'TestNoParallelTestSwapsAPackageSeam' -count=1`
- `git show 52e0d677 -- cmd/apogee/schedule_test.go | git apply -R && go test ./cmd/apogee/ -run TestNoParallelTestSwapsAPackageSeam -count=1; git checkout HEAD -- cmd/apogee/schedule_test.go` → FAILS with the fix hunk reversed (bite check), passes at HEAD.

**Commit:** `test(cmd/apogee): guard that no parallel test swaps a package-level seam`

## 2. headless --help names exit code 3 (apogee-o4w) — ✅ DONE (2026-09-16)

**What:** Fix: `cmd/apogee/headless.go` `newHeadlessCommand` Long text lists exit codes 0/1/2 and omits `exitRunFaulted = 3`. Extend the sentence with `3 the run started but its final turn was abandoned (stdout holds its last text, not an answer; the record is saved)` and add `a server that did not answer` to the exit-2 clause so help and `docs/manual/headless.md` agree. Add `TestHeadlessHelpNamesEveryExitCode` in `cmd/apogee/headless_help_test.go`: one `N the run` phrase per exit const, and the manual's exit table carries the same four rows (shape of `docs_env_test.go`'s manual-vs-code tests).

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_help_test.go`
**Read first:** cmd/apogee/headless.go — newHeadlessCommand (Long), exitRunFailed, exitNotStarted, exitRunFaulted; docs/manual/headless.md — the `| Exit | Means |` table; cmd/apogee/docs_eventlines_test.go — manualHeadlessPath, TestManualListsEveryEventLineKind; cmd/apogee/docs_env_test.go — TestManualListsEveryEnvironmentOverride

**Tests:** `TestHeadlessHelpNamesEveryExitCode`.

**Acceptance:**
- `go build ./... && go test ./cmd/apogee/ -run TestHeadlessHelpNamesEveryExitCode -count=1`
- `go run ./cmd/apogee headless --help | grep -c '3 the run'` → 1

**Commit:** `fix(headless): --help names exit code 3 and the unanswered-server case`

## 3. Public Event alias list gains RefClippedEvent (apogee-5wb) — ✅ DONE (2026-09-16)

**What:** Fix: `apogee.go`'s Event alias type block (18 members) lacks `RefClippedEvent = domain.RefClippedEvent`, so an embedder cannot type-switch on it or reach `Notice()`. Add it beside `PruneEvent`. Add `TestEveryDomainEventVariantIsAliased` in `apogee_alias_internal_test.go` (package `apogee`, precedent `version_internal_test.go`): parse `internal/domain/events.go`, collect every struct embedding `EventBase`, and require an `X = domain.X` spec for each in `apogee.go`. Extend `TestFacadeExportsEventLines` (`apogee_test.go`) to emit an `apogee.RefClippedEvent` and call `.Notice()`.

**Regression guard.** `TestFacadeExportsEventLines` pins three lines and `wantKinds` (apogee_test.go:230-239), and eventjson encodes `RefClippedEvent` as `ref_clipped` (internal/eventjson/encode.go:29): emitting the event through the sink adds a fourth line, so the extension also appends `"ref_clipped"` to `wantKinds` (or calls `.Notice()` on a value without emitting it through the sink). `example_test.go`'s exported-name list may gain `_ apogee.RefClippedEvent` beside `PruneEvent`.

**Files:** `apogee.go`, `apogee_alias_internal_test.go`, `apogee_test.go`, `example_test.go`
**Read first:** apogee.go — the `type (…)` Event alias block (PruneEvent, WireEvent); internal/domain/events.go — RefClippedEvent, RefClippedEvent.Notice, EventBase; apogee_test.go — TestFacadeExportsEventLines; internal/eventjson/encode.go — kindRefClipped; version_internal_test.go — package apogee precedent; example_test.go — the exported-name list

**Tests:** as named; `TestFacadeExportsEventLines` grows `wantKinds` by `"ref_clipped"` when it emits the event through the sink.

**Acceptance:**
- `go test . -run 'TestEveryDomainEventVariantIsAliased|TestFacadeExportsEventLines' -count=1`
- bite check: the alias test fails with the added line reverted.

**Commit:** `fix(apogee): alias RefClippedEvent on the public Event list and pin the list complete`

## 4. wrap-up runs the output-path write once (apogee-mfh) — ✅ DONE (2026-09-16)

**What:** Fix: `internal/agent/subagent.go` `wrapUpCalls` keeps every `write_file` call in the wrap-up reply, so two writes to the output path both run although `wrapUpOutputClauseFormat` says `You may still call write_file once, for %s only.` Keep the FIRST `write_file` whose `classifyWriteTarget(tool, call).real == a.outputTarget` and drop later output-path writes; writes aimed elsewhere keep flowing so `resolution.go`'s `wrap-up: only %s may be written` refusal still reaches the transcript. Update the func comment and the `loop.go` call-site comment.

**Files:** `internal/agent/subagent.go`, `internal/agent/loop.go`, `internal/agent/subagent_test.go`
**Read first:** internal/agent/subagent.go — wrapUpCalls, wrapUpWriter, wrapUpOutputClauseFormat; internal/agent/loop.go — the `a.turns.wrappingUp()` call filter; internal/agent/dispatch.go — classifyWriteTarget; internal/agent/subagent_test.go — outputPathAgent, TestSubAgent_OutputPathKeepsWriteFileInTheWrapUp; CONTEXT.md — the wrap-up exception paragraph ("dispatches that one call")

**Tests:** `TestWrapUpCallsKeepsTheFirstOutputWrite` (unit: elsewhere, output, output → kept = elsewhere + first output); `TestSubAgent_WrapUpRunsTheOutputWriteOnce` (journey over `outputPathAgent`: a wrap-up reply with two `write_file` calls to the output path with different content → file holds the first content, exactly one depth-1 `ToolResultEvent` for write_file).

**Acceptance:**
- `go test ./internal/agent/ -run 'TestWrapUpCalls|TestSubAgent_WrapUp|TestSubAgent_OutputPath' -count=1`
- bite check: the journey test fails against the pre-item `wrapUpCalls`.

**Commit:** `fix(agent): the wrap-up keeps one write_file to the output path, not every one`

## 5. read_file locate windows honour the default cap (apogee-8as) — ✅ DONE (2026-09-16)

NOTES (2026-09-16): the `locate` schema property's own sentence gained "bounded like a plain read" alongside the description sentence the item names — both are the model-facing promise; `git_show`'s description ("takes the same range arguments as read_file") left as is, it inherits the cap through renderFile.
NOTES (2026-09-16): the many-hits test carries four rows — the two the item names plus a byte-bound row that drops a wide window and one that cuts a lone wide window — so the "wide-line byte-bound variant" lives in the same table rather than a second test.
NOTES (2026-09-16): `docs/manual/commands.md` "How much a read returns" passage extended to state the locate bound and the bounded `Located …` line (user-facing behaviour).

**What:** Fix: `internal/tools/read_file.go` `renderFile`'s locate branch bypasses `capDefault`, so a range-less `locate` with hundreds of hits renders every merged ±10-line window, and `locateReport`'s `Located %q on lines: …` line is unbounded too. In `locateWindows` accumulate merged windows only while the joined body (windows plus `…` separators) stays within `defaultReadLines`/`defaultReadBytes`, always keeping at least one window, never cutting inside a window; when windows were dropped append the tail `\n[showing the windows around %d of %d hits — pass start_line/end_line for the rest]` and set `span.End` to the last shown window. Bound `locateReport` to the first 40 line numbers plus `… and %d more`. Update the tool description sentence that promises the cap so it covers locate.

**Regression guard.** Hits closer than 21 lines apart merge into ONE window (`mergeLineWindows`, internal/tools/grep.go:691), so "never cutting inside a window, always keeping at least one" would leave a term hitting every ≤20 lines of a 2000-line file rendering all 2000 lines with no tail — the common case the bead names. When the first window alone exceeds the cap, cut it with `capDefault` (whole lines) and emit the plain-read `[showing lines S-E of M — pass start_line/end_line for the rest]` tail; that row joins the many-hits test.

**Files:** `internal/tools/read_file.go`, `internal/tools/read_file_test.go`
**Read first:** internal/tools/read_file.go — renderFile, locateWindows, locateReport, capDefault; internal/tools/grep.go — mergeLineWindows; internal/tools/git.go — GitShow.Execute (second renderFile caller, inherits the cap); internal/tools/read_file_test.go — TestReadFile_Execute_WindowsALocateWithNoRange; internal/tui/toolregistry.go — readSpanStat

**Tests:** `TestReadFile_Execute_CapsALocateWithManyHits` (a 2000-line file hitting every 25 lines → body ≤ 400 lines, the exact tail above, span ends at the last shown window, report line ends in `… and N more`; a second row hitting every 10 lines — one merged window — → cut by `capDefault` with the plain-read `[showing lines S-E of M — …]` tail); a wide-line byte-bound variant; `TestReadFile_Execute_WindowsALocateWithNoRange` table unchanged.

**Acceptance:**
- `go test ./internal/tools/ -run 'TestReadFile_' -count=1`
- bite check: the many-hits test fails against the pre-item tree.

**Commit:** `fix(tools): read_file caps locate windows and the hit list like a plain read`

## 6. Denial kill matches a line-anchored signature (apogee-1mj)

**What:** Recast at the regression check (2026-09-16). Fix: `internal/platform/denialkill.go` matches `confinementDenialSignatures` by `strings.Contains` anywhere in the stream — on stdout and stderr alike — so a confined command that merely prints the phrase, or (the cited incident shape, session-mining `fc413fb5`) `cat`s a log to STDOUT whose lines END in a real Go denial (`open /dev/ptmx: permission denied`), is killed. Two changes. (1) Stderr-only watch on pipes: in `internal/subprocess/subprocess.go` `Run`, `cmd.Stdout = &out` unwrapped and `cmd.Stderr = NewDenialKillWriter(&out, cancel)`; because two writer values give os/exec two copiers, `CappedBuffer` gains a mutex, and its doc comment states that the combined output is no longer strictly byte-interleaved. The PTY console (`internal/console/process.go`) keeps its single watched stream under the anchored rule. (2) Anchored rule (binding): a line whose tail — after `TrimRight` of `" \t\r"` — ends in `Permission denied` / `permission denied` / `Operation not permitted` / `operation not permitted` followed by an allowed tail (empty, `)`, ` (os error N)`, `: '<path>'` or `: "<path>"`, ` at <file> line N`), or contains `EACCES`/`EPERM` bounded by non-alphanumeric bytes (so `EACCES:` and `Errno::EACCES` match); the final newline-less line counts as a line in both `LooksLikeConfinementDenial` and `DenialKillWriter.scan` (which keeps its cross-write tail so a line split across writes still matches). Amend ADR 0056 D2 with a dated paragraph recording both changes (stderr-only watch on pipes, anchored signature) and why (the cited fc413fb5 incident was a stdout cat of a log whose lines END in the real signature); update the `DenialKillWriter` doc comment. Rule for prose: every comment or doc line describing the watch as matching "anywhere"/"any byte" or "both streams" (`grep -rn "signature" internal/platform internal/subprocess internal/console docs/adr/0056*`) states the new rule.

**Regression guard.** Owner revised call (2026-09-16, via AskUserQuestion — record in "Ratified design calls", replacing the "Denial kill" line): BOTH a stderr-only watch AND the line-anchored rule. Recast What: in internal/subprocess/subprocess.go Run, `cmd.Stdout = &out` unwrapped and `cmd.Stderr = NewDenialKillWriter(&out, cancel)`; because two writer values give os/exec two copiers, `CappedBuffer` gains a mutex (state the combined output is no longer strictly byte-interleaved and say so in its doc comment); the PTY console (internal/console/process.go) keeps its single watched stream under the anchored rule. Anchored rule (binding): a line whose tail — after TrimRight of " \t\r" — ends in `Permission denied` / `permission denied` / `Operation not permitted` / `operation not permitted` followed by an allowed tail (empty, `)`, ` (os error N)`, `: '<path>'` or `: "<path>"`, ` at <file> line N`), or contains `EACCES`/`EPERM` bounded by non-alphanumeric bytes (so `EACCES:` and `Errno::EACCES` match); the final newline-less line counts as a line in both LooksLikeConfinementDenial and DenialKillWriter.scan. Tests add the Python `PermissionError: [Errno 13] Permission denied: '/etc/x'`, Rust `Permission denied (os error 13)`, Java `(Permission denied)`, Perl `Permission denied at x.pl line 3.`, Node `Error: EACCES: permission denied, open '/x'` rows (true) and the prose rows `permission denied for user x` / `note: permission denied earlier` (false); `TestRunSubprocessDenialWatchIgnoresStdout` (confined script echoes `open /dev/ptmx: permission denied` on STDOUT then writes a file → not DenialStopped, write lands) replaces the prose-only subprocess test; internal/console/process_test.go `TestProcessConfinedDenialStopsTheConsole` must stay green (its `\r\n` line). Files add internal/subprocess/subprocess.go and internal/console/process_test.go. The ADR 0056 amendment records both changes (stderr-only watch on pipes, anchored signature) and why (the cited fc413fb5 incident was a stdout cat of a log whose lines END in the real signature). Acceptance keeps `-race`. The bead is fully closed by this item; the fix's What names the cited incident shape. Superseded sources: internal/platform/denialkill.go:16-18 and ADR 0056 D2's recorded trade ("a false match is confined to confined runs and surfaces loudly") — the dated amendment records their replacement. Re-check (2026-09-16), four refinements of the anchored rule, binding: (a) the allowed tail after `: '` / `: "` is ANYTHING to end of line (covers Python's two-path `Permission denied: '/a' -> '/b'`), and ` (N)` is accepted beside ` (os error N)` (rsync's `Permission denied (13)`); only "no signature printed" misses (curl-23/useradd) stay out of scope. (b) Line start and line end count as the non-alphanumeric boundaries for `EACCES`/`EPERM` (the existing `write failed: EPERM` / `write failed: EACCES` rows in internal/platform/denialkill_test.go and the `write failed: EPERM` row of internal/tools/terminal_test.go's denial-label table stay green), and the Perl tail is ` at <file> line N` with an optional trailing `.`. (c) `DenialKillWriter.scan` carries the current UNFINISHED line between writes (capped, e.g. 4 KiB), not the fixed `denialSignatureOverlap` tail — a line split inside a long allowed tail must still match; `TestDenialKillWriterMatchesAcrossWriteBoundary` keeps its split-inside-the-signature shape. (d) internal/platform/confinetest/confinetest.go `runChainedClobber` is re-wired stderr-only (stdout to its own buffer) so the battery keeps proving the production shape, and its "exactly as the terminal tool's runSubprocess does" comment stays true; the prose rule extends to `grep -rn "interleav" internal/subprocess internal/tools/exec_common.go` so the two "interleaved order is the truthful one" claims (internal/subprocess/subprocess.go `SplitStdout` doc, internal/tools/exec_common.go:196) name the confined-run exception.

**Files:** `internal/platform/denialkill.go`, `internal/platform/denialkill_test.go`, `internal/platform/confinetest/confinetest.go`, `internal/subprocess/subprocess.go`, `internal/subprocess/subprocess_test.go`, `internal/tools/exec_common.go`, `internal/console/process_test.go`, `docs/adr/0056-*.md`
**Read first:** internal/platform/denialkill.go — confinementDenialSignatures, denialSignatureOverlap, LooksLikeConfinementDenial, DenialKillWriter.scan; internal/subprocess/subprocess.go — run, CappedBuffer; internal/platform/confinetest/confinetest.go — runChainedClobber, chained_script_clobber_denied; internal/tools/terminal.go — confinementDenialLabel switch; internal/subprocess/subprocess_test.go — TestRunSubprocessDenialWatchKillsConfinedRun, fakeConfiner; internal/tools/terminal_test.go — the denial-label table (`write failed: EPERM` row); internal/console/process_test.go — TestProcessConfinedDenialStopsTheConsole

**Tests:** `TestLooksLikeConfinementDenial` gains rows: true — `open /etc/x: Permission denied`, Python `PermissionError: [Errno 13] Permission denied: '/etc/x'`, Rust `Permission denied (os error 13)`, Java `(Permission denied)`, Perl `Permission denied at x.pl line 3.`, Node `Error: EACCES: permission denied, open '/x'`, Ruby `Errno::EACCES`, Python os.rename `PermissionError: [Errno 13] Permission denied: '/a' -> '/b'`, rsync `Permission denied (13)`, the existing `write failed: EPERM` / `write failed: EACCES` rows (errno at line end); false — `permission denied for user x`, `note: permission denied earlier`; a `\r`-terminated line and a final newline-less line both match. `TestDenialKillWriterMatchesAcrossWriteBoundary` covers a line-end split across writes and gains a case split inside a long allowed tail (`…Permission denied: '/etc/some/long/pa` + `th'\n`). The `write failed: EPERM` row of internal/tools/terminal_test.go's denial-label table and the confinetest battery's `chained_script_clobber_denied` (now stderr-only wired) stay green. `TestRunSubprocessDenialWatchIgnoresStdout` (confined script echoes `open /dev/ptmx: permission denied` on STDOUT then writes a file → not `DenialStopped`, the write lands); `TestRunSubprocessDenialWatchKillsConfinedRun` keeps killing on a stderr denial; a `CappedBuffer` concurrent-write test under `-race`. `TestProcessConfinedDenialStopsTheConsole` (internal/console/process_test.go, its `\r\n` line) stays green.

**Acceptance:**
- `go test -race ./internal/platform/ ./internal/subprocess/ ./internal/console/ -count=1`
- bite check: the prose rows and `TestRunSubprocessDenialWatchIgnoresStdout` fail against the pre-item tree.

**Commit:** `fix(platform): the confinement denial kill watches stderr on pipes and matches a line-anchored signature`

## 7. Per-server `wire:` key — config, template, manual, term, ADR (apogee-6fp)

**What:** Add `Wire string \`yaml:"wire,omitempty"\`` to `internal/config/config.go` `ServerEntry` with a doc paragraph in the `effort-dialect` style; `ValidateServers` refuses values other than empty/`openai`/`anthropic` with `apogee: servers: entry %d (%q): wire: …` and refuses a non-empty `effort-dialect` on an `anthropic` entry (`wire: anthropic sets the effort spelling itself — drop effort-dialect`); helper `isKnownWire`. Template `internal/config/defaults/config.yaml`: a `wire:` stanza after `effort-dialect` and the `endpoint` comment widened (`/v1/messages` on the anthropic wire); manual `docs/manual/configuration.md` `## The servers you run models on` key list gains `wire`. CONTEXT.md: a *Wire* glossary entry (the request/response protocol family a server speaks; `openai` chat-completions, `anthropic` Messages) and the *Upstream* entry widened from "OpenAI HTTP surface". Write ADR 0078 "a server's wire is a per-entry codec inside the provider Client" (decisions: key, zero value, effort implication, no thinking in v1, x-api-key auth, discovery shape, stub route).

**Regression guard.** `Wire string \`yaml:"wire,omitempty"\`` — omitempty is binding (renderServerEntry marshals the struct on legacy migration; TestMigrateLegacyConfigFoldsTheQuadruple's pinned bytes must stay green). Acceptance replaces the `wc -l ≥ 4` grep with `grep -nE '^\s*#?\s*wire:' internal/config/defaults/config.yaml docs/manual/configuration.md` ≥ 2 and `grep -n '\*\*Wire\*\*' CONTEXT.md` ≥ 1.

**Files:** `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/defaults/config.yaml`, `docs/manual/configuration.md`, `CONTEXT.md`, `docs/adr/0078-a-servers-wire-is-a-per-entry-codec-inside-the-provider-client.md`
**Read first:** internal/config/config.go — ServerEntry, ValidateServers, isKnownEffortDialect; internal/config/configmigrate.go — renderServerEntry; internal/config/config_test.go — TestApplyConfigServersInvalid; internal/config/configmigrate_test.go — TestMigrateLegacyConfigFoldsTheQuadruple; internal/config/defaults_test.go — TestEmbeddedDefaultConfigTeachesTheServersSchema; internal/config/unknownkeys.go — serverEntryType

**Tests:** `TestValidateServersRefusesAnUnknownWire`, `TestValidateServersRefusesEffortDialectOnTheAnthropicWire`, `TestServerEntryWireRoundTrips` (yaml → entry); `TestMigrateLegacyConfigFoldsTheQuadruple` (pinned migration bytes — no `wire: ""` written) stays green; template/manual gates (`defaults_test.go`, `cmd/apogee/docs_settings_test.go`) still pass.

**Acceptance:**
- `go test ./internal/config/ -count=1 && go test ./cmd/apogee/ -run 'Docs|Template|Defaults' -count=1`
- `grep -nE '^\s*#?\s*wire:' internal/config/defaults/config.yaml docs/manual/configuration.md | wc -l` ≥ 2 and `grep -n '\*\*Wire\*\*' CONTEXT.md | wc -l` ≥ 1

**Commit:** `feat(config): per-server wire: key selects the outbound protocol family (ADR 0078)`

## 8. Provider codec seam — the OpenAI wire behind `wireCodec` (apogee-6fp)

**What:** Structural, byte-identical. In `internal/provider` introduce exported `type Wire string` (`WireOpenAI = "openai"`, `WireAnthropic = "anthropic"`, `WireFor(name string) Wire` mapping ""→openai) and option `WithWire(Wire)`; inside `Client` an unexported `wireCodec` interface — `path() string`, `headers(apiKey string) map[string]string`, `encode(Request) (body []byte, carriesEffort bool, err error)`, `decodeWhole(io.Reader) (RawResponse, *wireError, error)`, `parseSSE(io.Reader, carried bool, yield func(Delta) bool)`. Move `buildBody`/`formatMessage`/`applyEffort`/`chatCompletionResponse` decode/`parseSSE` into `openaiCodec` (`internal/provider/wire_openai.go`); `send`/`do`/retry/`sanitize`/`observeWire`/`classify` stay shared; `setAuth` becomes codec headers. `WithChatPath` keeps working by overriding the openai codec's path. Unknown `Wire` in `WithWire` → openai. Binding standards: one deep module per codec; no dialect branches outside the codec.

**Regression guard.** Wire capture stays codec-neutral in the Client: `Client.Stream` tees the response body into the wireObserver capture (joined `data:` payloads as today, or the raw body when the codec is not openai) BEFORE handing the reader to `codec.parseSSE`; the codec interface takes no observer. Add a test that a WithWireObserver client records the anthropic stream's response (fixture) — the test lands in item 10 when the codec exists; item 8 states the seam. Further (verified): `setAuth` has two callers outside `do` — `discoverModels` and `discoverProps` (internal/provider/discovery.go:291, :333) — so `(c *Client) setAuth(h http.Header)` stays as the one shared applier of `c.codec.headers(c.apiKey)` (client.go:664) and discovery.go is untouched until item 11; today's parser routes in-band errors through `c.inBandErrorDelta → c.sanitize` (stream.go:253, :154), so the codec's `parseSSE`/`decodeWhole` receive the `*Client` (or its sanitize func) for that redaction only — `TestWireObserver_StreamRecordsSanitisedErrorBody` (stream_test.go:1166) stays green. Item 12's Dialer test needs `func (c *Client) Wire() Wire` — add the accessor here.

**Files:** `internal/provider/client.go`, `internal/provider/stream.go`, `internal/provider/wirejson.go`, `internal/provider/wire_openai.go`, `internal/provider/wire.go`, `internal/provider/doc.go`
**Read first:** internal/provider/client.go — Client, do, setAuth, buildBody, sanitize; internal/provider/stream.go — Stream, parseSSE, inBandErrorDelta

**Tests:** existing `client_test.go`/`stream_test.go`/`reliability_test.go` unchanged and green (they are the byte-identity net — `TestWireObserver_StreamRecordsEveryDataPayload` / `_StreamRecordsSanitisedErrorBody` prove the capture tee and the redaction, the `Bearer tok` pins prove `setAuth`); `TestWireForDefaultsToOpenAI`; `TestClientWireReportsTheOption`; a request-body golden captured before the refactor in `TestOpenAICodecBodyIsUnchanged`.

**Acceptance:**
- `go build ./... && go test ./internal/provider/ ./internal/agent/ ./internal/heartbeat/ -count=1`
- `git diff --stat` shows no test file in `internal/provider` edited except the two additions.

**Commit:** `refactor(provider): the OpenAI wire moves behind an unexported codec selected by WithWire`

## 9. Anthropic codec — request encoder and whole-response decoder (apogee-6fp)

**What:** Depends on item 8. `internal/provider/wire_anthropic.go`: `anthropicCodec` — path `/v1/messages`; headers `x-api-key` (when a key is set), `anthropic-version: 2023-06-01`, JSON content type; encode: every role-`system` message folds into top-level `system` (joined by blank lines), consecutive role-`tool` messages fold into ONE user message of `tool_result` blocks (`tool_use_id`, `content`, `is_error` from `ToolOutcome` when the seam carries it — else absent), assistant tool calls become `tool_use` blocks with `input` = the raw JSON string re-marshalled as an object (invalid JSON → an encode error naming the call id), `tools[]{name, description, input_schema}`, `max_tokens` required (fallback 4096 when `Sampling.MaxTokens` is nil), sampling fields only when set, `stream` as requested, `ThinkingEffort` → `output_config.effort` (off/none/minimal→omitted, low/medium/high/xhigh/max passed) and `thinking: {"type":"disabled"}` ALWAYS written (no thinking is ever requested); `carriesEffort` true when effort was written. decodeWhole: `content[]` text→Content, `tool_use`→ToolCalls (arguments re-stringified), `stop_reason` end_turn/stop_sequence→`stop`, max_tokens→`length`, tool_use→`tool_calls`, other→passed through; usage input+cache_read→PromptTokens, output→CompletionTokens, sum→TotalTokens, cache_read→CachedPromptTokens; error body `{type:"error",error:{type,message}}` → `wireError`. File the signed-thinking follow-up bead (`bd create`) and cite its id in the codec doc comment.

**Regression guard.** Reviewer premise (current Anthropic models run adaptive thinking when `thinking` is omitted, whose blocks must be replayed) — keep the ratified "never requests thinking" call by sending it explicitly: the encoder ALWAYS writes `thinking: {"type":"disabled"}`; `output_config.effort` is still written when effort is set (documented as independent of the thinking mode); a test pins the `thinking` key present with type disabled on every request. The item yields to the header's *Thinking* call (v1 requests no thinking) — this is how that call is satisfied on the current models. Further (verified): `compactCompleter` sends the whole history with nil tools (internal/agent/compact.go:574) and the openai wire folds role-`tool` messages to user text and drops tool calls in that case (client.go:696-700, `formatMessage`); tool_use/tool_result blocks with no `tools[]` are an Anthropic 400, so when `len(req.Tools) == 0` the encoder folds role-tool messages into plain user text and renders assistant tool calls as text, never as blocks.

**Files:** `internal/provider/wire_anthropic.go`, `internal/provider/wire_anthropic_test.go`
**Read first:** internal/provider/wire.go — Message, ToolCall, ToolSpec, Request; internal/provider/client.go — formatMessage, applyEffort; internal/agent/compact.go — compactCompleter.Complete; internal/agent/wire.go — toProviderRequest

**Tests:** table tests over encode (system fold, tool fold, tool_use input, max_tokens fallback, effort mapping, `thinking` present with `type: disabled` on every request — with and without effort, headers, the no-tools fold: a tool history with `len(Tools) == 0` renders no tool_use/tool_result block); decodeWhole for each stop_reason and the usage map; error body.

**Acceptance:**
- `go test ./internal/provider/ -run 'Anthropic' -count=1 && go vet ./internal/provider/`

**Commit:** `feat(provider): Anthropic Messages codec — request encoder and whole-response decoder`

## 10. Anthropic SSE parser and fault arms (apogee-6fp)

**What:** Depends on item 9. `internal/provider/wire_anthropic_stream.go`: `anthropicCodec.parseSSE` keyed on the JSON `type` of each `data:` payload (never `[DONE]`): `message_start` → Model + input usage; `content_block_start` text/tool_use (open a tool call in `openToolCalls` by block index with id+name); `content_block_delta` `text_delta`→content Delta, `input_json_delta`→argument fragment, `thinking_delta`→Thinking Delta; `content_block_stop`; `message_delta` → FinishReason (mapped as item 9) + output usage; `message_stop` → flush tool calls, Done; `ping` ignored; in-band `error` → `inBandErrorDelta`. Existing caps (`maxToolCallBytes`, `maxReplyTextBytes`) apply. `fault.go` gains the one classifier arm the header comment reserves: `529`/`overloaded_error` retryable, `rate_limit_error` as 429, and `prompt is too long` (400 `invalid_request_error`) as context overflow; `isContextOverflow` gains that marker.

**Regression guard.** The "529" row cannot bite: `isRetryableStatus` is `status >= 500` (client.go:718-720), so `classify(529, …)` is already retryable at BASE and `TestClassify` passes it before the item lands. Make the row the in-band shape that is new — `status: 0, errType: "overloaded_error"` → retryable — and pin `rate_limit_error` the same way (status 0 → code 429, retryable), beside the existing `rate_limit_exceeded` row (fault_test.go:105-111), which stays non-retryable. Item 8's capture tee is proven here: a `WithWireObserver` client records the anthropic stream's response from the fixture.

**Files:** `internal/provider/wire_anthropic_stream.go`, `internal/provider/wire_anthropic_stream_test.go`, `internal/provider/fault.go`, `internal/provider/fault_test.go`
**Read first:** internal/provider/stream.go — parseSSE, openToolCalls.fold, openToolCalls.open, inBandErrorDelta; internal/provider/fault.go — classify, fault; internal/provider/client.go — isContextOverflow, isRetryableStatus

**Tests:** a scripted SSE fixture with text + two interleaved tool_use blocks → Deltas in order, arguments assembled per index, `FinishReason == "tool_calls"`, usage mapped; a thinking_delta fixture; an in-band error mid-stream; `TestWireObserver_StreamRecordsTheAnthropicResponse` (the fixture's stream lands in a `WireRecord` through the Client's tee); fault table rows for in-band `overloaded_error` (status 0 → retryable), `rate_limit_error` (status 0 → 429, retryable), prompt-too-long (context overflow); `rate_limit_exceeded` stays non-retryable.

**Acceptance:**
- `go test ./internal/provider/ -run 'Anthropic|Fault|classify' -count=1`

**Commit:** `feat(provider): Anthropic SSE parser and the second wire's fault arms`

## 11. Discovery on the anthropic wire; Monitors carry the wire (apogee-6fp)

**What:** Depends on item 8. `internal/provider/discovery.go`: under `WireAnthropic`, `Discover` issues `GET /v1/models` with the codec's headers, decodes `{data:[{id,display_name}]}` into `ModelInfo` (no window, no slots, no effort tell; `resolveHint` grades as today), skips `/props`, and reports `EffortSupport` only from `forceEffortDialect`. `internal/heartbeat/heartbeat.go` `NewMonitor` needs no signature change (opts...); the four call sites — `cmd/apogee/wire_server.go`, `cmd/apogee/upstream.go`, `cmd/apogee/headless.go` (two), `cmd/apogee/delegation.go` — pass `provider.WithWire(provider.WireFor(entry.Wire))`. `docs/manual/probe.md` and `internal/probe/host.go`/`doc.go` report text: `GET /v1/models` stays literal (true on both wires) but the `/props` line is qualified as openai-only.

**Regression guard.** `apogee probe` dials wire-less — internal/probe/host.go / internal/probe/discovery.go pass `provider.WithWire(provider.WireFor(entry.Wire))` from the probed entry; add both to Files and a probe test that an anthropic entry's discovery request carries the anthropic headers. Further (verified): "reports `EffortSupport` only from `forceEffortDialect`" is always the zero value on this wire, because item 7 refuses every non-empty `effort-dialect` on an anthropic entry — `/effort` would answer `noEffortDialNote` (internal/tui/effort.go:35), the footer would hide the segment and the picker withhold the row on every anthropic server while the codec (item 9) honours the dial; so under `WireAnthropic` `Discover` sets `EffortSupport{Supported: true, Efforts: [low medium high xhigh max]}` on the active model and every entry (the wire implies the dial, per the ratified call) and keeps `forceEffortDialect` out of it. The two headless sites have no entry in scope — `discoverBeat` / `discoverDelegationBeat` are `func(ctx, endpoint, model, apiKey string)` (cmd/apogee/headless.go:396, :414), the same seam `firingInputs.beat` carries (wire_firing.go:68) — so thread the wire as a `provider.Wire` parameter on both seams and `firingInputs.beat` (wire_firing passes `provider.WireFor(in.entry.Wire)` / `entry.Wire`), updating daemon_test.go:85, daemonfire_test.go:66, schedule.go:168 and `stubBeat.discover` (wire_firing_test.go:714).

**Files:** `internal/provider/discovery.go`, `internal/provider/discovery_test.go`, `cmd/apogee/wire_server.go`, `cmd/apogee/upstream.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/delegation.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/schedule.go`, `cmd/apogee/daemon_test.go`, `cmd/apogee/daemonfire_test.go`, `internal/probe/host.go`, `internal/probe/discovery.go`, `internal/probe/discovery_test.go`, `docs/manual/probe.md`
**Read first:** internal/provider/discovery.go — Discover, discoverModels, discoverProps, forceEffortDialect; internal/heartbeat/heartbeat.go — NewMonitor; cmd/apogee/headless.go — discoverBeat, discoverDelegationBeat; cmd/apogee/wire_firing.go — firingInputs.beat

**Tests:** `TestDiscover_AnthropicWireListsModelsWithoutProps` (httptest: asserts `x-api-key` + `anthropic-version` headers, no `/props` request, model resolved, `EffortSupport` reports the five efforts on the active model and every entry); a cmd/apogee test that a `wire: anthropic` entry's Monitor request carries the anthropic headers (stub server recording headers); a headless/firing test that `discoverBeat` receives the entry's wire; an `internal/probe` test that an anthropic entry's discovery request carries the anthropic headers.

**Acceptance:**
- `go build ./... && go test ./internal/provider/ ./internal/heartbeat/ ./internal/probe/ -count=1 && go test ./cmd/apogee/ -run 'Monitor|Discover|Beat|Firing|Daemon|Schedule' -count=1`

**Commit:** `feat(provider): discovery on the anthropic wire; every Monitor is dialled with its server's wire`

## 12. Thread the wire through the engine dial seam (apogee-6fp)

**What:** Depends on items 7, 8, 11. Producers and consumers of the per-server wire value (enumerated): `domain.Config` gains `Wire string` (`internal/domain/config.go`, beside `EffortDialect`); `internal/agent/agent.go` New/Resume dial with `provider.WithWire(provider.WireFor(cfg.Wire))`; `rebind.go` `UpstreamSpec.Wire` → `SwitchUpstream` dial; `delegationtarget.go` `DelegationTarget.Wire` → `subagent.go` routed-child dial (no parent fallback: a target's own value, zero = openai); `construct.go` carries it; `cmd/apogee/wire_firing.go` firingConfig → `domain.Config.Wire`; `cmd/apogee/delegation.go` target builder; `cmd/apogee/naming.go`/`title.go` `upstreamBinding` gains `Wire` and `namingCall`'s `provider.NewClient` passes it; `cmd/apogee/probemodel.go` and `internal/judge` clients likewise; `cmd/apogee/wire_settings.go` `/server` switch builds the spec with the wire. Binding standard: the value is copied at each seam as a string (`domain` never imports `provider`).

**Regression guard.** The `/server` switch spec and the naming binding are built in `cmd/apogee/upstream.go` (`sessionMover.move` :271 builds `UpstreamSpec`; `upstreamHolder.Bind`/`Binding` :83/:166 carry endpoint/key/model only), not in `wire_settings.go` (no `UpstreamSpec` there): `upstreamHolder` gains `wire`, Bind/Swap take it (callers `serverBinder.bind` wire_server.go:142, `move` upstream.go:289), `Binding()` copies it into `upstreamBinding.Wire`, `move()` sets `UpstreamSpec.Wire` from `entry.Wire` — `upstream.go` replaces `wire_settings.go` in Files. `provider.Option` is opaque (`func(*Client)`, client.go:169) and Client exports no wire accessor, so the Dialer fake builds `provider.NewClient("", "", opts...)` and asserts `.Wire()` (the accessor item 8 adds). `internal/judge/judge.go` reads endpoint/key from env only (judge.go:20-27, :213-232) — there is no entry to take a wire from, so it is dropped from Files and the judge stays on the openai wire. The routed-target case lives in `dialer_test.go`/`routedspawn_test.go` (`routedTarget`), not `subagent_test.go`.

**Files:** `internal/domain/config.go`, `internal/agent/agent.go`, `internal/agent/rebind.go`, `internal/agent/delegationtarget.go`, `internal/agent/subagent.go`, `internal/agent/construct.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/delegation.go`, `cmd/apogee/naming.go`, `cmd/apogee/title.go`, `cmd/apogee/probemodel.go`, `cmd/apogee/upstream.go`, `cmd/apogee/wire_server.go`
**Read first:** internal/agent/agent.go — Dialer, dialProvider; internal/agent/rebind.go — UpstreamSpec; cmd/apogee/upstream.go — upstreamHolder.Bind, upstreamBinding, sessionMover.move; cmd/apogee/title.go — namingCall; internal/agent/dialer_test.go — fakeDialer

**Tests:** in `internal/agent`: the `Dialer` fake (`dialer_test.go`, `TestDialerIsUsedForSwitchUpstreamAndRoutedSpawn` extended) builds a Client from the received options and asserts `.Wire()` for New, SwitchUpstream and a routed child (`routedTarget`); in `cmd/apogee`: the naming client and a Firing's config carry `wire: anthropic` from the entry (`wire_firing_test.go`, `naming_test.go`), and a `/server` switch to an anthropic entry builds an `UpstreamSpec` with `Wire` set (`upstream` test).

**Acceptance:**
- `go build ./... && go test ./internal/domain/ ./internal/agent/ ./internal/judge/ -count=1 && go test ./cmd/apogee/ -run 'Firing|Naming|Delegation|Server' -count=1`

**Commit:** `feat(agent): the server's wire rides Config, UpstreamSpec and DelegationTarget into every dial`

## 13. stubllm speaks Messages on `/v1/messages` (apogee-6fp)

**What:** `internal/stubllm/server.go` `Handler()` gains `POST /v1/messages` → `handleMessages`: decode an `anthropicRequest` (`internal/stubllm/wire_anthropic.go`) into the neutral `log.Request` (which gains `Wire string`, `"openai"`/`"anthropic"`, set by the route handler — item 14 judges on it) — top-level `system` → a synthesised role-`system` message (so `when.system` and captures work), `tool_result` blocks → role `tool` + `ToolCallID`, `tool_use` → `ToolCalls`, `output_config.effort` → a fourth `Effort` member `OutputEffort` — then `take`/`reply` as today, and write Anthropic-shaped output: streaming `message_start`, `content_block_start/delta/stop` (text and `tool_use` with `input_json_delta` fragments honouring `Turn.Chunks`), `message_delta{stop_reason, usage}`, `message_stop`; whole-reply JSON likewise; `finishReason` mapped stop→`end_turn`, tool_calls→`tool_use`, length→`max_tokens`; `Turn.Error`/`HTTP` render the Anthropic error body; auth accepts `x-api-key` or Bearer. Recorder unchanged. `docs/design/test-drivers.md` gains a `### Wires` subsection under stubllm naming both routes.

**Files:** `internal/stubllm/server.go`, `internal/stubllm/wire_anthropic.go`, `internal/stubllm/log.go`, `internal/stubllm/server_test.go`, `internal/stubllm/doc.go`, `docs/design/test-drivers.md`
**Read first:** internal/stubllm/server.go — Handler, authorized, handleChat, take, reply; internal/stubllm/log.go — Request, Effort; internal/stubllm/script.go — Turn.finishReason

**Tests:** `TestMessagesRouteRunsAScriptedToolLoop` (a script with a tool call then a text reply, driven by hand-built Messages requests: system fold visible in the log, tool_use ids round-trip, stop reasons mapped); `TestMessagesRouteStreamsBlockEvents`; existing chat-completions tests untouched.

**Acceptance:**
- `go test ./internal/stubllm/ -count=1`

**Commit:** `feat(stubllm): a /v1/messages route serves the same scripts on the Anthropic wire`

## 14. End-to-end: a tool-use loop over the anthropic wire (apogee-6fp)

**What:** Depends on items 12, 13. The bead's acceptance. `cmd/apogee/e2e_wire_anthropic_test.go`: launch the TUI (`launchTUI`, `e2e_support_test.go`) against a stubllm script with a `wire: anthropic` server entry in the config, drive a prompt that triggers a `read_file` tool call and a final answer, and judge: the stub's request log shows `/v1/messages` requests only, the tool result arrived as a `tool_result` block, the final frame shows the answer, the headless `usage` line (run the same script via `apogee headless --format json`) carries `prompt_tokens`/`completion_tokens`/`cached_prompt_tokens` mapped from the Anthropic usage (ADR 0075 D10 names unchanged). Second case: the default entry still hits `/v1/chat/completions`. CHANGELOG sidecar for the whole feature lands here; close apogee-6fp.

**Regression guard.** `stubllm.Request` (log.go:12) records no route, and item 13 folds `tool_result` to role `tool` — identical to a chat-completions entry — so item 13 adds `Request.Wire string` ("openai"/"anthropic", set by the route handler) and this item judges on it. A `wire: anthropic` key sits inside a `servers:` list item that `launchTUI`/`appendHomeConfig` (e2e_support_test.go:59, :132 — appends lines after the block `e2eHome` wrote) cannot reach: use `launchTUIOn` (:93) with a home builder on the `launcherHome` pattern (e2e_livestate_test.go:407) and `headlessEventLines` + `eventLinesHome` (e2e_eventlines_test.go:146, :175) with `wire: anthropic` on the entry. Scripts load via `loadScript` from `testdata/stubllm/<name>.yaml` (e2e_stream_test.go:539), so the fixture is `cmd/apogee/testdata/stubllm/wire-anthropic.yaml`, loaded with `loadScript(t, "wire-anthropic")`.

**Files:** `cmd/apogee/e2e_wire_anthropic_test.go`, `cmd/apogee/testdata/stubllm/wire-anthropic.yaml`
**Read first:** cmd/apogee/e2e_support_test.go — launchTUIOn, e2eHome, appendHomeConfig; cmd/apogee/e2e_livestate_test.go — launcherHome; cmd/apogee/e2e_eventlines_test.go — headlessEventLines, eventLinesHome; cmd/apogee/e2e_stream_test.go — loadScript; internal/stubllm/log.go — Request

**Tests:** `TestE2EAnthropicWireCompletesAToolLoop`, `TestE2EDefaultWireStaysChatCompletions`.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'TestE2EAnthropicWire|TestE2EDefaultWire' -count=1`

**Commit:** `test(cmd/apogee): a full tool-use loop completes over the anthropic wire`

## 15. Inspector reads Anthropic SSE (apogee-6fp)

**What:** Depends on item 10. `internal/tui/inspector.go` decodes OpenAI `sseChunk` payloads for the readable wire view and falls back to raw JSON otherwise. Add a second decoder keyed on the payload's `type` (message_start … message_stop) rendering the same readable lines (text, thinking, tool_use name+fragment, stop reason, usage); `wireRequestSummary` also reads top-level `system` when present. Same output style as the OpenAI branch; no new key or option.

**Files:** `internal/tui/inspector.go`, `internal/tui/inspector_test.go`
**Read first:** internal/tui/inspector.go — wireReadableLines, wireResponsePassages, wireRequestSummary, sseChunk, prettyWireLine; internal/tui/inspector_test.go — TestReadableRequestSummarisesTheEnvelope, TestReadableMergesConsecutiveDeltas, TestReadableNamesAToolCallWithoutItsArguments

**Tests:** a captured Anthropic SSE response renders the readable lines; the OpenAI fixture output is unchanged (golden).

**Acceptance:**
- `go test ./internal/tui/ -run 'Inspector' -count=1`

**Commit:** `feat(tui): the Inspector renders Anthropic Messages streams readably`

## 16. `Meta.ParentID` — the fork pointer on the session record (apogee-zci)

**What:** `internal/session/store.go` `Meta` gains `ParentID string \`json:"parentID,omitempty"\`` (additive, no `RecordVersion` bump — the ScheduleID precedent, stated in the Meta doc comment). `validateID` applies to it on Load (a record whose ParentID is not a safe path component loads with ParentID cleared, never refused). `CONTEXT.md` §Session record gains one sentence: a forked record carries its parent's id.

**Regression guard.** Meta is decoded on three paths through one function — `decodeRecord` (store.go:436) serves `List`/`scan` (:244/:270 — the browser item 21 renders `⑂` from), `Load` (:299) and `LoadPath` (`--resume <file>`, :308) — so a clear placed in `Store.Load` alone leaves an unsafe ParentID in the browser list and on a path resume. The site is `decodeRecord`: it clears `rec.Meta.ParentID` when `validateID` refuses it (beside the ID refusal), so all three readers agree; `TestAnUnsafeParentIDLoadsCleared` asserts through both `Load` and `List`.

**Files:** `internal/session/store.go`, `internal/session/store_test.go`, `CONTEXT.md`
**Read first:** internal/session/store.go — Meta, validateID, decodeRecord, Store.List/scan, Store.Load, Store.LoadPath; internal/session/store_test.go — TestScheduleIdentityRoundTrips, firingRecord

**Tests:** `TestParentIDRoundTrips`, `TestRecordWithoutParentIDIsAnOrdinarySession`, `TestParentIDKeepsTheRecordVersion`, `TestAnUnsafeParentIDLoadsCleared` (asserted through both `Load` and `List`; modelled on the Schedule identity trio at `store_test.go`).

**Acceptance:**
- `go test ./internal/session/ -count=1`

**Commit:** `feat(session): Meta.ParentID records the session a record was forked from`

## 17. Engine cut primitive `CutSession` (apogee-zci)

**What:** Recast at the regression check (2026-09-16). `internal/agent/state.go`: `func CutSession(snap domain.Session, dropExchanges int) (domain.Session, error)` — pure over the opaque State: decode (`ErrSessionVersion` forward-reject as `restoreState`), locate the opening user messages (`RoleUser && !Interjected`, the `domain.lastExchangeOpening` rule; the overflow bridge is an opening like any other) walking BACKWARDS from the end, drop the last `dropExchanges` openings and everything after the surviving opening's Exchange end (`DropRange` from the earliest dropped opening to `Len`), `ClearDeferred`, `InExchange=false`, `ExchangeStart=0`, `PendingInput=nil`, `Tasks=nil` (ratified: task list cleared), re-encode. `dropExchanges < 0` or ≥ the number of openings → error naming the counts; `dropExchanges == 0` leaves the message history untouched but still applies the normalisation above. Export through the root facade as `apogee.CutSession`. Add `CutSnapshot(dropExchanges int) (domain.Session, error)` to `tui.Engine` (`internal/tui/tui.go`) = `CutSession(Snapshot(), n)` on the agent, forwarded by `cmd/apogee/wire_engine.go` `lateEngine`, stubbed on `internal/tui/seam_test.go` `fakeEngine`.

**Regression guard.** Cut semantics are FROM THE END, never by ordinal-from-start: `CutSession(snap domain.Session, dropExchanges int)` drops the last `dropExchanges` openings (`RoleUser && !Interjected`, walking backwards) and everything after the surviving opening's Exchange end; `dropExchanges < 0` or ≥ the number of openings → error naming the counts; `dropExchanges == 0` is a plain clone. `tui.Engine.CutSnapshot(dropExchanges int)` likewise. Apply the item's G lines for cancelled/aborted prompts. (Reviewer's G, verified: an ordinal from the START cannot name an Exchange once the session has folded — after a fold the history is [first user, summary, …] (internal/context/compact.go:58-84) plus a RoleUser bridge with no transcript entry (internal/agent/compact.go:303) — or cancelled — an Esc-cancel drops the Exchange's opening (internal/agent/turn.go:336) while the transcript keeps its entryUser; counted from the end with the bridge included, the post-fold stretch matches the engine exactly; item 18's guard supplies `drop` and marks cancelled prompts.) Re-check (2026-09-16), supersedes the "plain clone" wording above: `dropExchanges == 0` leaves the MESSAGE history untouched but still applies the normalisation (`ClearDeferred`, `InExchange=false`, `ExchangeStart=0`, `PendingInput=nil`, `Tasks=nil`) — item 20 calls `CutSnapshot(row.drop)` with drop 0 for the LAST prompt, and a fork at the newest prompt must clear the task list and PendingInput exactly like a fork at any earlier one; `TestCutSessionZeroIsAClone` pins it (messages equal, tasks cleared).

**Files:** `internal/agent/state.go`, `internal/agent/state_test.go`, `internal/agent/agent.go`, `apogee.go`, `internal/tui/tui.go`, `internal/tui/seam_test.go`, `cmd/apogee/wire_engine.go`
**Read first:** internal/agent/state.go — agentState, encodeState, restoreSnapshot, restoreState; internal/domain/exchange.go — lastExchangeOpening; internal/domain/hooks.go — Conversation.DropRange, ClearDeferred, MarshalJSON; internal/agent/compact.go — foldTable, overflowBridge; cmd/apogee/wire.go — `var _ tui.Engine = (*apogee.Agent)(nil)` (the Agent itself must gain CutSnapshot); cmd/apogee/wire_engine.go — lateEngine; internal/tui/seam_test.go — fakeEngine; apogee.go — Resume, DecodeSession

**Tests:** `TestCutSessionDropsTheLastExchanges` (3 Exchanges with tool calls and an interjection; drop 1 → messages end at Exchange 2's final assistant message, tasks cleared, not InExchange; drop 2 → ends at Exchange 1's); `TestCutSessionCountsTheBridgeAsAnOpening` (a folded-then-bridged history; drop 1 removes only the last post-bridge Exchange); `TestCutSessionZeroIsAClone` (drop 0 on a session with tasks and PendingInput → messages equal, tasks cleared, PendingInput nil, not InExchange); `TestCutSessionRejectsOutOfRange` (negative, and ≥ the openings); `TestCutSessionRejectsAForeignVersion`; resume of the cut Session via `Resume` works.

**Acceptance:**
- `go build ./... && go test ./internal/agent/ -run 'CutSession' -count=1 && go test ./internal/tui/ -run 'Seam' -count=1 && go test . -count=1`

**Commit:** `feat(agent): CutSession drops a session's last Exchanges and keeps the prefix`

## 18. Transcript prefix and fork eligibility (apogee-zci)

**What:** Recast at the regression check (2026-09-16). `internal/tui/transcript.go`: `forkPoints() []forkPoint{index int, drop int, text string}` — every depth-0 `entryUser` that lies AFTER the last depth-0 `entryCompacted` (ratified: folded stretches are not offered) and that the engine actually opened (a cancelled/aborted prompt is marked and neither listed nor counted); `drop` = the number of LATER depth-0 prompts that reached the wire, i.e. the `dropExchanges` `CutSession` takes; `prefixThrough(index int) []entry` — the entries up to the start of the next depth-0 `entryUser` after `index` (the whole Exchange incl. its tool cards and notes), never including startup entries. `internal/tui/transcriptbridge.go`: an export helper `entriesToRecords([]entry) []session.Entry` if one does not already exist (the save path's encoder), so the child's transcript blob is the cut prefix. `internal/tui/model.go` `foldCancelled`/`foldLoopError` mark the opening `entryUser` of a cancelled/faulted Exchange; the mark persists as an additive `omitempty` field on `session.Entry` (`internal/session/transcript.go`) so a resumed transcript still skips it. Limitation, dated 2026-09-16: records saved before the mark existed carry no mark, so `forkPoints` counts every unmarked depth-0 prompt as reaching the wire and such a record with a cancelled prompt cuts one Exchange later than picked; no user-facing text — `CutSession`'s range check still refuses an out-of-range drop, so the failure is a later cut, never a crash or a wrong session.

**Regression guard.** `forkPoints()` reports for each eligible prompt the number of LATER depth-0 prompts that reached the wire (`drop`), excluding prompts the engine never opened per this item's G lines (a cancelled/aborted prompt is marked and not counted); eligibility stays "after the last depth-0 compacted entry". Tests assert `drop` values, not start-ordinals. (Reviewer's G, verified: the engine drops every pre-fold prompt but the first (internal/context/compact.go:58-84), adds a bridge opening on an overflow fold (internal/agent/compact.go:303), and drops a cancelled/faulted prompt's opening (internal/tui/model.go foldCancelled/foldLoopError → turn.go:336) while transcript.go keeps the entryUser — counted from the end, the post-fold stretch matches the engine exactly because the bridge precedes it.) Re-check (2026-09-16), binding: The cancelled-prompt mark (the transcript Entry field this item adds) is set ONLY on the foldCancelled and foldLoopError paths in internal/tui — never on an exchangeDoneMsg whose Exchange faulted (turn.go endAbandoned keeps the opening) — and a test pins each of the three cases (cancelled → marked; loop error → marked; faulted → not marked, still counted). Records saved before the mark existed carry no mark: `forkPoints` counts every unmarked depth-0 prompt as reaching the wire, so such a record with a cancelled prompt cuts one Exchange later than picked; state this in the item's What as a dated limitation of pre-mark records (2026-09-16) with no user-facing text — `CutSession`'s range check still refuses an out-of-range drop, so the failure is a later cut, never a crash or a wrong session. Further (reviewer's G, verified): internal/tui/transcriptbridge_test.go `TestTranscriptCodecPersistsANamedDelegationAsItsTarget` pins `session.Entry`'s exact member list (`wantEntry`) by reflection — extend it with the new member; this item IS the "own decision" its failure message asks for.

**Files:** `internal/tui/transcript.go`, `internal/tui/transcript_test.go`, `internal/tui/transcriptbridge.go`, `internal/tui/transcriptbridge_test.go`, `internal/tui/model.go`, `internal/tui/model_test.go`, `internal/session/transcript.go`
**Read first:** internal/tui/transcript.go — entry, entryUser, entryCompacted, addUser, addUserAt (depth>0 entryUser), commitCancelled, userMessageCount; internal/tui/transcriptbridge.go — encodeTranscript (the skip rules entriesToRecords must keep), toWireEntry, fromWireEntry; internal/tui/model.go — foldCancelled, foldLoopError; internal/session/transcript.go — Entry, stripEntry; internal/tui/transcriptbridge_test.go — the wire-members pin; internal/agent/loop.go — step (autoCompact runs before open(), so the compacted entry lands AFTER the same-Step prompt); internal/agent/turn.go — abort, end (endAbandoned keeps the opening)

**Tests:** `TestForkPointsSkipFoldedPrompts` (user, compacted, user, user → two points with `drop` 1 and 0); `TestForkPointsSkipCancelledPrompts` (user, user(cancelled), user → two points with `drop` 1 and 0, the cancelled prompt absent and uncounted; the mark survives an encode/decode round trip); `TestCancelledMarkLandsOnlyOnTheTwoFolds` (three cases: foldCancelled → marked; foldLoopError → marked; an exchangeDoneMsg with a faulted Exchange → not marked, still counted); `wantEntry` in `TestTranscriptCodecPersistsANamedDelegationAsItsTarget` gains the new member; `TestPrefixThroughKeepsTheWholeExchange` (tool cards, a depth-1 user entry and a note inside the Exchange stay; the next prompt does not); an empty session → no points.

**Acceptance:**
- `go test ./internal/tui/ -run 'ForkPoints|PrefixThrough|CancelledMark|TranscriptCodec' -count=1`

**Commit:** `feat(tui): the transcript names its fork points and cuts its own prefix`

## 19. Host seam: `SessionHost.Fork` and the carried parent id (apogee-zci)

**What:** Recast at the regression check (2026-09-16). Depends on items 16, 17. `internal/tui/tui.go` `SessionHost` gains `Fork(parent session.Meta, sess domain.Session, transcript []session.Entry, title string) (session.Meta, error)`: mint a child id, write a full `session.Record` (RecordVersion, Meta with `ParentID` = the host's own session identity, `Title = title`, `Workspace`/`Model` from the host's wiring facts, `CreatedAt = UpdatedAt = now`, `UserMsgs` = the count of every `entryUser` in the prefix (the `userMessageCount` rule, depth>0 rows included), usage/ctx zero) through the store, return its Meta — no activation. `cmd/apogee/wire_session.go` implements it and `activeSession` gains `parentID` so `Save`'s rebuilt Meta keeps `ParentID` after `Activate(meta)` and after a `--resume` of a child (`newSessionHost`'s resumed branch); `Rotate` clears it. `internal/tui/sessionsave.go`: a new `writeFork` `recordWriteKind` carrying (cut Session, prefix entries, title, parent Meta); `pumpWrites` invokes `SessionHost.Fork` for it like the other kinds and its completion message carries the child Meta plus the `session.Record` the queue assembles from the same inputs. `internal/tui/seam_test.go` `fakeSessionHost` records Fork calls. Binding standard: on the host side Fork writes synchronously under `Store.mu` like `Rename`; the TUI reaches it only through the record write queue (`writeFork`), never by a synchronous host call from the command path.

**Regression guard.** The fork write goes THROUGH the record write queue: a new `writeFork` recordWrite kind carrying (cut Session, prefix entries, title, parent Meta); `SessionHost.Fork` is invoked by `pumpWrites` like the other kinds, and its completion message carries the child Meta+Record; never a synchronous host call from the command path. Keep the host-side Fork method as specified; the `Store.mu` sentence stands for the host side. Further (reviewer's G, verified): `newSessionHost`'s `resumed != nil` branch builds `activeSession{id,title,createdAt}` (cmd/apogee/wire_session.go:104-110) and Save rebuilds Meta from it (:150-166), so a `--resume <child id>` start's first Save would write ParentID "" — carry `resumed.Meta.ParentID` into `activeSession.parentID` there too (rule: every site that builds an activeSession — `grep -n 'activeSession{' cmd/apogee/wire_session.go`, three today) and pin it with a `TestSessionHostResumeBeginsActive`-shaped test. `ActiveID()` is "" until the first queued Save has landed (:305-312; the id is pre-minted in `nextID`), so Fork stamps `ParentID` from the host's own identity (`h.active.id`, else `h.nextID` — the `SessionID()` read) rather than trusting `parent.ID`, and returns it in the child Meta. Re-check (2026-09-16): `UserMsgs` is counted with the SAME rule as Save's (internal/tui/sessionsave.go `snapshotPayload` → `transcript.userMessageCount`, every `entryUser` including the depth>0 `addUserAt` rows), computed in the queue from the prefix entries — never the depth-0 count — so a child's "N msgs" cell does not change on its first Save.

**Files:** `internal/tui/tui.go`, `internal/tui/seam_test.go`, `internal/tui/sessionsave.go`, `internal/tui/sessionsave_test.go`, `cmd/apogee/wire_session.go`, `cmd/apogee/wire_session_test.go`
**Read first:** cmd/apogee/wire_session.go — sessionHost, activeSession, newSessionHost, Save, Activate, Rotate, SessionID, ActiveID; internal/tui/sessionsave.go — recordWriteKind, recordWrite, queueWrite, pumpWrites, writeCmd, foldRecordWrite, pumpOrQuit; internal/tui/tui.go — SessionHost; internal/tui/seam_test.go — fakeSessionHost; internal/session/store.go — Store.Save (under mu), NewID, EncodeTranscript; cmd/apogee/wire_session_test.go — TestSessionHostResumeBeginsActive, TestSessionHostRotateAndLoadActivate; internal/tui/sessions_test.go — the pendingWrites [writeSave, writeActivate] assertion

**Tests:** `TestSessionHostForkWritesAChildRecord` (child loads back with ParentID, the given transcript and Session; parent file byte-unchanged by the fork write itself); `TestForkStampsParentIDFromTheHostIdentity` (a host whose first Save has not landed forks a child whose ParentID is the pre-minted id); `TestActivateCarriesParentIDIntoLaterSaves`; `TestResumeCarriesParentIDIntoLaterSaves` (`newSessionHost(resumed)` on a child record → the next Save keeps ParentID); `TestRotateClearsParentID`; a `sessionsave_test.go` case that a queued `writeFork` reaches `fakeSessionHost.Fork` after the queued Save and its completion carries the child Meta and Record, with `UserMsgs` equal to the prefix's `entryUser` count including a depth-1 row (so a later Save of the child leaves the cell unchanged).

**Acceptance:**
- `go build ./... && go test ./cmd/apogee/ -run 'SessionHost|Activate|Rotate|Resume|Fork' -count=1 && go test ./internal/tui/ -run 'Seam|Write|Fork' -count=1`

**Commit:** `feat(session): the host forks a child record that carries its parent id through every save`

## 20. `/fork` — picker, cut, switch (apogee-zci)

**What:** Recast at the regression check (2026-09-16). Depends on items 18, 19. `internal/tui/command.go` `commandSpecs` gains `{name: "fork", summary: "branch a new session from one of this session's prompts"}` (idle-only, `touchesServer: false`). `internal/tui/fork.go`: `runFork` opens the shared `picker` overlay (`internal/tui/picker.go`: a `pickerFork` kind in the `pickerKind` enum and its four switch arms — `pickerOfferingRows`, `pickerTitle`, `pickerHintFor`, `acceptPicker` — the `/schedule-stop` shape) listing `forkPoints()` rows as `<n>. <prompt text, one line, clipped>` in transcript order; no rows → note `that stretch was folded — fork at a later block` (or `nothing to fork yet` on an empty session) and no fork. ⏎ on a row: `saveAtIdle`, then queue `writeFork` (item 19) with `eng.CutSnapshot(row.drop)`, `prefixThrough(row.index)` as records and the title; the switch to the child runs on the fork write's completion message via the sessions-browser resume flow (`resumeLoaded` with the returned record as a `sessionLoadedMsg`), followed by a note `forked from <parent id> — <parent title>`. Esc closes the picker. `docs/manual/commands.md` gains the `/fork` row.

**Regression guard.** `/fork` picks a row → `saveAtIdle`, then queue `writeFork` (item 19) with `eng.CutSnapshot(row.drop)`; the switch to the child runs on the fork write's completion message via the sessions-browser resume flow; note text unchanged. The item yields to `internal/tui/sessionsave.go:157-176` ("every write goes through ONE queue"; :85-92 records the closing callers being moved OFF synchronous host calls) — no disk write on the Update goroutine. Further (reviewer's G, verified): the `/schedule-stop` shape is the shared `picker` overlay keyed by `pickerKind` (picker.go:79; rows/title/hint/accept switches at `pickerOfferingRows`, `pickerTitle`, `pickerHintFor` :149, `acceptPicker`), so `internal/tui/picker.go` and `internal/tui/picker_test.go` (`TestPickerHintsLeadWithTypeToFilter`'s kinds list, :2251) join Files. Re-check (2026-09-16), three additions, binding: (a) `cmd/apogee/testdata/frames/popup-dropdown.txt` is a held golden of the `/` menu's first rows (/clear … /continue, /inspect, /model, /new) and `/fork` sorts between /continue and /inspect, so the golden is regenerated with `-update` (/new leaves the window) and `TestE2EPopupFramesLists` (cmd/apogee/e2e_popups_test.go, ungated, default suite) joins Acceptance. (b) With no session host (`m.sessions == nil`) `queueWrite` drops a `writeFork` silently, so `/fork` is refused up front with the /sessions posture (`openSessionBrowser`'s "no saved sessions" note) when `m.sessions` is nil. (c) The human can send a prompt between ⏎ and the completion fold, and `resumeLoaded`'s `RestoreSession` is refused only once the Exchange has opened (agent.go:1542-1545) — so on the completion fold, if `m.busy()` the switch is skipped and a note says `forked as <child id> — resume it from /sessions`; the record is already written.

**Files:** `internal/tui/command.go`, `internal/tui/commandrun.go`, `internal/tui/fork.go`, `internal/tui/fork_test.go`, `internal/tui/picker.go`, `internal/tui/picker_test.go`, `cmd/apogee/testdata/frames/popup-dropdown.txt`, `docs/manual/commands.md`
**Read first:** internal/tui/picker.go — pickerKind, picker, pickerHintFor, acceptPicker, pickerOfferingRows, pickerNote; internal/tui/schedule.go — runScheduleStop, stopSchedule; internal/tui/sessions.go — sessionLoadedMsg, loadSession, resumeLoaded, openSessionBrowser; internal/tui/sessionsave.go — saveAtIdle, scheduleWrite, foldRecordWrite; internal/tui/command.go — commandSpec, commandSpecs (alphabetical, pinned by command_test.go); internal/tui/commandrun.go — the "schedule-stop" case; internal/tui/picker_test.go — TestPickerHintsLeadWithTypeToFilter; cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesLists

**Tests:** `TestForkPickerListsPromptsAndForks` (driver: three prompts, `/fork`, pick row 2 → `fakeEngine` saw `CutSnapshot(1)`, the queued `writeFork` reached `fakeSessionHost.Fork` after the idle Save with a transcript ending before prompt 3, active id switched on the completion message, note text exact); `TestForkRefusesAFoldedSession` (note text exact, no Fork call); `TestForkEscClosesThePicker`; `TestForkIsIdleOnly`; `TestForkRefusesWithoutASessionHost` (`m.sessions == nil` → "no saved sessions" note, no picker, no Fork call); `TestForkSkipsTheSwitchWhenBusy` (a prompt sent between ⏎ and the completion fold → no `RestoreSession`, note `forked as <child id> — resume it from /sessions` exact, active id unchanged); `TestPickerHintsLeadWithTypeToFilter` gains the fork kind; `TestE2EPopupFramesLists` green against the regenerated `popup-dropdown.txt` golden.

**Acceptance:**
- `go test ./internal/tui/ -run 'Fork|Command' -count=1`
- `go test ./cmd/apogee/ -run 'TestE2EPopupFramesLists' -count=1`
- `grep -n '/fork' docs/manual/commands.md`

**Commit:** `feat(tui): /fork branches a new session from a chosen prompt and switches to it`

## 21. Session browser shows the fork relationship (apogee-zci)

**What:** Depends on item 16. `internal/tui/sessions.go` `sessionRowCells`: when `meta.ParentID != ""` the title cell gains ` · ⑂ <parent title>` (parent title looked up in the browser's `metas`; missing parent → `⑂ <parent id>`), in the slot after the `⟳ <schedule>` tag and escape-stripped like every Meta string. Column alignment unchanged. `docs/manual/sessions.md` gains one bullet describing `/fork` and the tag.

**Regression guard.** `sessionRowCells(meta, currentWorkspace, all, now)` is pure over one Meta (sessions.go:736) and the parent title must come from the browser's list, so its signature changes; nine direct calls in sessions_test.go (`TestSessionRowCells`, `…ScheduleTag`, `…StripsEscapes`) would go compile-red. Resolve the parent tag in `unfilteredRows` (sessions.go:227, which already walks `visible`; look the parent up in `b.metas`, not the workspace-filtered view) and pass it as one added string parameter, updating the nine test calls — or keep the old signature as a wrapper over the new one.

**Files:** `internal/tui/sessions.go`, `internal/tui/sessions_test.go`, `docs/manual/sessions.md`
**Read first:** internal/tui/sessions.go — sessionRowCells, sessionBrowser.unfilteredRows, visible, scheduleTagGlyph; internal/tui/sessions_test.go — TestSessionRowCells, TestSessionRowCellsScheduleTag, TestSessionRowsAlignTheColumns; internal/session/store.go — Meta

**Tests:** `TestSessionRowsTagForkedSessions` (tag with the parent's title resolved through `unfilteredRows`; parent absent → id; parent outside the current workspace still resolved; escape in the parent title stripped; a schedule tag and a fork tag coexist in order); the nine existing `sessionRowCells` calls updated (or untouched behind the wrapper) and green.

**Acceptance:**
- `go test ./internal/tui/ -run 'SessionRow|SessionBrowser' -count=1`

**Commit:** `feat(tui): the session browser tags a forked session with its parent`

## 22. End-to-end fork journey (apogee-zci)

**What:** Depends on items 20, 21. `cmd/apogee/e2e_fork_test.go` (shape of `e2e_tasklist_test.go`): stubllm script with three prompts; `/fork`, pick the row that drops the last prompt (`drop` 1); judge the frame shows the `forked from` note and the last prompt absent; `RelaunchWith("--resume <child id>")` → the child replays all but the last prompt and its next prompt reaches the wire with a history ending at the kept Exchange's final assistant message and containing no dropped prompt (stub request log, judged from the end); the parent's `Session`, `Transcript` and Meta minus `UpdatedAt` are unchanged from before `/fork`; `/sessions` shows the `⑂` tag — checked after the child's prompt. CHANGELOG sidecar for the feature; close apogee-zci.

**Regression guard.** the e2e judges via the stub request log that the child's next prompt's history ends at the kept Exchange's final assistant message and contains no dropped prompt — phrased from the end (the last prompt dropped), not by ordinal. Further (reviewer's G, verified): "byte-identical" cannot hold — item 20's flow starts with `saveAtIdle` and every Save re-stamps `UpdatedAt: now` (cmd/apogee/wire_session.go:153) — so compare the parent's `Session` and `Transcript` fields and Meta minus `UpdatedAt`, or take the byte baseline after the fork's queued save has landed (two records present). `loadScript(t, name)` reads `testdata/stubllm/<name>.yaml` (e2e_stream_test.go:539), so the fixture is `cmd/apogee/testdata/stubllm/fork.yaml`, loaded with `loadScript(t, "fork")`. The `/sessions` `⑂` check depends on item 19's resumed-branch guard (a relaunched child's first Save keeps ParentID); order it after the child's prompt deliberately, as the bite check for that guard.

**Files:** `cmd/apogee/e2e_fork_test.go`, `cmd/apogee/testdata/stubllm/fork.yaml`
**Read first:** cmd/apogee/e2e_tasklist_test.go — TestE2ETaskListReachesTheWireAndSurvivesAResume; cmd/apogee/e2e_support_test.go — launchTUIConfigured, e2eSession.RelaunchWith, sessionRecords; cmd/apogee/e2e_stream_test.go — loadScript; cmd/apogee/e2e_quitsave_test.go — the relaunch-then-/sessions pattern; cmd/apogee/e2e_approval_test.go — waitIdle; internal/stubllm — Server.Requests

**Tests:** `TestE2EForkKeepsThePrefixAndSurvivesAResume`.

**Acceptance:**
- `go test ./cmd/apogee/ -run 'TestE2EFork' -count=1`

**Commit:** `test(cmd/apogee): a fork keeps the prefix, resumes on its own and names its parent`
