# Plan — headless Event lines: `apogee headless --format json`

**Goal:** `apogee headless --format json` writes the run to stdout as versioned JSONL Event lines — one envelope per engine Event plus a `run_started` / `run_finished` frame pair — while `--format text` stays byte-identical to today. Closes bead `apogee-tvw`.
**Date:** 2026-09-07 · **Status:** unexecuted · **Sized for:** ~200k-context host
**Base:** `main` at `850f09c7` (line numbers below verified there; the working tree carries the uncommitted ADR 0075 / CONTEXT.md / ADR 0031 edits this plan relies on — commit them first or with item 1).

**Sources (precedence in this order):**
- `docs/adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md` — the contract (decisions 1–15, Consequences)
- `CONTEXT.md` — **Event**, **Event stream**, **Event lines**, **Driver** entries
- `docs/adr/0073-…` §6–§8 (Hooks boundary), `docs/adr/0031-…` amendment 2026-09-07
- `docs/design/test-drivers.md` (stubllm, goldens, `-update`)

**Ratified design calls** (owner 2026-09-07 unless marked *writer*, per ADR 0075's "pin at write time"):
- **Scope:** 17 variants every Depth, `WireEvent` never written and consumes no `seq`; JSONL only, no `serve`.
- **Envelope:** `{"event","v","seq","time","session","turn","depth","call_id","data"}` in that key order; every member always present, `null` where the line has no value; `v` is `1`; `time` RFC3339Nano; `seq` starts at 1 and both frames consume one.
- **Names:** the 19 kinds of ADR 0075 §4; `data` members are the variant's exported fields in snake_case, nested structs likewise; *writer:* `ToolResult.Summary` (sealed interface) is omitted; `json.RawMessage` arguments are embedded verbatim, empty → `null`.
- **Cancelled bracket carrier (writer):** `SubAgentPhaseEvent` gains `Cancelled bool` (`data.cancelled`); phase stays `finished`; a cancelled finished never enriches or ticks a TUI head.
- **Frames:** `run_finished` on every exit path; `session` from headless's `recordID`, `null` before it is minted; `saved` boolean; *writer:* `error` member (the run error text, `null` when none); exit code as the text path's (Ctrl-C → 1).
- **Blocking, SIGPIPE, second interrupt:** encoder is the outermost sink wrapper; SIGPIPE ignored on Unix under `--format json`; write error → one stderr line, stop emitting, run finishes; a second interrupt exits hard with code 1 and no frame.
- **Human path:** `--format text` default, unchanged; under `json` the answer is not printed and `pruneNoticeSink`'s stderr line is suppressed; all other stderr prose unchanged.
- **Facade:** `internal/eventjson` re-exported on `apogee.go` in the alias + forwarding-constructor style.

**Regression check (2026-09-07, 850f09c7):** items 2, 3, 5, 8 SAFE; four amended —
- 1: guard folded (a cancelled finished leaves the entry's phase untouched; `progressSaveTrigger` stays false; `fold.go` added).
- 4: guard folded (`finish` writes 0 on nil; the two pre-RunE refusals write no frame; `--format yaml` asserted on the returned error).
- 6: guard folded — supersedes the comments at `headless.go:56-60` and `:381-383` (the hard exit is the one deliberate skip of the deferred teardown).
- 7: guard folded — supersedes `internal/tuitest/golden.go:14-17` (ADR 0062) for this contract per ADR 0075 §14; dependency sentence replaced per the writer's decision (depends on 5 and 6).

**Standing requirements:**
- `skills: coding-standards`
- Deviations from item text land as a dated `NOTES:` line under the item.
- Never `-update` a golden to make it pass; a diff is a finding.

**Out of scope:** `apogee serve` (`apogee-afu`); live text rendering from the stream (`apogee-czf`); `--format` on the daemon or `probe`; `hooks.Payload` changes; any `EventBase` extension; version bumps.

## 1. Engine: a cancelled delegation closes its bracket — ✅ DONE (2026-09-07)

NOTES (2026-09-07): consequential edit — internal/domain/events.go (SubAgentFinished const doc): made necessary by the new Cancelled field, since "its result is known" no longer holds for every finished phase.
NOTES (2026-09-07): consequential edit — internal/agent/dispatch.go (runDelegationPool doc comment): made necessary by the pool now bracketing cancelled children too.
NOTES (2026-09-07): consequential edit — internal/tui/fold.go (progressSaveTrigger doc bullet): made necessary by the cancelled exception added to the predicate.
NOTES (2026-09-07): consequential edit — internal/tui/activity.go (foldEvent's SubAgentPhaseEvent comment): made necessary by a cancelled group now emitting a finished phase the slot drop sees.
NOTES (2026-09-07): the item's "set the phase but never call enrichWithResult" is implemented as the plan's own Regression guard directs — a `Cancelled: true` finished returns from `addSubAgentPhase` before touching `en.phase`, so `subAgentReported`/`childPhaseOf` stay false and the live star and `assertNoDoneMark` hold.
NOTES (2026-09-07): `internal/tui/subagentblock.go` needed no edit — `subAgentReported` already stays false because the cancelled phase is never recorded; the plan listed only its test file, which gained the new test.
NOTES (2026-09-07): the two cancel tests share one `assertCancelledBracket` helper placed in `delegationphase_test.go` beside `subAgentPhases`, rather than duplicating the assertion in both files.

**What:** Implements ADR 0075 §12. `internal/domain/events.go:177-181` — `SubAgentPhaseEvent` gains `Cancelled bool`; rewrite the doc comment at `:174-176` (a cancelled group now emits `finished` with `Cancelled: true`). `internal/agent/dispatch.go` — pool `:425-430`: emit `finished` for `dispatchCancelled` slots with `Cancelled: true` instead of skipping; serial `:865-870`: emit the same before returning `dispatchCancelled`. `emitSubAgentPhase` (`:473-478`) takes the flag. TUI: `internal/tui/transcript.go:1303-1316` `addSubAgentPhase` — on a cancelled finished, set the phase but never call `enrichWithResult`; `internal/tui/subagentblock.go:506` `subAgentFinished()` stays false for a cancelled head (it is closed by `closeInterruptedCalls` as today); `internal/tui/activity.go:135-140` comment rewritten (drop on finished is still right — the run is over). Binding: no new export on `apogee.go` (the field rides the alias).

**Regression guard.** In `addSubAgentPhase` (`transcript.go:1303`) a `Cancelled: true` finished leaves `en.phase` UNTOUCHED ("set the phase" above does not apply to it): `subAgentReported` (`subagentblock.go:505`, `done || phase == SubAgentFinished`) and `childPhaseOf` (`runview.go:80`) read the phase, so setting it would tick ✓ and drop the live star (`renderSubAgentRun` `:277`) and fail `assertNoDoneMark` in `e2e_outcome_test.go:161`. `progressSaveTrigger` (`internal/tui/fold.go:240`) returns false for `e.Cancelled` — no result landed, so the cancel path makes no extra record write.

**Files:** `internal/domain/events.go`, `internal/agent/dispatch.go`, `internal/agent/fanout_test.go`, `internal/agent/subagent_test.go`, `internal/agent/delegationphase_test.go`, `internal/tui/transcript.go`, `internal/tui/fold.go`, `internal/tui/fold_test.go`, `internal/tui/activity.go`, `internal/tui/subagentblock_test.go`

**Tests:**
- `internal/agent`: extend `TestFanOut_CancelRollsTheWholeTurnBack` (`fanout_test.go:318`) and `TestSubAgent_CancelledChildRollsTheParentTurnBack` (`subagent_test.go:642`) to assert exactly one `SubAgentPhaseEvent{Phase: finished, Cancelled: true}` per cancelled child, Depth and CallID matching its `started`.
- `internal/tui`: new `TestSubAgentCancelledFinishedLeavesTheHeadInterrupted` beside `subagentblock_test.go:590` — a cancelled finished neither ticks ✓ nor enriches, and `subAgentReported` stays false for the head; a `progressSaveTrigger` case in `fold_test.go` asserts a cancelled finished returns false.
- `cmd/apogee` golden `testdata/frames/t15-cancelled-delegation.txt` must pass unchanged.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./internal/domain/ ./internal/agent/ ./internal/tui/ && go test -race -count=1 ./cmd/apogee/ -run 'TestE2E.*Cancel|TestE2EOutcome'
```
**Commit:** `fix(agent): emit a cancelled finished phase so delegation brackets always close`

## 2. `internal/eventjson`: per-variant data encoders — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `data` follows the ratified call literally — "the variant's exported fields in snake_case, nested structs likewise" — so `tool_call` is `{"call":{…},"resolved_path":…}` and `tool_result` is `{"result":{…}}`, per ADR 0075 §3 ("variant members nested under `data`, never flattened"). ADR 0075 §11's shorthand spells these `tool_call.data.arguments` and `tool_result.data.content`; item 8's manual entry should use the nested paths `data.call.arguments` / `data.result.content`. No ADR edit made — §11 is fidelity prose, not the shape spec.
NOTES (2026-09-07): `ErrorEvent` encodes as `{"source","err"}` — `err` is the mechanical snake_case of the variant's own `Err` field, deliberately NOT the `error` a Hook event's payload spells (ADR 0075 §4 makes the vocabularies distinct on purpose).
NOTES (2026-09-07): the `WireEvent` → `ok == false` case is its own test (`TestEncodeSkipsTheWireEvent`) rather than a row of the golden table, since it has no `data` JSON to compare; a sibling `TestEncodeSkipsAnUnknownEvent` covers the default arm, and `TestKindsIsNotAliased` pins that `Kinds()` returns a fresh slice.
NOTES (2026-09-07): no `docmap_test.go` added — the package has one non-test file beside `doc.go`, far under the ~10-file threshold the standard sets, and `doc.go` already maps it.

**What:** New package `internal/eventjson` (doc.go states ADR 0075 as its contract). `encode.go`: `func Encode(ev domain.Event) (kind string, base domain.EventBase, data any, ok bool)` — `ok` false for `WireEvent`. The 17 mappings: `TokenEvent→token`, `ReasoningEvent→reasoning`, `StreamResetEvent→stream_reset`, `MessageEvent→message`, `ToolCallEvent→tool_call`, `ToolResultEvent→tool_result`, `SubAgentPhaseEvent→sub_agent_phase`, `SubAgentNamedEvent→sub_agent_named`, `ChildInterjectionEvent→child_interjection`, `ApprovalEvent→approval`, `TurnEvent→turn`, `MechanismFiredEvent→mechanism_fired`, `FloorGuardEvent→floor_guard`, `ErrorEvent→error`, `PruneEvent→prune`, `UsageEvent→usage`, `AuditEvent→audit`. `data` is a per-variant struct with explicit snake_case `json:` tags for every exported field (no `omitempty`; nested `ToolCall`, `ToolResult`, `ApprovalRequest`, `UserInput` get their own tagged mirrors — `ToolResult.Summary` omitted, `json.RawMessage` verbatim, empty → `null`). Binding trap: `AuditEvent` (`events.go:423-434`) — `base` is `ev.EventBase` read explicitly so `base.CallID` is the *spawning* call; `data.call_id` is the audited call's outer field. `Kinds()` returns all 19 names (frames included) for the manual test of item 8. Depends on item 1 (`cancelled` member).

**Files:** `internal/eventjson/doc.go`, `internal/eventjson/encode.go`, `internal/eventjson/encode_test.go`

**Tests:** `TestEncodeJSONGolden` — table-driven inline goldens in the style of `internal/hooks/payload_test.go:12-95`, one case per variant with the exact `data` JSON string, including: `audit` with distinct spawning/audited ids; `sub_agent_phase` at Depth 1 with `cancelled: true`; `tool_call` with raw arguments and with empty arguments (`null`); `tool_result` proving no `summary` key; `WireEvent` → `ok == false`. `TestKindsAreNineteen`.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./internal/eventjson/
```
**Commit:** `feat(eventjson): encode every engine Event variant as a snake_case data object`

## 3. `internal/eventjson`: the line Writer, frames and facade — ✅ DONE (2026-09-07)

NOTES (2026-09-07): the item's text does not say what happens when a `data` value refuses to marshal — reachable in practice, because a `ToolCallEvent` is emitted before anything parses the model's verbatim argument blob (`internal/agent/dispatch.go:256`), so malformed JSON reaches the encoder. Rather than let one bad blob stop the whole stream through the write-error path, `writeLine` retries the envelope once with `data` null; only a second failure stops the stream.
NOTES (2026-09-07): `Inner` is an unexported field (`inner`) set by `Wrap`, as the item specifies `Wrap` as its setter — an exported field beside the setter would be a second way to set the same thing.
NOTES (2026-09-07): consequential edit — example_test.go: made necessary by the new facade aliases (the file is the compile-time public-surface enumeration; five names added).
NOTES (2026-09-07): consequential edit — internal/eventjson/doc.go: made necessary by the new file (the package doc's "the files, one line each" map gained writer.go's row).

**What:** `internal/eventjson/writer.go`: `type Writer` built by `New(w io.Writer, o Options)`, `Options{Session string; Now func() time.Time; Report func(error)}`. `Writer` implements `domain.EventSink`: `Emit(ev)` encodes via item 2 (a `WireEvent` is forwarded only), writes one envelope line — key order and `null` rules per header — through a `bufio.Writer` flushed per line, then forwards to `Inner` (set by `Wrap(inner domain.EventSink) domain.EventSink`) — forward happens even after a write error. `SetSession(id)`; `seq` starts at 1, frames consume one. Frames: `RunStarted(RunStarted)` / `RunFinished(RunFinished)` — typed structs with tagged fields: `run_started{session, workspace, model, server, mode, bypass, confined, version}`; `run_finished{exit_code, turns, denied, faulted, fault, error, title, final_text, wrote, context_files, undo_note, saved, usage, sub_agents}` (`usage` / `sub_agents` mirror `run.Result`'s `Usage` / `SubAgentUsage` fields in snake_case; `context_files` mirrors `domain.ContextFilesReport`). Frames carry `turn`, `depth`, `call_id` as `null`. First write error: call `Report(err)` once, set `stopped`, every later line is dropped silently; `Emit` still forwards. `internal/eventjson` must not import `internal/run` (layering: `run` may later import it) — define the frame structs here and let headless fill them. Facade `apogee.go`: `type EventLines = eventjson.Writer`, `EventLinesOptions`, `RunStarted`, `RunFinished` aliases and `func NewEventLines(w io.Writer, o EventLinesOptions) *EventLines` beside the hooks block at `:633-647`. Depends on item 2.

**Files:** `internal/eventjson/writer.go`, `internal/eventjson/writer_test.go`, `apogee.go`

**Tests:** `TestWriterEnvelopeOrderAndNulls` (exact line strings; fixed `Now`); `TestWriterSeqCountsFramesAndSkipsWire`; `TestWriterForwardsEveryEventToInner` (including `WireEvent`); `TestWriterStopsAfterFirstWriteErrorAndReportsOnce` (failing writer, `Report` called once, forwarding continues); `TestWriterSessionNullUntilSet`; `TestFacadeExportsEventLines` in the root package's existing facade test file.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./internal/eventjson/ . 
```
**Commit:** `feat(eventjson): line Writer with frames, per-line flush and stop-on-error, re-exported on the facade`

## 4. Headless: `--format` flag, the exit funnel and a frame on every path — ✅ DONE (2026-09-07)

NOTES (2026-09-07): deviation — the post-sink refusal subtest (`run.Once`'s zero-Turn pre-run exit, `headless.go:624`) asserts `run_started` followed by exactly one `run_finished`, not "one `run_finished` line and nothing else" as the item's test bullet words it: the opening frame is written at `:579`, ahead of that exit, so two lines is the contract's own answer there. The three refusals that happen before `:579` do assert the closing frame alone.
NOTES (2026-09-07): the `Report` option of `eventjson.Options` is left nil — the single stderr line on a write error is item 6's (SIGPIPE), and sink installation and event forwarding are item 5's; this item constructs the Writer and writes frames only.
NOTES (2026-09-07): `run_started`'s `model` is `cfg.Model` (what the composer actually bound, which a per-model rebind may have moved off the entry's own `model:`) and `server` is `entry.Name`; `confined` is `opts.ConfineToWorkspace`, the posture the run was composed under.

**What:** `cmd/apogee/headless.go`. Add `--format` (`text` default, `json`) at `:242-259`; extend the `Long` text (`:203-224`) with one sentence on `--format json`. Any other value → `notStarted` in text mode (no stream exists yet). Under `json`, `runHeadless` (`:286`) becomes a funnel: construct `eventjson.New(cmd.OutOrStdout(), …)` before the first `notStarted` return at `:299`; every return — the thirteen `notStarted` sites (`:239`, `:270`, `:299`, `:305`, `:317`, `:323`, `:330`, `:351`, `:421`, `:472`, `:511`, `:541`, `:624`), `runFailed` (`:679-687`), faulted (`:693-698`) and success — passes through one `finish(err, res)` that writes `run_finished` with `exit_code = exitCodeFor(err)`, `error` = err text or `null`, `saved = res.SessionID != ""`, `session` from `recordID` (`:489`; `SetSession` right after it is minted, `null` before). The funnel is implemented by extracting today's body into `runHeadlessBody(…) (run.Result, error)` and writing the frame in the wrapper, so the text path's control flow is untouched. `run_started` is written immediately after the sink chain at `:579` (values: `recordID`, workspace root, model, server entry name, mode, bypass, confined, the build version string `apogee version` prints). Under `json` the stdout answer print at `:641-643` is skipped; every `cmd.PrintErrln` stays. Sink installation and event forwarding are item 5 — this item writes frames only. Depends on item 3.

**Regression guard.** `finish` writes `exit_code` 0 when `err == nil` and `exitCodeFor(err)` otherwise — `exitCodeFor(nil)` returns 1 (`headless.go:80-86`), so the success frame would carry 1. Of the thirteen sites, `:239` (`SetFlagErrorFunc`) and `:270` (`headlessArgs`) run inside cobra before `RunE`, where `--format` is unparsed: those two refusals write no frame and are outside the funnel, which covers the eleven in-body sites plus `runFailed`, faulted and success.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`

**Tests (canned `stubRunner`, `headlessRunOn` `:92-121`):**
- `TestHeadlessFormatJSONFramesEveryExit`: subtests for each not-started class — before the id (empty prompt `:299`, bad mode `:323`), heartbeat `:541` (`session` present, `saved: false`), post-sink `:624` (canned runner returns `Result{}, err`) — each asserts stdout is exactly one `run_finished` line with `exit_code: 2` and nothing else; completed run → `run_started` then `run_finished` with `exit_code: 0`, `final_text`, `wrote`, `saved: true`; failed → `1` with `error`; faulted → `3` with `fault`.
- `TestHeadlessFormatJSONKeepsStderrProse`: the summary, usage and `changed —` lines still on stderr; the answer absent from stdout.
- `TestHeadlessFormatRejectsUnknownValue`: `--format yaml` → `exitCodeFor(err) == 2`, empty stdout, and the returned error's text names `--format` (as `TestHeadlessExitCodes` asserts — `SilenceErrors: true` at `:227` keeps the error off stderr in-process; only `main.go:21` prints it).
- `TestHeadlessOutputRouting` (`:1106`), `TestHeadlessExitCodes` (`:1897`) and every existing test pass unchanged — the text path is byte-identical.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./cmd/apogee/ -run 'TestHeadless'
```
**Commit:** `feat(headless): --format json writes run_started/run_finished frames on every exit path`

## 5. Headless: the live Event lines, prune-line suppression, cancellation — ✅ DONE (2026-09-07)

NOTES (2026-09-07): `TestHeadlessFormatJSONEncoderIsOutermost` reads the type inside `eventjson.Writer`'s unexported `inner` field through `reflect` (type only, never the value): Wrap's field has no accessor, and adding one to the package for a single test would be worse than the inspection.

**What:** `cmd/apogee/headless.go:579`: under `json`, `cfg.Events = lines.Wrap(pruneNoticeSink{inner: cfg.Events, out: stderr, quiet: true})` — the encoder is the **outermost** wrapper (engine → `serialEventSink` → `eventTap` → encoder → `pruneNoticeSink` → `hooks.Runner`); it is never placed inside `hooks.Runner` (`internal/hooks/runner.go:66-70`). `pruneNoticeSink` (`:124-140`) gains `quiet bool`: forwards but prints nothing. `Options.Report` prints exactly once on stderr: `apogee headless: event lines stopped — <err>` (via `cmd.PrintErrln`). A Ctrl-C-cancelled run reaches the item-4 funnel as today (`runFailed`, exit 1) and its `run_finished` is written. Update the sink-order comment at `:576-578`. Depends on item 4.

**Files:** `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`

**Tests:**
- `TestHeadlessFormatJSONStreamsEveryEvent`: `stubRunner.emit` drives one of every variant (`WireEvent` included) at Depth 0 and a Depth-1 `sub_agent_phase`; stdout is `run_started`, the 17 envelopes in emission order with `seq` 2..18, then `run_finished` at `seq` 19; `WireEvent` absent.
- `TestHeadlessFormatJSONSuppressesThePruneNotice`: a `PruneEvent` yields a `prune` line and no `pruned … tool results` line on stderr; `TestHeadlessPrintsThePruneNotice` (`:2273`) still passes in text mode.
- `TestHeadlessFormatJSONEncoderIsOutermost`: inspect `cfg.Events` as `TestHeadlessComposesTheRunnerSpec` (`:617-640`) does — the outer value is the encoder's wrapper, its inner the `pruneNoticeSink`.
- `TestHeadlessFormatJSONWriteErrorStopsLinesNotTheRun`: failing stdout writer → run completes, exit code unchanged, the stderr line appears once.
- `TestHeadlessFormatJSONCancelledRunWritesTheFrame`: canned runner returns the cancellation error with `Turns: 1` → exit 1, `run_finished.exit_code: 1`, `error` present.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./cmd/apogee/ -run 'TestHeadless|TestPruneNoticeSink'
```
**Commit:** `feat(headless): stream the engine's Events as Event lines under --format json`

## 6. Headless: SIGPIPE and the hard second interrupt — ✅ DONE (2026-09-07)

NOTES (2026-09-07): consequential edit — cmd/apogee/doc.go: made necessary by the two new files (TestDocMapNamesEveryFile fails until the package file map names sigpipe_unix.go and sigpipe_windows.go).
NOTES (2026-09-07): the unit test models the FIRST press by cancelling the command's own context and delivering the same signal on the injected channel — a real SIGTERM at this process would end the test binary — so `interruptSignals` is the seam and `signal.NotifyContext`'s half is stood in for by the cancel. The second press is a plain send on that channel.
NOTES (2026-09-07): the "one value only → no hard exit" case is a SUBTEST of TestHeadlessSecondInterruptExitsHard rather than a test of its own, so the item's own `-run 'TestHeadlessSecondInterrupt|TestE2EEventLines'` acceptance covers both halves.
NOTES (2026-09-07): the e2e claim was negative-controlled — with `ignoreSIGPIPE` stubbed to a no-op the run dies with `signal: broken pipe` (exit -1) and the test fails, so it is not passing by accident.

**What:** New `cmd/apogee/sigpipe_unix.go` (`//go:build !windows`): `func ignoreSIGPIPE() { signal.Ignore(syscall.SIGPIPE) }`; `cmd/apogee/sigpipe_windows.go` (`//go:build windows`): no-op. Called in `runHeadless` only when `--format json`, before the first stdout write. Second interrupt: beside `signal.NotifyContext` at `:569-570`, under `json` register `signal.Notify(ch, os.Interrupt, syscall.SIGTERM)`; a goroutine that sees a **second** signal after the context is cancelled prints `apogee headless: second interrupt — exiting without waiting for the run` on stderr and calls `hardExit(exitRunFailed)`, where `var hardExit = os.Exit` is a package seam beside `runOnce` (`:96`). The goroutine stops with the run. Text mode gains neither. Depends on item 5.

**Regression guard.** The hard exit deliberately skips the deferred teardown — `defer stop()` (`:570`) and the Confiner `Close` (`:384-390`, Windows labels per ADR 0020 §2): say so in What's goroutine, and rewrite the comments at `headless.go:56-60` (`exitError`) and `:381-383` to carry this one exception — they are superseded for it. The manual sentence item 8 writes on the second interrupt says the same. The blocking runner in the test is a direct `runOnce` swap (`e2e_naming_test.go:191` shape), never a `stubRunner` (`once` ignores ctx, `headless_test.go:50`).

**Files:** `cmd/apogee/sigpipe_unix.go`, `cmd/apogee/sigpipe_windows.go`, `cmd/apogee/headless.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/e2e_eventlines_test.go`

**Tests:**
- `TestHeadlessSecondInterruptExitsHard` (direct `runOnce` swap that blocks until ctx is done, then blocks again): inject the signal channel through a seam, send two values; the injected `hardExit` stub snapshots INSIDE itself the code and the stdout bytes written so far, and the test asserts that snapshot — code 1, stderr line printed once, no `run_finished` in the snapshot (once the stub returns, the released runner lets item 4's funnel write the frame). One value only → no hard exit.
- `TestE2EEventLinesSurviveAClosedPipe` in `cmd/apogee/e2e_eventlines_test.go`: runs the built binary (`e2eBinary`, `main_test.go:42-85`) with `headless --format json` against `stubllm.New` and a stdout pipe whose read end is closed after the first line; asserts the process exits with the run's own code (0), not a signal, and stderr contains `event lines stopped` exactly once. Skipped on Windows.

**Acceptance:**
```
go build ./... && GOOS=windows go vet ./cmd/apogee/ && go test -race -count=1 ./cmd/apogee/ -run 'TestHeadlessSecondInterrupt|TestE2EEventLines'
```
**Commit:** `feat(headless): ignore SIGPIPE and exit hard on a second interrupt under --format json`

## 7. Goldens: the contract under a scripted upstream

**What:** ADR 0075 §14. `cmd/apogee/testdata/stubllm/eventlines.yaml`: a Depth-0 script — turn 1 a `read_file` tool call with `usage`, turn 2 (`when: {tool_result: read_file}`) the final text with `usage`. `cmd/apogee/e2e_eventlines_test.go` gains `TestE2EEventLinesGolden`: a real run through the `headlessAgainst` shape (`e2e_naming_test.go:182-222`) with `--format json`, compared line-for-line to `cmd/apogee/testdata/eventlines/run.jsonl` after normalizing `"time":"…"` → `"time":"<time>"`, `"session":"…"` → `"session":"<session>"`, the workspace path and the `version` value; `-update` rewrites the golden via the same `tuitest` flag (`internal/tuitest/golden.go:22-23`) — add `tuitest.GoldenText(t, name, text, redactions...)` for non-frame text, in `internal/tuitest/golden.go`, sharing `compareGolden`. Second golden `not-started.jsonl`: a real run against an endpoint that refuses the dial (heartbeat `:541`) → exactly one `run_finished`, `exit_code: 2`, `session` present, `saved: false`. Third `not-started-after-sink.jsonl`: canned runner pre-run exit (`:624` shape) → one `run_finished`, `exit_code: 2`. Each golden test also asserts semantically: `seq` contiguous from 1, `v` 1 on every line, first line `run_started`, last `run_finished`. Depends on items 5 and 6 — `cmd/apogee/e2e_eventlines_test.go` is created by item 6 and extended here; the scout turns this into a DEPENDS gate so 6 always lands first.

**Regression guard.** `GoldenText` takes the golden's dir and extension and threads them into `compareGolden` (`golden.go:113` hard-codes `goldenDir` + `.txt`, unreachable for `testdata/eventlines/*.jsonl`); the `-update` help at `:22-23` names both. `not-started.jsonl` is compared with `"error":"…"` redacted to `"error":"<error>"` (its text is `notice.ServerOffline` with the dial's OS- and port-specific words, `headless.go:541`, `heartbeat.go:180`) and the `cannot send — server offline` prefix asserted semantically. The `run_started`-first assertion is scoped to `run.jsonl`; the two not-started goldens assert one line, `run_finished`, `seq` 1. `golden.go:14-17` (ADR 0062, goldens for rendering surfaces only) is superseded for this contract by ADR 0075 §14 — say so beside `GoldenText`.

**Files:** `cmd/apogee/testdata/stubllm/eventlines.yaml`, `cmd/apogee/testdata/eventlines/run.jsonl`, `cmd/apogee/testdata/eventlines/not-started.jsonl`, `cmd/apogee/testdata/eventlines/not-started-after-sink.jsonl`, `cmd/apogee/e2e_eventlines_test.go`, `internal/tuitest/golden.go`, `internal/tuitest/golden_test.go`

**Tests:** the three golden tests above; `TestGoldenTextRoundTrips` in `internal/tuitest` (a non-`frames` dir and a non-`.txt` extension round-trip through `-update` and compare). `stub.AssertConsumed(t)` on the scripted run.

**Acceptance:**
```
go build ./... && go test -race -count=1 ./internal/tuitest/ && go test -race -count=1 ./cmd/apogee/ -run 'TestE2EEventLines'
```
**Commit:** `test(headless): golden Event lines under a scripted upstream and on the not-started paths`

## 8. Manual and docs: the Event lines contract

**What:** `docs/manual/headless.md` — a new `## Machine-readable output — --format json` section after the stdout/stderr paragraph (`:50-53`): the envelope with one example line, the 19 line kinds in one table (name → what it marks), the two frames and their members, `v:1` and the additive rule (ignore unknown names, members, enum values), `session`/`saved` and the `apogee undo` link, `final_text` duplication, lossless-and-blocking with the SIGPIPE and second-interrupt behaviour, the `event lines stopped` stderr line, the CI-redirection warning (full tool arguments and results are on stdout), and that the daemon and `probe` have no `--format`. Fix `:77` so the changed-files block is stated to be on **stderr** (matches `headless.go:653-658` and CHANGELOG `[Unreleased]` `:922`); reword `:50-53` so "only the answer goes to stdout" is scoped to `--format text`. `docs/manual/README.md:14` row mentions `--format json`. `docs/manual/hooks.md:208` stays true — verify, do not edit. Depends on item 3 (`eventjson.Kinds()`).

**Files:** `docs/manual/headless.md`, `docs/manual/README.md`, `cmd/apogee/docs_eventlines_test.go`

**Tests:** `TestManualListsEveryEventLineKind` in `cmd/apogee/docs_eventlines_test.go` (style of `docs_env_test.go`): every name from `eventjson.Kinds()` appears as a backticked token in `docs/manual/headless.md`, and the page names `--format json`, `run_started`, `run_finished` and `event lines stopped`.

**Acceptance:**
```
go test -race -count=1 ./cmd/apogee/ -run 'TestManual'
```
**Commit:** `docs(manual): document the headless Event lines contract`
